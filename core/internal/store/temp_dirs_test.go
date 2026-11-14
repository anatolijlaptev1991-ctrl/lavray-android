// SPDX-License-Identifier: GPL-3.0-only

package store

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// leftover создаёт «каталог упавшего процесса» с расшифрованным снимком.
func leftover(t *testing.T, root, name string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "snapshot.db"), []byte("PLAINTEXT-SECRET"), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func fastRemove(t *testing.T) {
	t.Helper()
	oldTries, oldDelay, oldRemove := removeTries, removeDelay, removeAll
	removeTries, removeDelay = 3, time.Millisecond
	t.Cleanup(func() { removeTries, removeDelay, removeAll = oldTries, oldDelay, oldRemove })
}

func TestSweepStaleTempDirs(t *testing.T) {
	root := mkTempDir(t)
	leftover(t, root, "backup-111")
	leftover(t, root, "restore-222")
	keep := leftover(t, root, "geo") // чужой каталог данных
	keepFile := filepath.Join(root, "backup-notadir")
	if err := os.WriteFile(keepFile, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	res := SweepStaleTempDirs(root)
	sort.Strings(res.Removed)
	if strings.Join(res.Removed, ",") != "backup-111,restore-222" || len(res.Failed) != 0 {
		t.Fatalf("итог подметания: %+v", res)
	}
	for _, gone := range []string{"backup-111", "restore-222"} {
		if _, err := os.Stat(filepath.Join(root, gone)); err == nil {
			t.Errorf("%s остался", gone)
		}
	}
	for _, kept := range []string{keep, keepFile} {
		if _, err := os.Stat(kept); err != nil {
			t.Errorf("подмели лишнее: %v", err)
		}
	}
	if res := SweepStaleTempDirs(root); len(res.Removed) != 0 || len(res.Failed) != 0 {
		t.Errorf("второй проход: %+v", res)
	}
}

func TestSweepReportsFailures(t *testing.T) {
	fastRemove(t)
	root := mkTempDir(t)
	leftover(t, root, "backup-stuck")
	removeAll = func(string) error { return errors.New("Access is denied") }

	res := SweepStaleTempDirs(root)
	if len(res.Removed) != 0 || len(res.Failed) != 1 {
		t.Fatalf("итог: %+v", res)
	}
	msg := res.Failed[0].Error()
	if !strings.Contains(msg, "backup-stuck") || !strings.Contains(msg, "Access is denied") {
		t.Errorf("ошибка без имени каталога или причины: %q", msg)
	}
	if strings.Contains(msg, root) {
		t.Errorf("в ошибке полный путь: %q", msg)
	}
}

func TestRemoveTempDirRetriesThenSucceeds(t *testing.T) {
	fastRemove(t)
	dir := leftover(t, mkTempDir(t), "backup-flaky")
	calls := 0
	real := os.RemoveAll
	removeAll = func(p string) error {
		calls++
		if calls < 3 {
			return errors.New("занято")
		}
		return real(p)
	}
	if err := RemoveTempDir(dir); err != nil {
		t.Fatalf("RemoveTempDir: %v", err)
	}
	if calls != 3 {
		t.Errorf("попыток %d, ждали 3", calls)
	}
}

// TestRestoreWarnsWhenTempDirCannotBeRemoved — при сбое удаления Restore не
// падает, а предупреждает; каталог подметёт следующий запуск.
func TestRestoreWarnsWhenTempDirCannotBeRemoved(t *testing.T) {
	fastRemove(t)
	_, dst, snap := pair(t)
	var warns []string
	dst.SetWarnLogger(func(format string, args ...any) { warns = append(warns, strings.TrimSpace(format)) })
	real := os.RemoveAll
	removeAll = func(string) error { return errors.New("Access is denied") }
	if err := dst.Restore(bg, snap); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if len(warns) != 1 {
		t.Fatalf("предупреждения: %q", warns)
	}
	removeAll = real
	if res := SweepStaleTempDirs(dst.Dir()); len(res.Removed) != 1 || !strings.HasPrefix(res.Removed[0], TempDirRestore) {
		t.Errorf("следующий запуск не подмёл каталог: %+v", res)
	}
}
