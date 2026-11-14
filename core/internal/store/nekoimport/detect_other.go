// SPDX-License-Identifier: GPL-3.0-only

//go:build !windows

package nekoimport

// listProcesses — NekoBox ищется по процессам только на Windows; на других
// системах работают ручной выбор пути и каталоги профиля.
func listProcesses() ([]Process, error) { return nil, nil }
