// SPDX-License-Identifier: GPL-3.0-only

package store

// default_preset_test.go — новая установка получает набор по умолчанию, а
// обновление не меняет маршруты тем, кто набор не выбирал.

import "testing"

const activePresetKey = "routing.active_preset"

// reopen закрывает базу и открывает заново, как при следующем запуске ядра.
func reopen(t *testing.T, s *Store) *Store {
	t.Helper()
	dir := s.Dir()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	next, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = next.Close() })
	return next
}

// reopenAfterUpgrade приводит базу к состоянию «первый запуск после 0.9.2».
func reopenAfterUpgrade(t *testing.T, s *Store) *Store {
	t.Helper()
	if _, err := s.db.Exec(`DELETE FROM meta WHERE key = ?`, defaultPresetSettledKey); err != nil {
		t.Fatal(err)
	}
	return reopen(t, s)
}

func addRawProfile(t *testing.T, s *Store) {
	t.Helper()
	_, err := s.db.Exec(`INSERT INTO profiles(id,group_id,type,sort_key,data,created_at_ms,updated_at_ms)
		VALUES (1,?,'trojan',1,'{}',1,1)`, DefaultGroupID)
	if err != nil {
		t.Fatal(err)
	}
}

func activePresetRow(t *testing.T, s *Store) (string, bool) {
	t.Helper()
	v, ok, err := s.GetSetting(activePresetKey)
	if err != nil {
		t.Fatal(err)
	}
	return v, ok
}

func TestFreshDatabaseHasNoPresetRow(t *testing.T) {
	s := openTemp(t)
	if v, ok := activePresetRow(t, s); ok {
		t.Fatalf("в свежей базе закреплён набор %s", v)
	}
}

func TestUpgradeKeepsGlobalForInstallWithServers(t *testing.T) {
	s := openTemp(t)
	addRawProfile(t, s)
	s = reopenAfterUpgrade(t, s)
	if v, ok := activePresetRow(t, s); !ok || v != `"global"` {
		t.Fatalf("набор = %q, есть строка: %v; ждём закреплённый global", v, ok)
	}
}

func TestUpgradeKeepsExplicitChoice(t *testing.T) {
	s := openTemp(t)
	addRawProfile(t, s)
	if err := s.SetSetting(activePresetKey, `"lav-cn"`); err != nil {
		t.Fatal(err)
	}
	s = reopenAfterUpgrade(t, s)
	if v, _ := activePresetRow(t, s); v != `"lav-cn"` {
		t.Fatalf("выбор пользователя перезаписан: %s", v)
	}
}

func TestUpgradeWithoutServersFollowsNewDefault(t *testing.T) {
	s := reopenAfterUpgrade(t, openTemp(t))
	if v, ok := activePresetRow(t, s); ok {
		t.Fatalf("база без серверов закрепила набор %s", v)
	}
}

// Новая установка, в которую серверы добавили после первого запуска, остаётся
// на значении по умолчанию.
func TestServersAddedAfterFirstStartDoNotPinGlobal(t *testing.T) {
	s := openTemp(t)
	addRawProfile(t, s)
	s = reopen(t, s)
	if v, ok := activePresetRow(t, s); ok {
		t.Fatalf("закреплён набор %s, хотя база создана новой версией", v)
	}
}

// Копия от 0.9.2 (отметки нет) после восстановления не меняет фактический набор.
func TestRestoreOldCopyKeepsEffectivePreset(t *testing.T) {
	cases := map[string]struct{ stored, want string }{
		"набор не выбирали": {"", `"global"`},
		"выбран lav-cn":     {`"lav-cn"`, `"lav-cn"`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			src, dst := openTemp(t), openTemp(t)
			addRawProfile(t, src)
			if c.stored != "" {
				if err := src.SetSetting(activePresetKey, c.stored); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := src.db.Exec(`DELETE FROM meta WHERE key = ?`, defaultPresetSettledKey); err != nil {
				t.Fatal(err)
			}
			if err := dst.Restore(bg, snapshotOf(t, src)); err != nil {
				t.Fatalf("Restore: %v", err)
			}
			if v, _ := activePresetRow(t, dst); v != c.want {
				t.Fatalf("набор после восстановления = %q, ждём %s", v, c.want)
			}
		})
	}
}

// Сброс настроек после обновления возвращает к значению по умолчанию: закрепление
// разовое и при следующем запуске не повторяется.
func TestPinIsOneShotAfterSettingsReset(t *testing.T) {
	s := openTemp(t)
	addRawProfile(t, s)
	s = reopenAfterUpgrade(t, s)
	if _, err := s.DeleteSettings([]string{activePresetKey}); err != nil {
		t.Fatal(err)
	}
	s = reopen(t, s)
	if v, ok := activePresetRow(t, s); ok {
		t.Fatalf("после сброса набор снова закреплён: %s", v)
	}
}
