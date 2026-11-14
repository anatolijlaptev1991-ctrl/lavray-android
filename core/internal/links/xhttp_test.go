// SPDX-License-Identifier: GPL-3.0-only

package links

import (
	"net/url"
	"strings"
	"testing"
)

// Ссылка XHTTP разбирается, а при экспорте тип транспорта, путь, режим и extra не
// теряются (раньше импорт отвергал такую ссылку целиком, потом терял mode и extra).
func TestXHTTPLinkRoundTrip(t *testing.T) {
	const extra = `{"xPaddingBytes":"100-1000","noGRPCHeader":true}`
	link := "vless://11111111-1111-1111-1111-111111111111@xh.example.org:443" +
		"?type=xhttp&security=tls&sni=xh.example.org&path=%2Fsplit&host=cdn.example.org&mode=packet-up" +
		"&extra=" + url.QueryEscape(extra) + "#xh"
	p, err := ParseLink(link)
	if err != nil {
		t.Fatal(err)
	}
	if p.Stream.Network != "xhttp" || p.Stream.Path != "/split" || p.Stream.Host != "cdn.example.org" {
		t.Fatalf("stream = %+v", p.Stream)
	}
	if p.Stream.XHTTPMode != "packet-up" || p.Stream.XHTTPExtra != extra {
		t.Fatalf("mode/extra = %q / %q", p.Stream.XHTTPMode, p.Stream.XHTTPExtra)
	}
	out, err := BuildLink(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "type=xhttp") || !strings.Contains(out, "path=%2Fsplit") || !strings.Contains(out, "mode=packet-up") {
		t.Errorf("экспорт потерял транспорт: %s", out)
	}
	back, err := ParseLink(out)
	if err != nil {
		t.Fatal(err)
	}
	if back.Stream.Network != "xhttp" || back.Stream.XHTTPMode != "packet-up" || back.Stream.XHTTPExtra != extra {
		t.Errorf("повторный разбор: %+v", back.Stream)
	}
}

// mode и extra бывают только у XHTTP: у других транспортов эти параметры не читаются.
func TestXHTTPParamsIgnoredForOtherNetworks(t *testing.T) {
	p, err := ParseLink("vless://11111111-1111-1111-1111-111111111111@h.example.org:443?type=ws&mode=auto&extra=%7B%7D&security=tls#n")
	if err != nil {
		t.Fatal(err)
	}
	if p.Stream.XHTTPMode != "" || p.Stream.XHTTPExtra != "" {
		t.Errorf("ws получил параметры XHTTP: %+v", p.Stream)
	}
}

// Слишком длинный extra отвергается лимитом поля (LNB-17), как и прочие поля потока.
func TestXHTTPExtraIsCapped(t *testing.T) {
	long := url.QueryEscape(`{"k":"` + strings.Repeat("x", 9000) + `"}`)
	_, err := ParseLink("vless://11111111-1111-1111-1111-111111111111@h.example.org:443?type=xhttp&extra=" + long + "#n")
	if err == nil {
		t.Fatal("extra сверх лимита принят")
	}
}

// vmess:// в формате JSON с net=xhttp тоже разбирается.
func TestXHTTPVMessJSON(t *testing.T) {
	js := `{"v":"2","ps":"xh","add":"xh.example.org","port":"443","id":"11111111-1111-1111-1111-111111111111","aid":"0","net":"xhttp","path":"/p","tls":"tls"}`
	p, err := ParseLink("vmess://" + b64std(js))
	if err != nil {
		t.Fatal(err)
	}
	if p.Stream.Network != "xhttp" {
		t.Errorf("network = %q", p.Stream.Network)
	}
}
