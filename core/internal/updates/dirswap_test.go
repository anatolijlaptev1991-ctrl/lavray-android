// SPDX-License-Identifier: GPL-3.0-only

package updates

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeTree(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

// failRenameOn подменяет переименование: n-й вызов (с 1) падает.
func failRenameOn(t *testing.T, n int) {
	t.Helper()
	orig, calls := renameDir, 0
	renameDir = func(a, b string) error {
		calls++
		if calls == n {
			return errors.New("инъекция: каталог занят")
		}
		return orig(a, b)
	}
	t.Cleanup(func() { renameDir = orig })
}

func newTree(t *testing.T) (cur, next string) {
	t.Helper()
	root := t.TempDir()
	cur = filepath.Join(root, "geo")
	writeTree(t, cur, map[string]string{"a.txt": "old"})
	next, err := NewSiblingDir(cur)
	if err != nil {
		t.Fatal(err)
	}
	writeTree(t, next, map[string]string{"a.txt": "new"})
	return cur, next
}

func TestSwapDirKeepsPrevious(t *testing.T) {
	cur, next := newTree(t)
	if err := SwapDir(cur, next); err != nil {
		t.Fatal(err)
	}
	if readFile(t, filepath.Join(cur, "a.txt")) != "new" || readFile(t, filepath.Join(PrevDir(cur), "a.txt")) != "old" {
		t.Fatal("после замены активна новая ревизия, прошлая лежит в .prev")
	}
	if exists(next) {
		t.Fatal("временный каталог должен исчезнуть")
	}
}

func TestSwapDirKeepsOnlyOnePrevious(t *testing.T) {
	cur, next := newTree(t)
	if err := SwapDir(cur, next); err != nil {
		t.Fatal(err)
	}
	next2, _ := NewSiblingDir(cur)
	writeTree(t, next2, map[string]string{"a.txt": "newer"})
	if err := SwapDir(cur, next2); err != nil {
		t.Fatal(err)
	}
	if readFile(t, filepath.Join(PrevDir(cur), "a.txt")) != "new" {
		t.Fatal("в .prev одна, самая свежая прошлая ревизия")
	}
}

func TestSwapDirWithoutCurrent(t *testing.T) {
	root := t.TempDir()
	cur := filepath.Join(root, "override")
	next, _ := NewSiblingDir(cur)
	writeTree(t, next, map[string]string{"x": "1"})
	if err := SwapDir(cur, next); err != nil {
		t.Fatal(err)
	}
	if !exists(filepath.Join(cur, "x")) || exists(PrevDir(cur)) {
		t.Fatal("нет активного каталога: новый встаёт на место, .prev не появляется")
	}
}

// R: сбой последнего переименования возвращает прежнюю ревизию на место, а старая .prev остаётся.
func TestSwapDirRestoresOnFailure(t *testing.T) {
	cur, next := newTree(t)
	writeTree(t, PrevDir(cur), map[string]string{"a.txt": "older"})
	// вызовы: 1 — prev→discard, 2 — cur→prev, 3 — next→cur (падает)
	failRenameOn(t, 3)
	if err := SwapDir(cur, next); err == nil {
		t.Fatal("ожидалась ошибка")
	}
	if readFile(t, filepath.Join(cur, "a.txt")) != "old" {
		t.Fatal("активная ревизия должна остаться прежней")
	}
	if readFile(t, filepath.Join(PrevDir(cur), "a.txt")) != "older" {
		t.Fatal("прошлая ревизия .prev должна вернуться на место")
	}
	if !exists(next) {
		t.Fatal("подготовленный каталог остаётся вызывающему для уборки")
	}
}

func TestSwapDirFailureOnFirstRename(t *testing.T) {
	cur, next := newTree(t)
	failRenameOn(t, 1) // без прошлой ревизии первое переименование — cur→prev
	if err := SwapDir(cur, next); err == nil {
		t.Fatal("ожидалась ошибка")
	}
	if readFile(t, filepath.Join(cur, "a.txt")) != "old" || exists(PrevDir(cur)) {
		t.Fatal("состояние не изменилось")
	}
}

func TestSwapDirFailureWhenMovingOldPrev(t *testing.T) {
	cur, next := newTree(t)
	writeTree(t, PrevDir(cur), map[string]string{"a.txt": "older"})
	failRenameOn(t, 1) // prev→discard
	if err := SwapDir(cur, next); !errors.Is(err, ErrDirBusy) {
		t.Fatalf("ожидалась ErrDirBusy: %v", err)
	}
	if readFile(t, filepath.Join(PrevDir(cur), "a.txt")) != "older" || readFile(t, filepath.Join(cur, "a.txt")) != "old" {
		t.Fatal("состояние не изменилось")
	}
}

func TestRollbackDirRestoresPrevious(t *testing.T) {
	cur, next := newTree(t)
	if err := SwapDir(cur, next); err != nil {
		t.Fatal(err)
	}
	if err := RollbackDir(cur); err != nil {
		t.Fatal(err)
	}
	if readFile(t, filepath.Join(cur, "a.txt")) != "old" || exists(PrevDir(cur)) {
		t.Fatal("после отката активна прошлая ревизия, .prev пуст")
	}
	if err := RollbackDir(cur); !errors.Is(err, ErrNoPrevious) {
		t.Fatalf("повторный откат: %v", err)
	}
}

func TestRollbackDirFailureKeepsState(t *testing.T) {
	cur, next := newTree(t)
	_ = SwapDir(cur, next)
	failRenameOn(t, 2) // 1 — cur→aside, 2 — prev→cur (падает)
	if err := RollbackDir(cur); err == nil {
		t.Fatal("ожидалась ошибка")
	}
	if readFile(t, filepath.Join(cur, "a.txt")) != "new" || readFile(t, filepath.Join(PrevDir(cur), "a.txt")) != "old" {
		t.Fatal("состояние не изменилось")
	}
}

func TestRollbackDirFirstRenameFails(t *testing.T) {
	cur, next := newTree(t)
	_ = SwapDir(cur, next)
	failRenameOn(t, 1)
	if err := RollbackDir(cur); !errors.Is(err, ErrDirBusy) {
		t.Fatalf("ожидалась ErrDirBusy: %v", err)
	}
	if readFile(t, filepath.Join(cur, "a.txt")) != "new" {
		t.Fatal("состояние не изменилось")
	}
}

func TestCleanSwapLeftovers(t *testing.T) {
	cur, _ := newTree(t)
	stale, _ := NewSiblingDir(cur)
	writeTree(t, stale, map[string]string{"x": "1"})
	CleanSwapLeftovers(cur)
	if exists(stale) {
		t.Fatal("остатки прерванной замены убираются")
	}
	if !exists(cur) {
		t.Fatal("активный каталог не трогается")
	}
}
