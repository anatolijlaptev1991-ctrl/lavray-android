// SPDX-License-Identifier: GPL-3.0-only

package nekoimport

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func writeProfile(t *testing.T, dir string, id int, pad int) {
	t.Helper()
	body := `{"type":"socks","id":` + strconv.Itoa(id) + `,"gid":0,"bean":{"name":"n","addr":"1.1.1.1","port":1,"v":5,"info":"` +
		strings.Repeat("x", pad) + `"}}`
	write(t, filepath.Join(dir, "profiles", strconv.Itoa(id)+".json"), body)
}

// Границы: файл ровно в предел принимается, на байт больше — пропускается с
// причиной; записей ровно в предел можно, на одну больше — ErrLimits (B).
func TestFileSizeAndCountCaps(t *testing.T) {
	dir := mkTempDir(t)
	write(t, filepath.Join(dir, "groups", "0.json"), `{"id":0,"name":"G","front_proxy_id":-1}`)
	writeProfile(t, dir, 1, 0)
	fi, _ := os.Stat(filepath.Join(dir, "profiles", "1.json"))
	limit := fi.Size()
	writeProfile(t, dir, 2, 1) // на один байт больше

	plan, err := scanWith(dir, Limits{MaxFiles: 3, MaxFileBytes: limit, MaxTotalBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Profiles) != 1 || len(plan.Report.Skipped) != 1 || !strings.Contains(plan.Report.Skipped[0].Reason, "larger") {
		t.Fatalf("профили %d, пропуски %+v", len(plan.Profiles), plan.Report.Skipped)
	}

	// Записей 3 (1 группа + 2 профиля) при пределе 3 — можно; при пределе 2 — ErrLimits.
	if _, err := scanWith(dir, Limits{MaxFiles: 2, MaxFileBytes: limit, MaxTotalBytes: 1 << 20}); !errors.Is(err, ErrLimits) {
		t.Errorf("предел числа файлов: ошибка %v", err)
	}
	// Общий размер.
	if _, err := scanWith(dir, Limits{MaxFiles: 10, MaxFileBytes: 1 << 20, MaxTotalBytes: limit}); !errors.Is(err, ErrLimits) {
		t.Errorf("предел общего размера: ошибка %v", err)
	}
	// Боевые пределы сохранены.
	if DefaultLimits.MaxFiles != 20000 || DefaultLimits.MaxFileBytes != 2<<20 || DefaultLimits.MaxTotalBytes != 64<<20 {
		t.Errorf("DefaultLimits изменены: %+v", DefaultLimits)
	}
}

// Не обычный файл (здесь каталог с именем 1.json) не читается и не роняет разбор (S).
func TestNonRegularFileSkipped(t *testing.T) {
	dir := mkTempDir(t)
	write(t, filepath.Join(dir, "groups", "0.json"), `{"id":0,"name":"G","front_proxy_id":-1}`)
	if err := os.MkdirAll(filepath.Join(dir, "profiles", "1.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	plan, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Profiles) != 0 || len(plan.Report.Skipped) != 1 || !strings.Contains(plan.Report.Skipped[0].Reason, "not a regular file") {
		t.Errorf("пропуски %+v", plan.Report.Skipped)
	}
}

// Символическая ссылка не читается, даже если ведёт на годный файл (S).
func TestSymlinkNotFollowed(t *testing.T) {
	dir := mkTempDir(t)
	outside := mkTempDir(t)
	write(t, filepath.Join(dir, "groups", "0.json"), `{"id":0,"name":"G","front_proxy_id":-1}`)
	write(t, filepath.Join(outside, "secret.json"),
		`{"type":"socks","id":1,"gid":0,"bean":{"name":"leak","addr":"1.1.1.1","port":1,"v":5}}`)
	if err := os.MkdirAll(filepath.Join(dir, "profiles"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.json"), filepath.Join(dir, "profiles", "1.json")); err != nil {
		t.Skipf("символические ссылки недоступны на этой системе: %v", err)
	}
	plan, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Profiles) != 0 {
		t.Fatalf("ссылка прочитана: %+v", plan.Profiles)
	}
	if len(plan.Report.Skipped) != 1 || !strings.Contains(plan.Report.Skipped[0].Reason, "symlink") {
		t.Errorf("причина пропуска: %+v", plan.Report.Skipped)
	}

	// Подкаталог-ссылка (profiles → чужой каталог) тоже не обходится.
	linked := mkTempDir(t)
	write(t, filepath.Join(linked, "groups", "0.json"), `{"id":0,"name":"G","front_proxy_id":-1}`)
	write(t, filepath.Join(outside, "p", "2.json"),
		`{"type":"socks","id":2,"gid":0,"bean":{"name":"leak2","addr":"1.1.1.1","port":1,"v":5}}`)
	if err := os.Symlink(filepath.Join(outside, "p"), filepath.Join(linked, "profiles")); err != nil {
		t.Skipf("символические ссылки каталогов недоступны: %v", err)
	}
	plan, err = Scan(linked)
	if err != nil || len(plan.Profiles) != 0 {
		t.Errorf("каталог-ссылка прочитан: %+v %v", plan.Profiles, err)
	}
}

// Из имён берётся только базовое имя записи каталога: «..» в имени файла не
// существует, а нечисловые имена игнорируются (S).
func TestOnlyBaseNamesAreUsed(t *testing.T) {
	files := intJSONFiles([]string{"..", "../1.json", "1.json", "x.json", "-3.json", "pm.json", "007.json"})
	if len(files) != 2 || files[0].id != 1 || files[1].id != 7 || files[1].name != "007.json" {
		t.Errorf("файлы %+v", files)
	}
}
