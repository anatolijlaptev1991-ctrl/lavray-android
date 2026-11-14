// SPDX-License-Identifier: GPL-3.0-only

//go:build linux && !amd64

package libc // import "modernc.org/libc"

import "golang.org/x/sys/unix"

func rawSyscall(trap, a1, a2, a3 uintptr) (uintptr, uintptr, unix.Errno) {
	return unix.Syscall(trap, a1, a2, a3)
}

func rawSyscall6(trap, a1, a2, a3, a4, a5, a6 uintptr) (uintptr, uintptr, unix.Errno) {
	return unix.Syscall6(trap, a1, a2, a3, a4, a5, a6)
}
