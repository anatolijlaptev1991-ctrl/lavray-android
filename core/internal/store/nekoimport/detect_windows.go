// SPDX-License-Identifier: GPL-3.0-only

//go:build windows

package nekoimport

import (
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

// listProcesses перечисляет процессы nekobox.exe и nekoray.exe. Только чтение:
// снимок списка процессов и право PROCESS_QUERY_LIMITED_INFORMATION (имя образа);
// процесс не открывается на запись, не останавливается и не трогается иначе.
// Процесс с повышением права на имя образа не даёт — для него ExeDir пустой.
func listProcesses() ([]Process, error) { return listProcessesWhere(IsNekoProcess) }

// listProcessesWhere — то же для процессов, имя образа которых подходит match
// (тест ищет собственный процесс, не трогая чужие).
func listProcessesWhere(match func(name string) bool) ([]Process, error) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer func() { _ = windows.CloseHandle(snap) }()
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	var out []Process
	for err = windows.Process32First(snap, &entry); err == nil; err = windows.Process32Next(snap, &entry) {
		name := windows.UTF16ToString(entry.ExeFile[:])
		if match(name) {
			out = append(out, Process{Name: name, ExeDir: exeDir(entry.ProcessID)})
		}
	}
	return out, nil
}

// exeDir — каталог образа процесса; пусто, если доступа нет.
func exeDir(pid uint32) string {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer func() { _ = windows.CloseHandle(h) }()
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &n); err != nil {
		return ""
	}
	return filepath.Dir(windows.UTF16ToString(buf[:n]))
}
