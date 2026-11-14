// SPDX-License-Identifier: GPL-3.0-only

package api

// status_settings_test.go — GetStatus не строит полный BuildSettings: читает
// только шесть значений ответа (раньше — 51 SQL-чтение), а ответ тот же.

import (
	"sync/atomic"
	"testing"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"google.golang.org/protobuf/proto"
)

// legacyInboundTun — прежнее построение Inbound/Tun через полный buildSettings.
func legacyInboundTun(s *Server) (*lavrayv1.InboundInfo, *lavrayv1.TunInfo) {
	inb := s.buildSettings(false).Inbound
	in := &lavrayv1.InboundInfo{
		Listen:      inb.Address,
		Port:        int32(inb.Port),
		AuthEnabled: inb.AuthUser != "" && inb.AuthPass != "",
	}
	tun := s.buildSettings(false).Tun
	ti := &lavrayv1.TunInfo{Enabled: tun.Enabled}
	if tun.InterfaceName != "" {
		ti.InterfaceName = strp(tun.InterfaceName)
	}
	return in, ti
}

func TestCoreStatusSameAsLegacy(t *testing.T) {
	cases := map[string]map[string]string{
		"умолчания": {},
		"заданные": {
			"inbound.address": `"0.0.0.0"`, "inbound.port": `8080`,
			"inbound.auth.user": `"u"`, "inbound.auth.pass": `"p"`,
			"tun.interface_name": `"lav0"`, "tun.enabled": `true`,
		},
		"без пароля":       {"inbound.auth.user": `"u"`, "inbound.auth.pass": `""`},
		"битый JSON":       {"inbound.port": `"не число"`, "inbound.address": `12`, "tun.interface_name": `{`},
		"пустые строки":    {"inbound.address": `""`, "tun.interface_name": `""`},
		"пресет и dns":     {"routing.active_preset": `"lav-ru"`, "routing.dns_override": `{"x":1}`},
		"порт вне нормы":   {"inbound.port": `-5`},
		"интерфейс пустой": {"tun.interface_name": `null`},
	}
	for name, kv := range cases {
		t.Run(name, func(t *testing.T) {
			s, st := subAutoTestServer(t)
			for k, v := range kv {
				if err := st.SetSetting(k, v); err != nil {
					t.Fatal(err)
				}
			}
			cs := s.coreStatus()
			in, ti := legacyInboundTun(s)
			if !proto.Equal(cs.Inbound, in) {
				t.Errorf("inbound: %v, было %v", cs.Inbound, in)
			}
			if !proto.Equal(cs.Tun, ti) {
				t.Errorf("tun: %v, было %v", cs.Tun, ti)
			}
		})
	}
}

func TestCoreStatusReadBudget(t *testing.T) {
	s, _ := subAutoTestServer(t)
	var reads atomic.Int32
	s.onSettingRead = func(string) { reads.Add(1) }
	s.coreStatus()
	if n := reads.Load(); n > 6 {
		t.Fatalf("GetStatus прочёл настройки %d раз, допустимо не больше 6", n)
	}
}
