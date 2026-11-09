// SPDX-License-Identifier: GPL-3.0-only

//go:build singbox_run

package mobile

import (
	"context"
	"testing"

	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/include"
	"github.com/sagernet/sing-box/option"
	sbjson "github.com/sagernet/sing/common/json"
	"github.com/sagernet/sing/service"

	"github.com/anatolijlaptev1991-ctrl/lavray/core/internal/inst"
)

// Конфиг в духе cfgbuild для режима TUN: mixed-in и правило process_name,
// которые должен убрать патч, и два сервера local.
const smokeConfig = `{
 "log": {"level": "info"},
 "dns": {"servers": [{"tag":"dns-direct","type":"local"},{"tag":"dns-local","type":"local"}],
         "rules": [{"domain_suffix":[".lan"],"action":"predefined","rcode":0}]},
 "inbounds": [
  {"tag":"mixed-in","type":"mixed","listen":"127.0.0.1","listen_port":2080},
  {"tag":"tun-in","type":"tun","interface_name":"lavray-tun","address":["172.18.0.1/30"],"auto_route":true,"mtu":9000}
 ],
 "outbounds": [{"tag":"proxy","type":"direct"},{"tag":"block","type":"block"}],
 "route": {
  "rules": [
   {"inbound":["mixed-in","tun-in"],"action":"sniff"},
   {"process_name":["chrome.exe"],"outbound":"block"},
   {"domain":["x.test"],"outbound":"proxy"}
  ],
  "auto_detect_interface": true,
  "default_domain_resolver": {"server":"dns-direct"},
  "final": "proxy"
 }
}`

// Патченный конфиг принимается sing-box с платформой Android, а транспорт
// local заменён системным (иначе он читал бы /etc/resolv.conf).
func TestPatchedConfigBuildsWithPlatform(t *testing.T) {
	patched, rep, err := patchConfig([]byte(smokeConfig), tunParams{Stack: "system", MTU: 1500})
	if err != nil {
		t.Fatal(err)
	}
	if rep.DroppedRules != 1 || len(rep.RemovedInbounds) != 1 {
		t.Fatalf("отчёт патча: %+v", rep)
	}
	h := newHost(fakePlatform{})
	ctx := include.Context(h.boxContext(context.Background()))
	var options option.Options
	if err := sbjson.UnmarshalContext(ctx, patched, &options); err != nil {
		t.Fatalf("разбор: %v", err)
	}
	instance, err := box.New(box.Options{Context: ctx, Options: options})
	if err != nil {
		t.Fatalf("box.New: %v", err)
	}
	defer instance.Close()
	tr, ok := service.FromContext[adapter.DNSTransportManager](ctx).Transport("dns-local")
	if !ok {
		t.Fatal("нет транспорта dns-local")
	}
	if _, isSystem := tr.(*systemTransport); !isSystem {
		t.Errorf("dns-local имеет тип %T, ждали *systemTransport", tr)
	}
}

// Раннер сохраняет необязательные интерфейсы, которые проверяет ядро.
func TestRunnerKeepsOptionalInterfaces(t *testing.T) {
	var r inst.Runner = newRunner(newHost(fakePlatform{}))
	if _, ok := r.(inst.ProxyDialer); !ok {
		t.Error("нет inst.ProxyDialer")
	}
	if _, ok := r.(inst.LogSinker); !ok {
		t.Error("нет inst.LogSinker")
	}
	if _, ok := r.(inst.TrafficProvider); !ok {
		t.Error("нет inst.TrafficProvider")
	}
}

// Режим без tun-inbound на телефоне невозможен: Start возвращает errNoTun.
func TestRunnerRejectsConfigWithoutTun(t *testing.T) {
	r := newRunner(newHost(fakePlatform{}))
	cfg := `{"inbounds":[{"tag":"mixed-in","type":"mixed","listen":"127.0.0.1","listen_port":2080}]}`
	if err := r.Start(context.Background(), []byte(cfg)); err == nil {
		t.Fatal("ждали ошибку")
	}
}
