// SPDX-License-Identifier: GPL-3.0-only

package nekoimport

import (
	"errors"
	"path/filepath"
	"testing"
)

// makeConfig создаёт в dir минимальный config NekoBox (1 группа, 2 профиля, 1 маршрут).
func makeConfig(t *testing.T, dir string) string {
	t.Helper()
	write(t, filepath.Join(dir, "groups", "0.json"), `{"id":0,"name":"G","front_proxy_id":-1}`)
	write(t, filepath.Join(dir, "groups", "pm.json"), `{"groups":[0]}`)
	write(t, filepath.Join(dir, "profiles", "1.json"), `{"type":"socks","id":1,"gid":0,"bean":{}}`)
	write(t, filepath.Join(dir, "profiles", "2.json"), `{"type":"socks","id":2,"gid":0,"bean":{}}`)
	write(t, filepath.Join(dir, "routes_box", "Default"), `{}`)
	return dir
}

func noProcesses() ([]Process, error) { return nil, nil }

func bySource(list []Candidate) map[string]Candidate {
	out := map[string]Candidate{}
	for _, c := range list {
		out[c.Source] = c
	}
	return out
}

// Переносимая установка: config рядом с запущенным nekobox.exe (P).
func TestDetectPortableNextToExe(t *testing.T) {
	root := mkTempDir(t)
	cfg := makeConfig(t, filepath.Join(root, "nekoray", "config"))
	env := Env{Processes: func() ([]Process, error) {
		return []Process{{Name: "NekoBox.exe", ExeDir: filepath.Join(root, "nekoray")}, {Name: "notepad.exe", ExeDir: root}}, nil
	}}
	got := Detect(env, nil)
	if len(got) != 1 {
		t.Fatalf("кандидаты: %+v", got)
	}
	c := got[0]
	if c.Path != cfg || c.Source != SourceProcess || c.Groups != 1 || c.Profiles != 2 || c.Routes != 1 || !c.LooksRunning {
		t.Errorf("кандидат: %+v", c)
	}
}

// %APPDATA% и %LOCALAPPDATA%: каталог nekoray\config; запущенных процессов нет (P).
func TestDetectAppdata(t *testing.T) {
	roaming, local := mkTempDir(t), mkTempDir(t)
	makeConfig(t, filepath.Join(roaming, "nekoray", "config"))
	makeConfig(t, filepath.Join(local, "nekoray", "config"))
	got := bySource(Detect(Env{AppData: roaming, LocalAppData: local, Processes: noProcesses}, nil))
	if len(got) != 2 || got[SourceAppData].Path == "" || got[SourceLocalAppData].Path == "" {
		t.Fatalf("кандидаты: %+v", got)
	}
	if got[SourceAppData].LooksRunning || got[SourceLocalAppData].LooksRunning {
		t.Error("NekoBox не запущен, а looks_running выставлен")
	}
	// Пустые переменные окружения и отсутствие каталогов — не ошибка.
	if n := len(Detect(Env{Processes: noProcesses}, nil)); n != 0 {
		t.Errorf("кандидатов без окружения: %d", n)
	}
}

// Один каталог, найденный тремя путями, — один кандидат (P). Выигрывает ручной путь.
func TestDetectDedup(t *testing.T) {
	root := mkTempDir(t)
	app := filepath.Join(root, "nekoray")
	cfg := makeConfig(t, filepath.Join(app, "config"))
	env := Env{AppData: root, Processes: func() ([]Process, error) {
		return []Process{{Name: "nekoray.exe", ExeDir: app + string(filepath.Separator)}}, nil
	}}
	// Ручной путь с «..» и каталогом установки вместо config.
	got := Detect(env, []string{filepath.Join(app, "config", "..", "config"), app, cfg})
	if len(got) != 1 || got[0].Source != SourceManual || got[0].Path != cfg {
		t.Fatalf("кандидаты: %+v", got)
	}
}

// Ручные пути: относительные и без данных отбрасываются (I); каталог установки
// принимается и указывает на вложенный config.
func TestDetectManualPaths(t *testing.T) {
	root := mkTempDir(t)
	cfg := makeConfig(t, filepath.Join(root, "install", "config"))
	empty := mkTempDir(t)
	got := Detect(Env{Processes: noProcesses}, []string{"relative/path", "", empty, filepath.Join(root, "install")})
	if len(got) != 1 || got[0].Path != cfg {
		t.Fatalf("кандидаты: %+v", got)
	}
}

// Процесс с неизвестным путём (запущен с повышением): все кандидаты помечаются
// «запущен» — предупреждение лучше пропущенного (P).
func TestDetectUnknownProcessPathMarksAll(t *testing.T) {
	root := mkTempDir(t)
	cfg := makeConfig(t, filepath.Join(root, "config"))
	env := Env{Processes: func() ([]Process, error) { return []Process{{Name: "nekobox.exe"}}, nil }}
	got := Detect(env, []string{cfg})
	if len(got) != 1 || !got[0].LooksRunning {
		t.Fatalf("кандидаты: %+v", got)
	}
}

// Сбой перечисления процессов не ломает поиск.
func TestDetectProcessListErrorIgnored(t *testing.T) {
	root := mkTempDir(t)
	cfg := makeConfig(t, filepath.Join(root, "config"))
	env := Env{Processes: func() ([]Process, error) { return nil, errors.New("access denied") }}
	if got := Detect(env, []string{cfg}); len(got) != 1 || got[0].LooksRunning {
		t.Fatalf("кандидаты: %+v", got)
	}
}

func TestResolveConfigDir(t *testing.T) {
	root := mkTempDir(t)
	cfg := makeConfig(t, filepath.Join(root, "config"))
	for _, in := range []string{cfg, root} {
		if got, ok := ResolveConfigDir(in); !ok || got != cfg {
			t.Errorf("ResolveConfigDir(%q) = %q, %v", in, got, ok)
		}
	}
	if _, ok := ResolveConfigDir(mkTempDir(t)); ok {
		t.Error("пустой каталог принят")
	}
	if _, ok := ResolveConfigDir(filepath.Join(root, "нет такого")); ok {
		t.Error("несуществующий путь принят")
	}
}

// Установка в %APPDATA% (режим -appdata): exe лежит в другом каталоге, и
// сопоставить процесс с config по пути нельзя. Запущенный NekoBox всё равно
// даёт предупреждение «закройте NekoBox» (P).
func TestDetectAppdataLooksRunning(t *testing.T) {
	roaming := mkTempDir(t)
	other := mkTempDir(t)
	makeConfig(t, filepath.Join(roaming, "nekoray", "config"))
	portable := makeConfig(t, filepath.Join(other, "portable", "config"))
	env := Env{AppData: roaming, Processes: func() ([]Process, error) {
		return []Process{{Name: "nekobox.exe", ExeDir: filepath.Join(other, "Program Files", "nekoray")}}, nil
	}}
	got := bySource(Detect(env, []string{portable}))
	if !got[SourceAppData].LooksRunning {
		t.Errorf("appdata при запущенном NekoBox не помечен: %+v", got[SourceAppData])
	}
	if got[SourceManual].LooksRunning {
		t.Errorf("переносимая установка из другого каталога помечена запущенной: %+v", got[SourceManual])
	}
	// Тот же каталог, названный вручную, остаётся каталогом профиля.
	byManual := Detect(env, []string{filepath.Join(roaming, "nekoray", "config")})
	if len(byManual) != 1 || !byManual[0].LooksRunning {
		t.Errorf("каталог профиля, указанный вручную: %+v", byManual)
	}
}
