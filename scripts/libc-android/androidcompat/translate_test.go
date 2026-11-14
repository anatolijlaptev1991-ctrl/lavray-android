// SPDX-License-Identifier: GPL-3.0-only

package androidcompat

import "testing"

// Android's app seccomp filter has no entry for any legacy call translated here.
func TestTranslateRewritesLegacyCalls(t *testing.T) {
	cases := []struct {
		name string
		n    uintptr
		in   [6]uintptr
		want uintptr
		args [6]uintptr
	}{
		{"open", sysOpen, [6]uintptr{10, 20, 30}, sysOpenat, [6]uintptr{atFdcwd, 10, 20, 30}},
		{"stat", sysStat, [6]uintptr{10, 20}, sysNewfstatat, [6]uintptr{atFdcwd, 10, 20, 0}},
		{"lstat", sysLstat, [6]uintptr{10, 20}, sysNewfstatat, [6]uintptr{atFdcwd, 10, 20, atSymlinkNofollow}},
		{"access", sysAccess, [6]uintptr{10, 4}, sysFaccessat, [6]uintptr{atFdcwd, 10, 4}},
		{"pipe", sysPipe, [6]uintptr{10}, sysPipe2, [6]uintptr{10, 0}},
		{"rename", sysRename, [6]uintptr{10, 20}, sysRenameat, [6]uintptr{atFdcwd, 10, atFdcwd, 20}},
		{"mkdir", sysMkdir, [6]uintptr{10, 0o700}, sysMkdirat, [6]uintptr{atFdcwd, 10, 0o700}},
		{"rmdir", sysRmdir, [6]uintptr{10}, sysUnlinkat, [6]uintptr{atFdcwd, 10, atRemovedir}},
		{"link", sysLink, [6]uintptr{10, 20}, sysLinkat, [6]uintptr{atFdcwd, 10, atFdcwd, 20, 0}},
		{"unlink", sysUnlink, [6]uintptr{10}, sysUnlinkat, [6]uintptr{atFdcwd, 10, 0}},
		{"symlink", sysSymlink, [6]uintptr{10, 20}, sysSymlinkat, [6]uintptr{10, atFdcwd, 20}},
		{"readlink", sysReadlink, [6]uintptr{10, 20, 30}, sysReadlinkat, [6]uintptr{atFdcwd, 10, 20, 30}},
		{"chmod", sysChmod, [6]uintptr{10, 0o600}, sysFchmodat, [6]uintptr{atFdcwd, 10, 0o600}},
		{"chown", sysChown, [6]uintptr{10, 1, 2}, sysFchownat, [6]uintptr{atFdcwd, 10, 1, 2, 0}},
		{"mknod", sysMknod, [6]uintptr{10, 0o600, 7}, sysMknodat, [6]uintptr{atFdcwd, 10, 0o600, 7}},
	}
	for _, c := range cases {
		gotN, gotA := Translate(c.n, c.in)
		if gotN != c.want || gotA != c.args {
			t.Errorf("%s: got (%d, %v), want (%d, %v)", c.name, gotN, gotA, c.want, c.args)
		}
	}
}

func TestTranslateKeepsOtherCalls(t *testing.T) {
	in := [6]uintptr{1, 2, 3, 4, 5, 6}
	for _, n := range []uintptr{0, 1, 5, 9, 202, sysOpenat, sysNewfstatat} {
		gotN, gotA := Translate(n, in)
		if gotN != n || gotA != in {
			t.Errorf("call %d changed: (%d, %v)", n, gotN, gotA)
		}
	}
}

func TestAtFdcwdIsMinusOneHundred(t *testing.T) {
	v := atFdcwd // a variable: the constant does not fit int64 at compile time
	if int64(v) != -100 {
		t.Fatalf("atFdcwd = %d", int64(v))
	}
}
