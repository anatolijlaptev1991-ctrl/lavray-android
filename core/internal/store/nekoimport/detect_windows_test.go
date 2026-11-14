// SPDX-License-Identifier: GPL-3.0-only

//go:build windows

package nekoimport

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// listProcessesWhere проверяется на собственном процессе теста: чужие процессы
// (в том числе живой NekoBox разработчика) не перечисляются и не открываются.
// Снимок находит процесс по имени образа, а exeDir отдаёт каталог его exe.
func TestListProcessesFindsOwnProcess(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	own := filepath.Base(exe)
	list, err := listProcessesWhere(func(name string) bool { return strings.EqualFold(name, own) })
	if err != nil {
		t.Fatalf("listProcessesWhere: %v", err)
	}
	if len(list) == 0 {
		t.Fatalf("собственный процесс %q не найден в снимке", own)
	}
	for _, p := range list {
		if !strings.EqualFold(p.Name, own) {
			t.Errorf("в ответе посторонний процесс %q", p.Name)
		}
	}
	if got := exeDir(uint32(os.Getpid())); !strings.EqualFold(got, filepath.Dir(exe)) {
		t.Errorf("exeDir = %q, ожидалось %q", got, filepath.Dir(exe))
	}
}

// Без права на чтение пути образа (несуществующий pid) каталог пуст.
func TestExeDirOfUnknownProcessIsEmpty(t *testing.T) {
	if got := exeDir(0xFFFFFFF0); got != "" {
		t.Errorf("exeDir(несуществующий pid) = %q", got)
	}
}
