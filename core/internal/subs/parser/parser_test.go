// SPDX-License-Identifier: GPL-3.0-only

package parser

import (
	"bytes"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParsePlainMultiline(t *testing.T) {
	res, err := Parse(readFixture(t, "plain.txt"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Format != FormatLines {
		t.Errorf("format = %s, want lines", res.Format)
	}
	if len(res.Profiles) != 3 {
		t.Fatalf("profiles = %d, want 3", len(res.Profiles))
	}
	if res.Profiles[0].Type != model.TypeVMess || res.Profiles[0].Name != "NMT1" {
		t.Errorf("p0 = %s/%s", res.Profiles[0].Type, res.Profiles[0].Name)
	}
	if res.Profiles[1].Type != model.TypeTrojan || res.Profiles[1].Address != "example.org" {
		t.Errorf("p1 = %s/%s", res.Profiles[1].Type, res.Profiles[1].Address)
	}
	if res.Profiles[2].Type != model.TypeShadowsocks {
		t.Errorf("p2 type = %s", res.Profiles[2].Type)
	}
	if len(res.Errors) != 1 || !strings.Contains(res.Errors[0].Err, "схема") && !strings.Contains(res.Errors[0].Err, "не распознана") {
		// мусорная строка собрана как ошибка, пакет не повален
		t.Errorf("errors = %+v", res.Errors)
	}
}

func TestParseBase64Blob(t *testing.T) {
	res, err := Parse(readFixture(t, "base64.txt"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Format != FormatBase64 {
		t.Errorf("format = %s, want base64", res.Format)
	}
	if len(res.Profiles) != 2 {
		t.Fatalf("profiles = %d, want 2", len(res.Profiles))
	}
	// Заголовки из тела (FR-UI-262)
	if res.Info == nil {
		t.Fatal("info is nil")
	}
	if res.Info.Title != "Тестовый" {
		t.Errorf("title = %q", res.Info.Title)
	}
	if res.Info.UploadBytes == nil || *res.Info.UploadBytes != 100 {
		t.Errorf("upload = %+v", res.Info.UploadBytes)
	}
	if res.Info.TotalBytes == nil || *res.Info.TotalBytes != 1000 {
		t.Errorf("total = %+v", res.Info.TotalBytes)
	}
	if res.Info.ExpireMs == nil || *res.Info.ExpireMs != 1735689600000 {
		t.Errorf("expire_ms = %+v", res.Info.ExpireMs)
	}
}

func TestParseBase64URLSafeNoPadding(t *testing.T) {
	// urlsafe-алфавит без паддинга (частый вид выдачи панелей)
	inner := "ss://YWVzLTI1Ni1nY206c2VjcmV0@1.2.3.4:8388#URLSAFE\n"
	blob := base64.RawURLEncoding.EncodeToString([]byte(inner))
	res, err := Parse([]byte(blob), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Profiles) != 1 || res.Profiles[0].Name != "URLSAFE" {
		t.Fatalf("profiles = %+v", res.Profiles)
	}
}

func TestParseSingBoxJSON(t *testing.T) {
	res, err := Parse(readFixture(t, "singbox.json"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Format != FormatSingBox {
		t.Errorf("format = %s", res.Format)
	}
	if len(res.Profiles) != 8 {
		t.Fatalf("profiles = %d, want 8 (selector/direct/unknown — skip; битый порт — ошибка)", len(res.Profiles))
	}
	if res.Skipped != 3 {
		t.Errorf("skipped = %d, want 3", res.Skipped)
	}
	if len(res.Errors) != 1 {
		t.Errorf("errors = %+v, want 1 (порт 0)", res.Errors)
	}
	byTag := map[string]*model.Profile{}
	for _, p := range res.Profiles {
		byTag[p.Name] = p
	}
	vm := byTag["vm1"]
	if vm == nil || vm.Type != model.TypeVMess || vm.Port != 443 {
		t.Fatalf("vm1 = %+v", vm)
	}
	if vm.Stream == nil || vm.Stream.Network != "ws" || vm.Stream.Security != "tls" ||
		vm.Stream.SNI != "cdn.example.com" || vm.Stream.UTLSFingerprint != "chrome" ||
		vm.Stream.Host != "ws.example.com" || vm.Stream.WSEarlyDataHeader != "Sec-WebSocket-Protocol" {
		t.Errorf("vm1 stream = %+v", vm.Stream)
	}
	vl := byTag["vl1-reality"]
	if vl.Stream == nil || vl.Stream.Security != "reality" || vl.Stream.RealityPublicKey != "PUBKEY123" || vl.Stream.RealityShortID != "0123abcd" {
		t.Errorf("vless stream = %+v", vl.Stream)
	}
	d := vl.Detail.(*model.TrojanVlessDetail)
	if d.Password != "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee" || d.Flow != "xtls-rprx-vision" {
		t.Errorf("vless detail = %+v", d)
	}
	tr := byTag["tr1"]
	if tr.Stream == nil || tr.Stream.ALPN != "h2,http/1.1" || !tr.Stream.AllowInsecure {
		t.Errorf("trojan stream = %+v", tr.Stream)
	}
	ss := byTag["ss1"]
	if ss.Detail.(*model.ShadowsocksDetail).Plugin != "obfs-local;obfs=http;obfs-host=ss.example.io" {
		t.Errorf("ss plugin = %+v", ss.Detail)
	}
	hy := byTag["hy2-hop"]
	hd := hy.Detail.(*model.Hysteria2Detail)
	if hd.ServerPorts != "30000:30100" || hd.HopInterval != "30s" || hd.ObfsPassword != "obfspass" || hd.DownloadMbps != 500 {
		t.Errorf("hy2 detail = %+v", hd)
	}
	tu := byTag["tuic1"]
	td := tu.Detail.(*model.TUICDetail)
	if td.CongestionControl != "bbr" || !td.ZeroRttHandshake || td.Heartbeat != "10s" || td.SNI != "tuic-sni.example.app" {
		t.Errorf("tuic detail = %+v", td)
	}
	so := byTag["socks4-node"]
	if so.Detail.(*model.SocksHTTPDetail).Proto != "socks4" {
		t.Errorf("socks proto = %+v", so.Detail)
	}
}

func TestParseSingBoxTopLevelArray(t *testing.T) {
	content := `[{"type":"trojan","tag":"a","server":"s.example.com","server_port":443,"password":"p"}]`
	res, err := Parse([]byte(content), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Profiles) != 1 || res.Profiles[0].Name != "a" {
		t.Fatalf("profiles = %+v", res.Profiles)
	}
}

func TestParseErrors(t *testing.T) {
	cases := []struct {
		fixture string
		msg     string
	}{
		{"empty.txt", ErrEmptyContent},
		{"html.txt", ErrHTMLStubMsg},
		{"broken.txt", "не распознано ни одного профиля"},
		{"json-no-outbounds.json", "не распознан"},
	}
	for _, c := range cases {
		_, err := Parse(readFixture(t, c.fixture), Options{})
		if err == nil {
			t.Errorf("%s: ожидалась ошибка %q", c.fixture, c.msg)
			continue
		}
		if !strings.Contains(err.Error(), c.msg) {
			t.Errorf("%s: ошибка %q не содержит %q", c.fixture, err.Error(), c.msg)
		}
	}
}

func TestParseBrokenJSONFallsThrough(t *testing.T) {
	// Битый JSON — не считается sing-box; многострочник тоже не опознается →
	// структурная ошибка (не паника).
	if _, err := Parse(readFixture(t, "singbox-broken.json"), Options{}); err == nil {
		t.Fatal("ожидалась ошибка")
	}
}

func TestParseHappLines(t *testing.T) {
	res, err := Parse(readFixture(t, "happ.txt"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	// vector 0 расшифровывается в https://sub.example.com/... — парсится как
	// http-профиль (ссылка http(s) — валидная share-ссылка HTTP-прокси);
	// trojan — второй профиль; happ://crypt5/short — ошибка строки.
	if len(res.Profiles) != 2 {
		t.Fatalf("profiles = %d, want 2", len(res.Profiles))
	}
	if res.Profiles[0].Type != model.TypeHTTP || res.Profiles[0].Address != "sub.example.com" {
		t.Errorf("p0 = %+v", res.Profiles[0])
	}
	if len(res.Errors) != 1 || !strings.Contains(res.Errors[0].Err, "happ") {
		t.Errorf("errors = %+v", res.Errors)
	}
}

func TestParseHappInject(t *testing.T) {
	// Инъекция декриптора: happ → многострочник из двух ссылок.
	calls := 0
	opts := Options{DecryptHapp: func(string) (string, error) {
		calls++
		return "trojan://pass@example.org:443#H1\nvmess://eyJwcyI6IkgyIiwiYWRkIjoiZXhhbXBsZS5jb20iLCJwb3J0Ijo0NDMsImlkIjoiODM0MGNmYzctMzQ1Ni00Nzg5LWE4OWItMDEyMzQ1Njc4OWFiYyJ9", nil
	}}
	res, err := Parse([]byte("happ://crypt5/fake-link\n"), opts)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Errorf("calls = %d", calls)
	}
	if len(res.Profiles) != 2 || res.Profiles[0].Name != "H1" || res.Profiles[1].Name != "H2" {
		t.Fatalf("profiles = %+v", res.Profiles)
	}
}

func TestParseHappErrorInjected(t *testing.T) {
	opts := Options{DecryptHapp: func(string) (string, error) { return "", errors.New("boom") }}
	_, err := Parse([]byte("happ://crypt5/x\n"), opts)
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err = %v", err)
	}
}

func TestParseNekoCCfgWarning(t *testing.T) {
	// G33/LNB-25: neko-профиль с c_cfg — поля отброшены, предупреждение в preview.
	bean := `{"name":"n","addr":"example.com","port":443,"pass":"p","c_cfg":"{\"log\":{\"level\":\"debug\"}}","c_out":"{\"tag\":\"x\"}","core":"internal-full"}`
	link := "nekoray://trojan#" + base64.URLEncoding.EncodeToString([]byte(bean))
	res, err := Parse([]byte(link+"\n"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Profiles) != 1 {
		t.Fatalf("profiles = %d", len(res.Profiles))
	}
	p := res.Profiles[0]
	if p.CustomConfigJSON != "" || p.CustomOutboundJSON != "" {
		t.Errorf("custom-поля не отброшены: %+v", p)
	}
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "c_cfg") {
		t.Errorf("warnings = %+v", res.Warnings)
	}
}

func TestParseMaxProfiles(t *testing.T) {
	var b bytes.Buffer
	for i := 0; i < 20; i++ {
		b.WriteString("trojan://pass@example.org:443#P\n")
	}
	res, err := Parse(b.Bytes(), Options{MaxProfiles: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Profiles) != 5 {
		t.Fatalf("profiles = %d, want 5", len(res.Profiles))
	}
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "лимит") {
		t.Errorf("warnings = %+v", res.Warnings)
	}
}

func TestBase64WithProxiesWordInside(t *testing.T) {
	// LNB-16-соседний кейс: base64, внутри декода есть слово «proxies:» —
	// не должен уводиться в YAML-ветку (её нет) и не должен молча дать 0.
	inner := "# profile with proxies: inside name\ntrojan://pass@example.org:443#proxies:\n"
	blob := base64.StdEncoding.EncodeToString([]byte(inner))
	res, err := Parse([]byte(blob), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Profiles) != 1 || res.Profiles[0].Name != "proxies:" {
		t.Fatalf("profiles = %+v", res.Profiles)
	}
}

func TestBadBase64PaddingRejected(t *testing.T) {
	// deviations.yaml: некорректный padding — LAVRay отвергает.
	blob := "dHJvamFuOi8vcGFzc0BleGFtcGxlLm9yZzo0NDMjUD" + "=" + "QQ" // '=' в середине
	if _, err := Parse([]byte(blob), Options{}); err == nil {
		t.Fatal("ожидалась ошибка (паддинг в середине)")
	}
}

func TestHeaderParsing(t *testing.T) {
	info := &model.SubscriptionInfo{}
	if err := ApplySubscriptionHeader(info, "subscription-userinfo", "upload=1; download=2; total=3; expire=60"); err != nil {
		t.Fatal(err)
	}
	if *info.UploadBytes != 1 || *info.ExpireMs != 60000 {
		t.Errorf("userinfo = %+v", info)
	}
	if err := ApplySubscriptionHeader(info, "subscription-userinfo", "upload=abc"); err == nil {
		t.Error("мусор в userinfo должен быть ошибкой")
	}
	if err := ApplySubscriptionHeader(info, "profile-title", encodeTitleB64("Заголовок")); err != nil {
		t.Fatal(err)
	}
	if info.Title != "Заголовок" {
		t.Errorf("title = %q", info.Title)
	}
	if err := ApplySubscriptionHeader(info, "profile-update-interval", "24"); err != nil {
		t.Fatal(err)
	}
	if info.UpdateIntervalHours == nil || *info.UpdateIntervalHours != 24 {
		t.Errorf("interval = %+v", info.UpdateIntervalHours)
	}
	if err := ApplySubscriptionHeader(info, "profile-update-interval", "x"); err == nil {
		t.Error("мусорный интервал должен быть ошибкой")
	}
	if got := TitleFromDisposition(`attachment; filename="my-sub.txt"`); got != "my-sub.txt" {
		t.Errorf("disposition = %q", got)
	}
	if got := TitleFromDisposition(`inline`); got != "" {
		t.Errorf("disposition без filename = %q", got)
	}
	k, v, ok := splitBodyHeader("# Support-URL: https://help.example.com")
	if !ok || k != "support-url" || v != "https://help.example.com" {
		t.Errorf("splitBodyHeader = %q %q %v", k, v, ok)
	}
	if _, _, ok := splitBodyHeader("# просто комментарий"); ok {
		t.Error("комментарий не заголовок")
	}
}

func TestParseNekoCustomCoreStripped(t *testing.T) {
	// W3-1 (LNB-25/FR-UI-257): core/cmd/cs из neko-ссылки в подписке —
	// обнуляются, профиль сохраняется каркасом; warning упоминает поля.
	bean := `{"name":"evil","addr":"h.example.com","port":443,"core":"external","cmd":["curl","evil.sh"],"cs":"-l %mapping_port%","socks_port":10808}`
	link := "nekoray://custom#" + base64.URLEncoding.EncodeToString([]byte(bean))
	res, err := Parse([]byte(link+"\n"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Profiles) != 1 {
		t.Fatalf("profiles = %d", len(res.Profiles))
	}
	p := res.Profiles[0]
	d, ok := p.Detail.(*model.CustomDetail)
	if !ok {
		t.Fatalf("detail = %T", p.Detail)
	}
	if len(d.Command) != 0 || d.Core != "" || d.ExternalCoreName != "" ||
		d.ConfigTemplate != "" || d.OutboundJSON != "" || d.FullConfigJSON != "" {
		t.Errorf("поля custom-ядра не обнулены: %+v", d)
	}
	if !p.CustomFieldsConfirmed {
		// ожидаемо false — просто фиксируем контракт
	} else {
		t.Errorf("CustomFieldsConfirmed должен остаться false")
	}
	found := false
	for _, w := range res.Warnings {
		if strings.Contains(w, "cmd") || strings.Contains(w, "core") || strings.Contains(w, "cs") {
			found = true
		}
	}
	if !found {
		t.Errorf("warning о custom-полях не выдан: %+v", res.Warnings)
	}
}
