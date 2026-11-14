// SPDX-License-Identifier: GPL-3.0-only

package mobile

import (
	"context"
	"encoding/binary"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	mDNS "github.com/miekg/dns"
	C "github.com/sagernet/sing-box/constant"
)

func TestBearerToken(t *testing.T) {
	tests := []struct {
		header string
		want   string
		ok     bool
	}{
		{"Bearer abc", "abc", true},
		{"bearer   abc", "abc", true},
		{"abc", "", false},
		{"Basic abc", "", false},
		{"", "", false},
	}
	for _, tc := range tests {
		got, ok := bearerToken(tc.header)
		if got != tc.want || ok != tc.ok {
			t.Errorf("bearerToken(%q) = %q,%v", tc.header, got, ok)
		}
	}
}

func TestAuthMiddleware(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	tests := []struct {
		name, token, method, path, auth string
		want                            int
	}{
		{"верный токен", "secret", "POST", "/x", "Bearer secret", 204},
		{"неверный токен", "secret", "POST", "/x", "Bearer other", 401},
		{"без заголовка", "secret", "POST", "/x", "", 401},
		{"голый токен", "secret", "POST", "/x", "secret", 401},
		{"пустой токен ядра отклоняет всех", "", "POST", "/x", "Bearer ", 401},
		{"healthz без токена", "secret", "GET", "/healthz", "", 200},
		{"OPTIONS без токена", "secret", "OPTIONS", "/x", "", 204},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			if tc.auth != "" {
				req.Header.Set("Authorization", tc.auth)
			}
			rec := httptest.NewRecorder()
			authMiddleware(next, tc.token).ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Errorf("код %d, ждали %d", rec.Code, tc.want)
			}
		})
	}
}

// packedTzdata собирает файл tzdata Android с одной зоной.
func packedTzdata(t *testing.T, name string, tzif []byte) string {
	t.Helper()
	const indexOff = 24
	dataOff := indexOff + tzEntrySize
	buf := make([]byte, dataOff)
	copy(buf, "tzdata2025a\x00")
	binary.BigEndian.PutUint32(buf[12:], indexOff)
	binary.BigEndian.PutUint32(buf[16:], uint32(dataOff))
	binary.BigEndian.PutUint32(buf[20:], uint32(dataOff+len(tzif)))
	copy(buf[indexOff:], name)
	binary.BigEndian.PutUint32(buf[indexOff+tzNameSize:], 0)
	binary.BigEndian.PutUint32(buf[indexOff+tzNameSize+4:], uint32(len(tzif)))
	path := filepath.Join(t.TempDir(), "tzdata")
	if err := os.WriteFile(path, append(buf, tzif...), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// tzifFixed — TZif версии 1 с одним типом: фиксированное смещение.
func tzifFixed(offset int32, abbr string) []byte {
	b := []byte("TZif")
	b = append(b, 0)
	b = append(b, make([]byte, 15)...)
	for _, n := range []uint32{0, 0, 0, 0, 1, uint32(len(abbr) + 1)} {
		b = binary.BigEndian.AppendUint32(b, n)
	}
	b = binary.BigEndian.AppendUint32(b, uint32(offset))
	b = append(b, 0, 0)
	return append(b, append([]byte(abbr), 0)...)
}

func TestLoadAndroidZone(t *testing.T) {
	src := packedTzdata(t, "Europe/Moscow", tzifFixed(3*3600, "MSK"))
	loc, err := loadAndroidZone("Europe/Moscow", []string{"/нет/такого", src})
	if err != nil {
		t.Fatal(err)
	}
	name, off := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC).In(loc).Zone()
	if name != "MSK" || off != 3*3600 {
		t.Errorf("зона = %s %d", name, off)
	}
	if _, err := loadAndroidZone("Asia/Nowhere", []string{src}); err == nil {
		t.Error("ждали ошибку для отсутствующей зоны")
	}
	if _, err := loadAndroidZone("X", []string{"/нет/такого"}); err == nil {
		t.Error("ждали ошибку для отсутствующего файла")
	}
}

func TestResolveZoneEmptyName(t *testing.T) {
	if loc, err := resolveZone("", nil); loc != nil || err != nil {
		t.Errorf("пустое имя: %v %v", loc, err)
	}
}

func TestPrepareEnv(t *testing.T) {
	root := t.TempDir()
	t.Setenv("TMPDIR", "x")
	t.Setenv("HOME", "x")
	o := &Options{DataDir: filepath.Join(root, "data"), TempDir: filepath.Join(root, "tmp")}
	if err := prepareEnv(o); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("TMPDIR") != o.TempDir || os.Getenv("HOME") != o.DataDir {
		t.Error("TMPDIR/HOME не заданы")
	}
	if prepareEnv(&Options{}) == nil {
		t.Error("пустые каталоги должны давать ошибку")
	}
}

type fakePlatform struct{}

func (fakePlatform) OpenTun(string) (int32, error) { return 0, nil }
func (fakePlatform) Protect(int32) error           { return nil }
func (fakePlatform) FindOwnerUid(int32, string, int32, string, int32) (int32, error) {
	return -1, nil
}
func (fakePlatform) PackagesForUid(int32) string               { return "" }
func (fakePlatform) AppRoutes() string                         { return "" }
func (fakePlatform) StartNetworkMonitor(NetworkListener) error { return nil }
func (fakePlatform) CloseNetworkMonitor() error                { return nil }
func (fakePlatform) Interfaces() (string, error)               { return "[]", nil }

func TestValidateOptions(t *testing.T) {
	long := strings.Repeat("a", maxUnixPath+1)
	tests := []struct {
		name string
		o    *Options
		p    Platform
		ok   bool
	}{
		{"nil Options", nil, fakePlatform{}, false},
		{"nil Platform", &Options{SocketPath: "/s", AuthToken: "t"}, nil, false},
		{"нет сокета", &Options{AuthToken: "t"}, fakePlatform{}, false},
		{"нет токена", &Options{SocketPath: "/s"}, fakePlatform{}, false},
		{"длинный путь", &Options{SocketPath: long, AuthToken: "t"}, fakePlatform{}, false},
		{"верно", &Options{SocketPath: "/s", AuthToken: "t"}, fakePlatform{}, true},
	}
	for _, tc := range tests {
		if err := validateOptions(tc.o, tc.p); (err == nil) != tc.ok {
			t.Errorf("%s: err = %v", tc.name, err)
		}
	}
}

func TestParseInterfaces(t *testing.T) {
	raw := `[
	 {"name":"wlan0","index":3,"mtu":1500,"addrs":["192.168.1.5/24","fe80::1/64","bad"],"flags":4161,"metered":true,"type":"wifi","dns":["8.8.8.8"],"gateway":["192.168.1.1"]},
	 {"name":"wlan0","index":9},
	 {"name":"rmnet0","index":4,"type":1},
	 {"name":"lo","index":1,"flags":73,"type":"weird"}]`
	list, err := parseInterfaces(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Fatalf("интерфейсов %d, ждали 3 (дубль имени отброшен)", len(list))
	}
	w := list[0]
	if w.Type != C.InterfaceTypeWIFI || !w.Expensive || w.Flags&net.FlagUp == 0 || len(w.Addresses) != 2 ||
		len(w.DNSServers) != 1 || w.Gateways[0] != netip.MustParseAddr("192.168.1.1") {
		t.Errorf("wlan0 = %+v", w)
	}
	if list[1].Type != C.InterfaceTypeCellular || list[1].Flags&net.FlagUp == 0 {
		t.Errorf("rmnet0 = %+v (flags по умолчанию UP)", list[1])
	}
	if list[2].Type != C.InterfaceTypeOther || list[2].Flags&net.FlagLoopback == 0 {
		t.Errorf("lo = %+v", list[2])
	}
	if _, err := parseInterfaces(`{`); err == nil {
		t.Error("ждали ошибку разбора")
	}
}

type monitorPlatform struct {
	fakePlatform
	starts, closes int
}

func (m *monitorPlatform) StartNetworkMonitor(NetworkListener) error { m.starts++; return nil }
func (m *monitorPlatform) CloseNetworkMonitor() error                { m.closes++; return nil }

func TestHubSharesPlatformMonitor(t *testing.T) {
	fp := &monitorPlatform{}
	hub := newNetHub(fp)
	mk := func() *defaultMonitor { return &defaultMonitor{bp: &boxPlatform{h: &host{hub: hub}}} }
	a, b := mk(), mk()
	if err := hub.subscribe(a); err != nil {
		t.Fatal(err)
	}
	if err := hub.subscribe(b); err != nil {
		t.Fatal(err)
	}
	if fp.starts != 1 {
		t.Fatalf("StartNetworkMonitor вызван %d раз, ждали 1", fp.starts)
	}
	if err := hub.unsubscribe(a); err != nil || fp.closes != 0 {
		t.Fatalf("после первой отписки closes=%d err=%v", fp.closes, err)
	}
	if err := hub.unsubscribe(b); err != nil || fp.closes != 1 {
		t.Fatalf("после последней отписки closes=%d err=%v", fp.closes, err)
	}
}

func TestSystemTransportExchange(t *testing.T) {
	lookup := func(_ context.Context, network, host string) ([]netip.Addr, error) {
		switch {
		case host == "example.test" && network == "ip4":
			return []netip.Addr{netip.MustParseAddr("192.0.2.7")}, nil
		case host == "example.test":
			return nil, &net.DNSError{IsNotFound: true}
		}
		return nil, &net.DNSError{Err: "timeout", IsTimeout: true}
	}
	tr := &systemTransport{lookup: lookup}
	ask := func(name string, qtype uint16) (*mDNS.Msg, error) {
		m := new(mDNS.Msg)
		m.SetQuestion(mDNS.Fqdn(name), qtype)
		return tr.exchangeLookup(context.Background(), m)
	}
	resp, err := ask("example.test", mDNS.TypeA)
	if err != nil || len(resp.Answer) != 1 {
		t.Fatalf("A: %v %v", resp, err)
	}
	if resp, err = ask("example.test", mDNS.TypeAAAA); err != nil || len(resp.Answer) != 0 || resp.Rcode != mDNS.RcodeSuccess {
		t.Errorf("AAAA без адресов: %v %v", resp, err)
	}
	if _, err = ask("fail.test", mDNS.TypeA); err == nil {
		t.Error("ждали ошибку таймаута")
	}
	if _, err = ask("example.test", mDNS.TypeMX); err == nil {
		t.Error("MX не поддерживается")
	}
}
