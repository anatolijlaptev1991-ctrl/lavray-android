// SPDX-License-Identifier: GPL-3.0-only

package cfgbuild

import (
	"reflect"
	"testing"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

// proxyTLSALPN собирает конфиг профиля и возвращает tls.alpn исходящего proxy
// (nil — ключа нет) и признак наличия блока tls.
func proxyTLSALPN(t *testing.T, p *model.Profile) ([]any, bool) {
	t.Helper()
	res, err := Build(BuildRequest{Profile: p})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	tls, ok := outboundByTag(t, res, "proxy")["tls"].(map[string]any)
	if !ok {
		return nil, false
	}
	alpn, _ := tls["alpn"].([]any)
	return alpn, true
}

func vlessWithStream(st *model.StreamSettings) *model.Profile {
	return &model.Profile{
		ID: 1, Type: model.TypeVLESS, Address: "vl.example.org", Port: 443,
		Detail: &model.TrojanVlessDetail{Password: "b831381d-6324-4d53-ad4f-8cda48b30811"},
		Stream: st,
	}
}

// TestALPNByTransport — ALPN из ссылки приводится к транспорту (0.2.1): все
// ветви alpnForTransport через Build, как их проходит подключение.
func TestALPNByTransport(t *testing.T) {
	list := func(items ...string) []any {
		out := make([]any, len(items))
		for i, s := range items {
			out[i] = s
		}
		return out
	}
	cases := []struct {
		name   string
		stream model.StreamSettings
		want   []any // nil — ALPN не задан
	}{
		// ws и httpupgrade: h2/h3 убираются.
		{"ws h2 -> без ALPN", model.StreamSettings{Network: "ws", ALPN: "h2"}, nil},
		{"ws h3 -> без ALPN", model.StreamSettings{Network: "ws", ALPN: "h3"}, nil},
		{"ws h2,h3 -> без ALPN", model.StreamSettings{Network: "ws", ALPN: "h2,h3"}, nil},
		{"ws h2,http/1.1 -> http/1.1", model.StreamSettings{Network: "ws", ALPN: "h2,http/1.1"}, list("http/1.1")},
		{"ws http/1.1 -> без изменений", model.StreamSettings{Network: "ws", ALPN: "http/1.1"}, list("http/1.1")},
		{"ws без ALPN -> без ALPN", model.StreamSettings{Network: "ws"}, nil},
		{"httpupgrade h2 -> без ALPN", model.StreamSettings{Network: "httpupgrade", ALPN: "h2"}, nil},
		{"httpupgrade h2,http/1.1 -> http/1.1", model.StreamSettings{Network: "httpupgrade", ALPN: "h2,http/1.1"}, list("http/1.1")},
		// grpc и http (h2-транспорт): ALPN без h2 заменяется на [h2].
		{"grpc h2 -> без изменений", model.StreamSettings{Network: "grpc", ALPN: "h2"}, list("h2")},
		{"grpc http/1.1 -> h2", model.StreamSettings{Network: "grpc", ALPN: "http/1.1"}, list("h2")},
		{"grpc h3 -> h2", model.StreamSettings{Network: "grpc", ALPN: "h3"}, list("h2")},
		{"grpc h2,http/1.1 -> без изменений", model.StreamSettings{Network: "grpc", ALPN: "h2,http/1.1"}, list("h2", "http/1.1")},
		{"grpc без ALPN -> без ALPN", model.StreamSettings{Network: "grpc"}, nil},
		{"http http/1.1 -> h2", model.StreamSettings{Network: "http", ALPN: "http/1.1"}, list("h2")},
		{"http h2 -> без изменений", model.StreamSettings{Network: "http", ALPN: "h2"}, list("h2")},
		{"tcp+обфускация http: http/1.1 -> h2", model.StreamSettings{Network: "tcp", HeaderType: "http", ALPN: "http/1.1"}, list("h2")},
		// quic, tcp без транспорта и REALITY: как в ссылке.
		{"quic h3 -> без изменений", model.StreamSettings{Network: "quic", ALPN: "h3"}, list("h3")},
		{"quic h2 -> без изменений", model.StreamSettings{Network: "quic", ALPN: "h2"}, list("h2")},
		{"tcp h2,http/1.1 -> без изменений", model.StreamSettings{Network: "tcp", ALPN: "h2,http/1.1"}, list("h2", "http/1.1")},
		{"без сети h2 -> без изменений", model.StreamSettings{ALPN: "h2"}, list("h2")},
		{"reality+grpc http/1.1 -> без изменений", model.StreamSettings{
			Network: "grpc", ALPN: "http/1.1", RealityPublicKey: "UuMBgl7MXTPx9inmQp2UC7Jcnwc6XYbwDNebonM-FCc",
		}, list("http/1.1")},
		{"reality+tcp h2 -> без изменений", model.StreamSettings{
			Network: "tcp", ALPN: "h2", RealityPublicKey: "UuMBgl7MXTPx9inmQp2UC7Jcnwc6XYbwDNebonM-FCc",
		}, list("h2")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st := c.stream
			st.Security = "tls"
			if st.RealityPublicKey != "" {
				st.Security = "reality"
			}
			got, hasTLS := proxyTLSALPN(t, vlessWithStream(&st))
			if !hasTLS {
				t.Fatal("в конфиге нет tls")
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("tls.alpn = %v, ожидалось %v", got, c.want)
			}
		})
	}
}

// vmess и trojan идут по тому же пути, что и vless; ws без TLS не получает ALPN.
func TestALPNByTransportOtherProtocols(t *testing.T) {
	vm := &model.Profile{
		ID: 2, Type: model.TypeVMess, Address: "vm.example.org", Port: 443,
		Detail: &model.VMessDetail{UUID: "b831381d-6324-4d53-ad4f-8cda48b30811", Security: "auto"},
		Stream: &model.StreamSettings{Network: "ws", Security: "tls", ALPN: "h2", UTLSFingerprint: "firefox"},
	}
	if got, _ := proxyTLSALPN(t, vm); got != nil {
		t.Errorf("vmess ws tls alpn=h2: tls.alpn = %v, ожидалось отсутствие", got)
	}
	tr := &model.Profile{
		ID: 3, Type: model.TypeTrojan, Address: "tr.example.org", Port: 443,
		Detail: &model.TrojanVlessDetail{Password: "pw"},
		Stream: &model.StreamSettings{Network: "grpc", Security: "tls", ALPN: "http/1.1"},
	}
	if got, _ := proxyTLSALPN(t, tr); !reflect.DeepEqual(got, []any{"h2"}) {
		t.Errorf("trojan grpc alpn=http/1.1: tls.alpn = %v, ожидалось [h2]", got)
	}
	plain := &model.Profile{
		ID: 4, Type: model.TypeVMess, Address: "vm.example.org", Port: 80,
		Detail: &model.VMessDetail{UUID: "b831381d-6324-4d53-ad4f-8cda48b30811", Security: "auto"},
		Stream: &model.StreamSettings{Network: "ws", Path: "/ws", ALPN: "h2"},
	}
	if _, hasTLS := proxyTLSALPN(t, plain); hasTLS {
		t.Error("vmess ws без TLS не должен получать блок tls")
	}
}

// Экспорт и тестовый инстанс (T-84) строятся тем же Build, поэтому исправление
// действует и на них.
func TestALPNByTransportForTestAndExport(t *testing.T) {
	p := vlessWithStream(&model.StreamSettings{Network: "ws", Security: "tls", ALPN: "h2"})
	for name, req := range map[string]BuildRequest{
		"for_test":   {Profile: p, ForTest: true},
		"for_export": {Profile: p, ForExport: true},
	} {
		res, err := Build(req)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		tls, _ := outboundByTag(t, res, "proxy")["tls"].(map[string]any)
		if _, has := tls["alpn"]; has {
			t.Errorf("%s: tls.alpn = %v, ожидалось отсутствие", name, tls["alpn"])
		}
	}
}
