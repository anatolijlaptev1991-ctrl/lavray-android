// SPDX-License-Identifier: GPL-3.0-only

// Package androidcompat rewrites the legacy x86-64 system calls that Android's
// seccomp filter kills (SIGSYS) into their *at counterparts, which bionic itself
// uses and the filter allows. It is a copy-in for modernc.org/libc (see
// scripts/build-core.sh): the translated musl code in libc issues open, stat,
// lstat, access, mkdir and friends by their x86-64 numbers, which arm64 does not
// have and which Android never lets an app call.
package androidcompat

// Linux x86-64 system call numbers (the legacy calls and their replacements).
const (
	sysOpen     = 2
	sysStat     = 4
	sysLstat    = 6
	sysAccess   = 21
	sysPipe     = 22
	sysRename   = 82
	sysMkdir    = 83
	sysRmdir    = 84
	sysLink     = 86
	sysUnlink   = 87
	sysSymlink  = 88
	sysReadlink = 89
	sysChmod    = 90
	sysChown    = 92
	sysMknod    = 133

	sysOpenat     = 257
	sysMkdirat    = 258
	sysMknodat    = 259
	sysFchownat   = 260
	sysNewfstatat = 262
	sysUnlinkat   = 263
	sysRenameat   = 264
	sysLinkat     = 265
	sysSymlinkat  = 266
	sysReadlinkat = 267
	sysFchmodat   = 268
	sysFaccessat  = 269
	sysPipe2      = 293

	atFdcwd           = ^uintptr(99) // AT_FDCWD (-100) as an unsigned register value
	atSymlinkNofollow = 0x100
	atRemovedir       = 0x200
)

// Translate returns the system call to issue instead of n with arguments a. Calls
// that need no rewriting come back unchanged.
func Translate(n uintptr, a [6]uintptr) (uintptr, [6]uintptr) {
	switch n {
	case sysOpen:
		return sysOpenat, [6]uintptr{atFdcwd, a[0], a[1], a[2]}
	case sysStat:
		return sysNewfstatat, [6]uintptr{atFdcwd, a[0], a[1], 0}
	case sysLstat:
		return sysNewfstatat, [6]uintptr{atFdcwd, a[0], a[1], atSymlinkNofollow}
	case sysAccess:
		return sysFaccessat, [6]uintptr{atFdcwd, a[0], a[1]}
	case sysPipe:
		return sysPipe2, [6]uintptr{a[0], 0}
	case sysRename:
		return sysRenameat, [6]uintptr{atFdcwd, a[0], atFdcwd, a[1]}
	case sysMkdir:
		return sysMkdirat, [6]uintptr{atFdcwd, a[0], a[1]}
	case sysRmdir:
		return sysUnlinkat, [6]uintptr{atFdcwd, a[0], atRemovedir}
	case sysLink:
		return sysLinkat, [6]uintptr{atFdcwd, a[0], atFdcwd, a[1], 0}
	case sysUnlink:
		return sysUnlinkat, [6]uintptr{atFdcwd, a[0], 0}
	case sysSymlink:
		return sysSymlinkat, [6]uintptr{a[0], atFdcwd, a[1]}
	case sysReadlink:
		return sysReadlinkat, [6]uintptr{atFdcwd, a[0], a[1], a[2]}
	case sysChmod:
		return sysFchmodat, [6]uintptr{atFdcwd, a[0], a[1]}
	case sysChown:
		return sysFchownat, [6]uintptr{atFdcwd, a[0], a[1], a[2], 0}
	case sysMknod:
		return sysMknodat, [6]uintptr{atFdcwd, a[0], a[1], a[2]}
	}
	return n, a
}
