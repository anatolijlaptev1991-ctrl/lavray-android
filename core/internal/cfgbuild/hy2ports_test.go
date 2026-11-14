// SPDX-License-Identifier: GPL-3.0-only

package cfgbuild

import (
	"errors"
	"reflect"
	"testing"

	"connectrpc.com/connect"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

func TestHysteria2ServerPorts(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"   ", nil},
		{"8443", []string{"8443:8443"}},
		{"1000:2000", []string{"1000:2000"}},
		{"1000-2000", []string{"1000:2000"}},
		{" 1000 : 2000 ", []string{"1000:2000"}},
		{"1000:2000,3000:4000", []string{"1000:2000", "3000:4000"}},
		{"443, 5000-6000 ,7000", []string{"443:443", "5000:6000", "7000:7000"}},
		{"1:65535", []string{"1:65535"}},
	}
	for _, c := range cases {
		got, err := hysteria2ServerPorts(c.in)
		if err != nil {
			t.Errorf("%q: неожиданная ошибка: %v", c.in, err)
			continue
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%q: получили %v, ждали %v", c.in, got, c.want)
		}
	}
}

func TestHysteria2ServerPortsGarbage(t *testing.T) {
	for _, in := range []string{
		"abc", "0", "65536", "-1", "1000:", ":2000", "2000:1000", "1000:2000:3000",
		"1000,,2000", "1000,", ",1000", "10 00", "+80", "1e3", "1000:abc",
	} {
		got, err := hysteria2ServerPorts(in)
		if err == nil {
			t.Errorf("%q: ждали ошибку, получили %v", in, got)
			continue
		}
		var e *Error
		if !errors.As(err, &e) || e.Code != connect.CodeInvalidArgument {
			t.Errorf("%q: ждали *Error INVALID_ARGUMENT, получили %T %v", in, err, err)
		}
	}
}

func TestBuildHysteria2ServerPortsList(t *testing.T) {
	p := &model.Profile{
		Type: model.TypeHysteria2, Address: "hy2.example.org", Port: 8443,
		Detail: &model.Hysteria2Detail{Password: "x", ServerPorts: "8443", HopInterval: "30s"},
	}
	out, err := buildHysteria2(p)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := out["server_ports"].([]string); !ok || !reflect.DeepEqual(got, []string{"8443:8443"}) {
		t.Fatalf("server_ports = %#v, ждали []string{\"8443:8443\"}", out["server_ports"])
	}
	if out["hop_interval"] != "30s" {
		t.Fatalf("hop_interval = %v", out["hop_interval"])
	}

	p.Detail = &model.Hysteria2Detail{Password: "x", ServerPorts: "oops"}
	if _, err := buildHysteria2(p); err == nil {
		t.Fatal("мусорный server_ports должен давать ошибку")
	}

	p.Detail = &model.Hysteria2Detail{Password: "x", HopInterval: "30s"}
	out, err = buildHysteria2(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, has := out["server_ports"]; has {
		t.Fatal("без server_ports ключ не эмитится")
	}
	if _, has := out["hop_interval"]; has {
		t.Fatal("hop_interval без server_ports не эмитится")
	}
}
