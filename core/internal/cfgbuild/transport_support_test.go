// SPDX-License-Identifier: GPL-3.0-only

package cfgbuild

import (
	"testing"

	"connectrpc.com/connect"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/links"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

const xhttpLink = "vless://11111111-1111-1111-1111-111111111111@xh.example.org:443" +
	"?type=xhttp&security=tls&sni=xh.example.org&path=%2Fsplit&host=cdn.example.org&mode=packet-up#xhttp-node"

// Ссылка type=xhttp разбирается (раньше импорт отвергал её целиком), а профиль
// помечается как неподдерживаемый ядром: форк sing-box транспорта XHTTP не имеет.
func TestParseXHTTPMarkedUnsupported(t *testing.T) {
	p, err := links.ParseLink(xhttpLink)
	if err != nil {
		t.Fatalf("ParseLink: %v", err)
	}
	if p.Stream == nil || p.Stream.Network != "xhttp" {
		t.Fatalf("stream = %+v, ожидался network=xhttp", p.Stream)
	}
	if p.Stream.Path != "/split" || p.Stream.Host != "cdn.example.org" {
		t.Errorf("path/host = %q/%q", p.Stream.Path, p.Stream.Host)
	}
	if got := unsupportedTransport(p); got != "xhttp" {
		t.Errorf("unsupportedTransport = %q, ожидалось xhttp", got)
	}
	if got := UnsupportedReason(p); got != "xhttp_unsupported" {
		t.Errorf("UnsupportedReason = %q", got)
	}
}

// Прежний псевдоним Xray (type=splithttp) — тот же транспорт.
func TestParseSplitHTTPAliasIsXHTTP(t *testing.T) {
	p, err := links.ParseLink("vless://11111111-1111-1111-1111-111111111111@h.example.org:443?type=splithttp&security=tls#n")
	if err != nil {
		t.Fatal(err)
	}
	if p.Stream.Network != "xhttp" {
		t.Errorf("network = %q", p.Stream.Network)
	}
}

// Поддерживаемые транспорты и профили без потока причины не получают.
func TestSupportedTransportsHaveNoReason(t *testing.T) {
	for _, network := range []string{"", "tcp", "ws", "grpc", "http", "httpupgrade", "quic"} {
		p := &model.Profile{Type: model.TypeVLESS, Stream: &model.StreamSettings{Network: network}}
		if got := UnsupportedReason(p); got != "" {
			t.Errorf("network %q: причина %q", network, got)
		}
	}
	if got := UnsupportedReason(&model.Profile{Type: model.TypeVLESS}); got != "" {
		t.Errorf("без потока: причина %q", got)
	}
	if got := UnsupportedReason(nil); got != "" {
		t.Errorf("nil: причина %q", got)
	}
}

// Сборка конфига отказывает с FAILED_PRECONDITION и причиной transport_unsupported.
func TestBuildRejectsXHTTP(t *testing.T) {
	p, err := links.ParseLink(xhttpLink)
	if err != nil {
		t.Fatal(err)
	}
	p.ID = 5
	_, err = Build(BuildRequest{Profile: p})
	e, ok := AsError(err)
	if !ok {
		t.Fatalf("err = %v, ожидался *cfgbuild.Error", err)
	}
	if e.Code != connect.CodeFailedPrecondition || e.Reason != ReasonTransportUnsupported {
		t.Errorf("code=%v reason=%q", e.Code, e.Reason)
	}
}

// Звено цепочки с XHTTP тоже не собирается: иначе цепочка молча шла бы мимо него.
func TestBuildRejectsXHTTPInsideChain(t *testing.T) {
	member, err := links.ParseLink(xhttpLink)
	if err != nil {
		t.Fatal(err)
	}
	member.ID = 2
	chain := &model.Profile{ID: 1, Type: model.TypeChain, Detail: &model.ChainDetail{ProfileIDs: []int64{2}}}
	_, err = Build(BuildRequest{Profile: chain, Lookup: lookupPool(member)})
	e, ok := AsError(err)
	if !ok || e.Reason != ReasonTransportUnsupported {
		t.Fatalf("err = %v, ожидалась причина %s", err, ReasonTransportUnsupported)
	}
}

// Ошибка Connect несёт причину в ErrorInfo, а не только в тексте.
func TestErrorCarriesReasonDetail(t *testing.T) {
	e := failedPrecondition("stream.network", "profile %d: транспорт %s не поддерживается", 5, "xhttp")
	e.Reason = ReasonTransportUnsupported
	ce := e.ConnectError()
	if len(ce.Details()) != 1 {
		t.Fatalf("details = %d, ожидалась 1", len(ce.Details()))
	}
}
