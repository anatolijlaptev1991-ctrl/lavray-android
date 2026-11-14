// SPDX-License-Identifier: GPL-3.0-only

package web

import "testing"

// TestBindNonLoopbackRequiresFlag — не-loopback адрес (и 0.0.0.0 тем более)
// допускается только с --web-allow-remote (S).
func TestBindNonLoopbackRequiresFlag(t *testing.T) {
	cases := []struct {
		addr        string
		okLocal     bool // без флага
		okWithFlag  bool
		description string
	}{
		{"127.0.0.1:8080", true, true, "loopback v4"},
		{"127.0.0.1:0", true, true, "loopback, порт 0"},
		{"[::1]:8080", true, true, "loopback v6"},
		{"localhost:8080", true, true, "localhost"},
		{"192.168.1.1:8443", false, true, "LAN-адрес роутера"},
		{"0.0.0.0:8080", false, true, "все интерфейсы v4"},
		{":8080", false, true, "пустой хост = все интерфейсы"},
		{"[::]:8080", false, true, "все интерфейсы v6"},
		{"example.com:80", false, false, "имя, не IP"},
		{"127.0.0.1", false, false, "нет порта"},
		{"127.0.0.1:99999", false, false, "порт вне диапазона"},
		{"127.0.0.1:abc", false, false, "порт не число"},
		{"", false, false, "пусто"},
	}
	for _, c := range cases {
		if err := CheckBind(c.addr, false); (err == nil) != c.okLocal {
			t.Errorf("%s (%q) без флага: err=%v, ждали ok=%v", c.description, c.addr, err, c.okLocal)
		}
		if err := CheckBind(c.addr, true); (err == nil) != c.okWithFlag {
			t.Errorf("%s (%q) с флагом: err=%v, ждали ok=%v", c.description, c.addr, err, c.okWithFlag)
		}
	}
}

func TestIsLoopbackBind(t *testing.T) {
	if !IsLoopbackBind("127.0.0.1:1") || !IsLoopbackBind("localhost:1") || !IsLoopbackBind("[::1]:1") {
		t.Fatal("loopback не распознан")
	}
	if IsLoopbackBind("192.168.1.1:1") || IsLoopbackBind("0.0.0.0:1") || IsLoopbackBind("garbage") {
		t.Fatal("не-loopback принят за loopback")
	}
}
