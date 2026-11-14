// SPDX-License-Identifier: GPL-3.0-only

//go:build linux && amd64

package libc // import "modernc.org/libc"

import (
	"golang.org/x/sys/unix"
	"modernc.org/libc/androidcompat"
)

func rawSyscall(trap, a1, a2, a3 uintptr) (uintptr, uintptr, unix.Errno) {
	return rawSyscall6(trap, a1, a2, a3, 0, 0, 0)
}

func rawSyscall6(trap, a1, a2, a3, a4, a5, a6 uintptr) (uintptr, uintptr, unix.Errno) {
	n, a := androidcompat.Translate(trap, [6]uintptr{a1, a2, a3, a4, a5, a6})
	return unix.Syscall6(n, a[0], a[1], a[2], a[3], a[4], a[5])
}
