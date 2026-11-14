// SPDX-License-Identifier: GPL-3.0-only

package updates

import "testing"

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.2.0", "1.2.0", 0},
		{"1.2", "1.2.0", 0},
		{"1.10.1", "1.2.0", 1},
		{"0.5.0", "0.5.1", -1},
		{"0.5.1-rc.1", "0.5.1", -1},     // пре-релиз младше релиза
		{"0.5.1-rc.2", "0.5.1-rc.1", 1}, // номер пре-релиза числовой
		{"0.5.1-rc.10", "0.5.1-rc.9", 1},
		{"0.5.0", "0.5.1-rc.1", -1},
		{"0.5.1-beta", "0.5.1-rc", -1}, // не числовой идентификатор — лексически
		{"0.5.1+build7", "0.5.1", 0},   // сборочные метаданные игнорируются
		{"v0.5.1", "0.5.1", 0},
		{"", "0.0.0", 0},
	}
	for _, c := range cases {
		if got := CompareVersions(c.a, c.b); got != c.want {
			t.Errorf("CompareVersions(%q,%q)=%d, ожидалось %d", c.a, c.b, got, c.want)
		}
		if got := CompareVersions(c.b, c.a); got != -c.want {
			t.Errorf("CompareVersions(%q,%q)=%d, ожидалось %d (симметрия)", c.b, c.a, got, -c.want)
		}
	}
}

func TestCompareCoreVersions(t *testing.T) {
	if CompareCoreVersions("1.14.1-lav.2", "1.14.1-lav.10") >= 0 {
		t.Error("lav.2 должна быть младше lav.10")
	}
	if CompareCoreVersions("1.14.2-lav.1", "1.14.1-lav.9") <= 0 {
		t.Error("база важнее lav-суффикса")
	}
	if CompareCoreVersions("1.14.1-lav.3", "1.14.1-lav.3") != 0 {
		t.Error("равные версии")
	}
}
