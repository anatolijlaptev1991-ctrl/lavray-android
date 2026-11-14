// SPDX-License-Identifier: GPL-3.0-only

package model

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repoRoot — корень репозитория: тест лежит в core/internal/model.
func repoRoot(t *testing.T) string {
	t.Helper()
	root := filepath.Join("..", "..", "..")
	if _, err := os.Stat(filepath.Join(root, "core", "internal", "model", "settings.go")); err != nil {
		t.Fatalf("корень репозитория не найден (%s): %v", root, err)
	}
	return root
}

// Каждый ключ каталога настроек имеет читателя: файл, где строка ключа
// действительно есть, либо честную пометку import-only с причиной. Ключ без
// читателя больше не может тихо присутствовать в каталоге («настройки-обманки»).
func TestEverySettingHasReader(t *testing.T) {
	root := repoRoot(t)
	readers := SettingReaders()
	for _, d := range SettingDescriptors() {
		r, ok := readers[d.Key]
		if !ok {
			t.Errorf("%s: нет записи в SettingReaders (укажите читателя или import-only с причиной)", d.Key)
			continue
		}
		if r.Kind == ReaderImportOnly {
			if strings.TrimSpace(r.Reason) == "" || r.File != "" {
				t.Errorf("%s: import-only требует причину и не имеет файла (reason=%q file=%q)", d.Key, r.Reason, r.File)
			}
			continue
		}
		checkReaderFile(t, root, d.Key, r)
	}
}

// checkReaderFile: файл читателя существует, это не тест, лежит в слое своего
// вида и содержит строку ключа.
func checkReaderFile(t *testing.T, root, key string, r SettingReader) {
	t.Helper()
	if r.Kind != ReaderCore && r.Kind != ReaderShell && r.Kind != ReaderUI {
		t.Errorf("%s: неизвестный вид читателя %q", key, r.Kind)
		return
	}
	if !strings.HasPrefix(r.File, string(r.Kind)+"/") {
		t.Errorf("%s: файл %q не в каталоге %s/", key, r.File, r.Kind)
	}
	if strings.HasSuffix(r.File, "_test.go") || strings.Contains(r.File, ".test.") || strings.HasSuffix(r.File, "/tests.rs") {
		t.Errorf("%s: читатель %q — тест", key, r.File)
	}
	// Слои shell и ui — настольные (Tauri и веб-интерфейс), в LAVRay Android их нет:
	// здесь проверяются только читатели в ядре.
	if r.Kind != ReaderCore {
		return
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(r.File)))
	if err != nil {
		t.Errorf("%s: файл читателя не читается: %v", key, err)
		return
	}
	if !strings.Contains(string(data), key) {
		t.Errorf("%s: строки ключа нет в %s", key, r.File)
	}
}

// В таблице нет записей для ключей, которых нет в каталоге.
func TestSettingReadersHaveNoStaleKeys(t *testing.T) {
	known := map[string]bool{}
	for _, d := range SettingDescriptors() {
		known[d.Key] = true
	}
	for key := range SettingReaders() {
		if !known[key] {
			t.Errorf("%s: запись в SettingReaders без ключа в каталоге", key)
		}
	}
}

// Читатели, на которые опираются хвосты A, названы верно.
func TestSettingReadersTailsA(t *testing.T) {
	r := SettingReaders()
	want := map[string]ReaderKind{
		"stats.interval_ms":      ReaderCore,
		"share.vmess_old_format": ReaderCore,
		"ui.max_log_lines":       ReaderUI,
		"win.sp_format":          ReaderImportOnly,
	}
	for key, kind := range want {
		if r[key].Kind != kind {
			t.Errorf("%s: вид %q, want %q", key, r[key].Kind, kind)
		}
	}
}
