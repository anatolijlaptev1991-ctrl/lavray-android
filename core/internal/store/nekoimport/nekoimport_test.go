// SPDX-License-Identifier: GPL-3.0-only

package nekoimport

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/store"
)

func openStore(t *testing.T) *store.Store {
	t.Helper()
	// Не t.TempDir(): на Windows RemoveAll гоняется с освобождением
	// хэндлов -wal/-shm (см. store_test.go mkTempDir).
	dir, err := os.MkdirTemp("", "lavray-nekoimport-test-*")
	if err != nil {
		t.Fatalf("mkdir temp: %v", err)
	}
	s, err := store.Open(dir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		_ = s.Close()
		for i := 0; i < 50; i++ {
			if err := os.RemoveAll(dir); err == nil {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	})
	return s
}

// mkTempDir — каталог с ретрай-удалением (гонка -wal/-shm на Windows).
func mkTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "lavray-nekoimport-test-*")
	if err != nil {
		t.Fatalf("mkdir temp: %v", err)
	}
	t.Cleanup(func() {
		for i := 0; i < 50; i++ {
			if err := os.RemoveAll(dir); err == nil {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	})
	return dir
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func profileIDs(list []*model.Profile) []int64 {
	ids := make([]int64, len(list))
	for i, p := range list {
		ids[i] = p.ID
	}
	return ids
}

// TestImportFixture — эталонный каталог: 2 группы, 8 профилей известных
// типов (порядок отображения из group.order, вкладок — из pm.json),
// один битый файл и один неизвестный тип — оба в отчёте, импорт не падает.
func TestImportFixture(t *testing.T) {
	s := openStore(t)
	rep, err := Import(s, filepath.Join("testdata", "nekobox"))
	if err != nil {
		t.Fatalf("import: %v", err)
	}

	if rep.GroupsImported != 2 {
		t.Fatalf("groups imported = %d, want 2", rep.GroupsImported)
	}
	wantTypes := map[string]int{
		"socks": 1, "http": 1, "shadowsocks": 1, "trojan": 1,
		"vmess": 1, "vless": 1, "hysteria2": 1, "chain": 1,
		"tuic": 1, "custom_external": 1, "naive": 1,
	}
	if !reflect.DeepEqual(rep.ProfilesByType, wantTypes) {
		t.Fatalf("profiles by type = %v, want %v", rep.ProfilesByType, wantTypes)
	}
	if n := rep.ProfilesImported(); n != 11 {
		t.Fatalf("profiles imported = %d, want 11", n)
	}
	if len(rep.Skipped) != 2 {
		t.Fatalf("skipped = %+v, want 2 entries", rep.Skipped)
	}
	skippedFiles := map[string]bool{}
	for _, sk := range rep.Skipped {
		skippedFiles[sk.File] = true
	}
	if !skippedFiles[filepath.Join("profiles", "7.json")] {
		t.Fatalf("broken file not reported: %+v", rep.Skipped)
	}
	if !skippedFiles[filepath.Join("profiles", "9.json")] {
		t.Fatalf("unknown-type file not reported: %+v", rep.Skipped)
	}
	for _, sk := range rep.Skipped {
		if sk.File == filepath.Join("profiles", "9.json") && sk.Type != "wireguard" {
			t.Fatalf("unknown type not recorded: %+v", sk)
		}
	}
	if len(rep.Warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", rep.Warnings)
	}

	// Группы: порядок вкладок из pm.json ([0,1]).
	groups, err := s.ListGroups()
	if err != nil {
		t.Fatal(err)
	}
	// Миграция сеет Default в резервном диапазоне (store.DefaultGroupID) —
	// импортированные группы идут в порядке pm.json следом за ней.
	if len(groups) != 3 || groups[0].ID != store.DefaultGroupID || groups[1].ID != 0 || groups[2].ID != 1 {
		t.Fatalf("groups = %+v, want [Default,0,1]", groups)
	}
	g1 := groups[2]
	if g1.Kind != model.GroupSubscription || g1.SubscriptionURL != "https://sub.example.com/list" {
		t.Fatalf("g1 = %+v", g1)
	}
	if g1.LastUpdateMs != 1730000000000 { // lastup в NekoBox — секунды; модель — мс (×1000)
		t.Fatalf("g1 lastup = %d", g1.LastUpdateMs)
	}
	// SubscriptionInfo из info (заголовок Subscription-UserInfo).
	si := g1.SubscriptionInfo
	if si == nil || si.UploadBytes == nil || *si.UploadBytes != 1048576 ||
		si.DownloadBytes == nil || *si.DownloadBytes != 2097152 ||
		si.TotalBytes == nil || *si.TotalBytes != 1073741824 ||
		si.ExpireMs == nil || *si.ExpireMs != 1735689600000 {
		t.Fatalf("g1 subscription info = %+v", si)
	}
	// front_proxy_id проставлен после вставки профилей (FK).
	if g1.FrontProxyID == nil || *g1.FrontProxyID != 0 {
		t.Fatalf("g1 front proxy = %+v, want 0", g1.FrontProxyID)
	}
	if groups[0].FrontProxyID != nil {
		t.Fatalf("g0 front proxy = %+v, want nil (-1)", groups[0].FrontProxyID)
	}
	if g1.UIState == "" {
		t.Fatal("g1 ui_state lost")
	}

	// Порядок профилей — из order-полей групп.
	l0, err := s.ListProfiles(0)
	if err != nil {
		t.Fatal(err)
	}
	if got := profileIDs(l0); !reflect.DeepEqual(got, []int64{0, 1, 2, 3, 8}) {
		t.Fatalf("group 0 order = %v", got)
	}
	l1, err := s.ListProfiles(1)
	if err != nil {
		t.Fatal(err)
	}
	if got := profileIDs(l1); !reflect.DeepEqual(got, []int64{4, 5, 6, 10, 11, 12}) { // + tuic/custom/naive
		t.Fatalf("group 1 order = %v", got)
	}

	// socks: детали, stream, custom-поля (LNB-25: без подтверждения), трафик.
	p0, err := s.GetProfile(0)
	if err != nil {
		t.Fatal(err)
	}
	if p0.Type != model.TypeSOCKS || p0.Address != "5.5.5.5" || p0.Port != 1080 || p0.Name != "socks five" {
		t.Fatalf("p0 = %+v", p0)
	}
	sd, ok := p0.Detail.(*model.SocksHTTPDetail)
	if !ok || sd.Proto != "socks5" || sd.Username != "u1" || sd.Password != "p1" {
		t.Fatalf("p0 detail = %+v", p0.Detail)
	}
	st := p0.Stream
	if st == nil || st.Network != "ws" || st.Security != "tls" || st.Path != "/ws" ||
		st.Host != "cdn.example.com" || st.SNI != "cdn.example.com" || st.UTLSFingerprint != "chrome" || st.Multiplex != 1 {
		t.Fatalf("p0 stream = %+v", st)
	}
	if p0.CustomConfigJSON == "" || p0.CustomOutboundJSON == "" || p0.CustomFieldsConfirmed {
		t.Fatalf("p0 custom fields: cfg=%q out=%q confirmed=%v (LNB-25: перенос без подтверждения)",
			p0.CustomConfigJSON, p0.CustomOutboundJSON, p0.CustomFieldsConfirmed)
	}
	if p0.LegacyBean == "" {
		t.Fatal("p0 legacy_bean lost")
	}
	if p0.Traffic.UplinkBytes != 222 || p0.Traffic.DownlinkBytes != 111 {
		t.Fatalf("p0 traffic = %+v", p0.Traffic)
	}
	if p0.TestResult.LatencyMs == nil || *p0.TestResult.LatencyMs != 120 {
		t.Fatalf("p0 latency = %+v", p0.TestResult.LatencyMs)
	}

	// http: yc=-1 → unavailable (отрицательная латентность сохраняется).
	p1, err := s.GetProfile(1)
	if err != nil {
		t.Fatal(err)
	}
	if hd, ok := p1.Detail.(*model.SocksHTTPDetail); !ok || hd.Proto != "http" {
		t.Fatalf("p1 detail = %+v", p1.Detail)
	}
	if p1.TestResult.LatencyMs == nil || *p1.TestResult.LatencyMs != -1 {
		t.Fatalf("p1 latency = %+v, want -1 (unavailable)", p1.TestResult.LatencyMs)
	}

	// shadowsocks: yc=0 → не тестировали (nil).
	p2, err := s.GetProfile(2)
	if err != nil {
		t.Fatal(err)
	}
	ss, ok := p2.Detail.(*model.ShadowsocksDetail)
	if !ok || ss.Method != "aes-256-gcm" || ss.Password != "sspass" || ss.UDPOverTCP != 1 {
		t.Fatalf("p2 detail = %+v", p2.Detail)
	}
	if p2.TestResult.LatencyMs != nil {
		t.Fatalf("p2 latency = %+v, want nil (yc=0)", p2.TestResult.LatencyMs)
	}

	// trojan: пароль + tls sni.
	p3, err := s.GetProfile(3)
	if err != nil {
		t.Fatal(err)
	}
	td, ok := p3.Detail.(*model.TrojanVlessDetail)
	if !ok || td.Password != "trpass" {
		t.Fatalf("p3 detail = %+v", p3.Detail)
	}
	if p3.Stream == nil || p3.Stream.Security != "tls" || p3.Stream.SNI != "tr.example.com" || p3.Stream.ALPN != "h2,http/1.1" {
		t.Fatalf("p3 stream = %+v", p3.Stream)
	}

	// vmess: uuid/security + grpc-транспорт.
	p4, err := s.GetProfile(4)
	if err != nil {
		t.Fatal(err)
	}
	vd, ok := p4.Detail.(*model.VMessDetail)
	if !ok || vd.UUID != "b831381d-6324-4d53-ad4f-8cda48b30811" || vd.Security != "auto" {
		t.Fatalf("p4 detail = %+v", p4.Detail)
	}
	if p4.Stream == nil || p4.Stream.Network != "grpc" || p4.Stream.Path != "/grpc" {
		t.Fatalf("p4 stream = %+v", p4.Stream)
	}

	// vless: reality (pbk/sid) + flow.
	p5, err := s.GetProfile(5)
	if err != nil {
		t.Fatal(err)
	}
	vl, ok := p5.Detail.(*model.TrojanVlessDetail)
	if !ok || vl.Password != "uuid-vless-1234" || vl.Flow != "xtls-rprx-vision" {
		t.Fatalf("p5 detail = %+v", p5.Detail)
	}
	if p5.Stream == nil || p5.Stream.RealityPublicKey != "pubkey123" || p5.Stream.RealityShortID != "sid123" {
		t.Fatalf("p5 stream = %+v", p5.Stream)
	}

	// hysteria2: server_ports/hop_interval (FR-UI-268) — имена полей sing-box 1.14.
	p6, err := s.GetProfile(6)
	if err != nil {
		t.Fatal(err)
	}
	hy, ok := p6.Detail.(*model.Hysteria2Detail)
	if !ok {
		t.Fatalf("p6 detail = %+v", p6.Detail)
	}
	if hy.Password != "hy2pass" || hy.ObfsPassword != "obfs123" ||
		hy.ServerPorts != "20000-30000" || hy.HopInterval != "30s" ||
		hy.UploadMbps != 100 || hy.DownloadMbps != 500 || hy.SNI != "hy.example.com" {
		t.Fatalf("p6 hy2 detail = %+v", hy)
	}

	// chain: ссылки на id профилей сохранены и валидны.
	p8, err := s.GetProfile(8)
	if err != nil {
		t.Fatal(err)
	}
	ch, ok := p8.Detail.(*model.ChainDetail)
	if !ok || !reflect.DeepEqual(ch.ProfileIDs, []int64{4, 5}) {
		t.Fatalf("p8 detail = %+v", p8.Detail)
	}
	for _, ref := range ch.ProfileIDs {
		if _, err := s.GetProfile(ref); err != nil {
			t.Fatalf("chain reference %d broken: %v", ref, err)
		}
	}
}

// TestImportIdempotent — повторный импорт не дублирует (§5.3).
func TestImportIdempotent(t *testing.T) {
	s := openStore(t)
	dir := filepath.Join("testdata", "nekobox")
	if _, err := Import(s, dir); err != nil {
		t.Fatalf("first import: %v", err)
	}
	rep, err := Import(s, dir)
	if err != nil {
		t.Fatalf("second import: %v", err)
	}
	if rep.GroupsImported != 0 || rep.ProfilesImported() != 0 {
		t.Fatalf("second import duplicated: %+v", rep)
	}
	if rep.AlreadyPresent != 13 { // 2 группы + 11 профилей
		t.Fatalf("already present = %d, want 13", rep.AlreadyPresent)
	}
	all, err := s.ListProfiles(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 5 { // без дубликатов
		t.Fatalf("group 0 profiles = %d, want 5", len(all))
	}
}

// TestImportBrokenFile — битый JSON одного файла: остальные импортируются,
// файл попадает в отчёт (FR-UI-260).
func TestImportBrokenFile(t *testing.T) {
	dir := mkTempDir(t)
	write(t, filepath.Join(dir, "groups", "pm.json"), `{"groups":[0]}`)
	write(t, filepath.Join(dir, "groups", "0.json"), `{"id":0,"name":"G","order":[0],"front_proxy_id":-1}`)
	write(t, filepath.Join(dir, "profiles", "0.json"),
		`{"type":"socks","id":0,"gid":0,"yc":10,"bean":{"_v":0,"name":"ok","addr":"1.2.3.4","port":1080,"v":5}}`)
	write(t, filepath.Join(dir, "profiles", "1.json"), `{"type":"socks","id":1,"gid":0,"bean":{"name":"broken`)

	s := openStore(t)
	rep, err := Import(s, dir)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if rep.ProfilesImported() != 1 || rep.ProfilesByType["socks"] != 1 {
		t.Fatalf("profiles by type = %v, want 1 socks", rep.ProfilesByType)
	}
	if len(rep.Skipped) != 1 || rep.Skipped[0].File != filepath.Join("profiles", "1.json") {
		t.Fatalf("skipped = %+v, want profiles/1.json", rep.Skipped)
	}
	if p, err := s.GetProfile(0); err != nil || p.Name != "ok" {
		t.Fatalf("good profile not imported: %+v (%v)", p, err)
	}
	if _, err := s.GetProfile(1); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("broken profile must not be imported, got %v", err)
	}
}

// TestImportOldFormatGroup — группа старого формата: profiles/<id>.json с
// type=group; профиль ссылается на неё по gid.
func TestImportOldFormatGroup(t *testing.T) {
	dir := mkTempDir(t)
	write(t, filepath.Join(dir, "profiles", "5.json"),
		`{"type":"group","id":5,"gid":-1,"bean":{"_v":0,"name":"OldSub","url":"https://old.example.com/sub",
		  "order":[6],"lastup":42,"front_proxy_id":-1}}`)
	write(t, filepath.Join(dir, "profiles", "6.json"),
		`{"type":"trojan","id":6,"gid":5,"yc":0,"bean":{"_v":0,"name":"tr","addr":"3.3.3.3","port":443,"pass":"pw"}}`)

	s := openStore(t)
	rep, err := Import(s, dir)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if rep.OldFmtGroups != 1 || rep.GroupsImported != 1 || rep.ProfilesImported() != 1 {
		t.Fatalf("report = %+v", rep)
	}
	g, err := s.GetGroup(5)
	if err != nil {
		t.Fatalf("old-format group not imported: %v", err)
	}
	// lastup=42 в bean — секунды; модель хранит мс (×1000, GroupUpdater.cpp:564).
	if g.Name != "OldSub" || g.Kind != model.GroupSubscription || g.SubscriptionURL != "https://old.example.com/sub" || g.LastUpdateMs != 42000 {
		t.Fatalf("group = %+v", g)
	}
	list, err := s.ListProfiles(5)
	if err != nil || len(list) != 1 || list[0].ID != 6 {
		t.Fatalf("profiles of old group = %+v (%v)", list, err)
	}
}

// TestImportOrphanProfile — профиль с несуществующей группой не теряется:
// импортируется в первую группу с предупреждением.
func TestImportOrphanProfile(t *testing.T) {
	dir := mkTempDir(t)
	write(t, filepath.Join(dir, "groups", "pm.json"), `{"groups":[0]}`)
	write(t, filepath.Join(dir, "groups", "0.json"), `{"id":0,"name":"G","order":[],"front_proxy_id":-1}`)
	write(t, filepath.Join(dir, "profiles", "0.json"),
		`{"type":"socks","id":0,"gid":77,"bean":{"_v":0,"name":"orphan","addr":"1.1.1.1","port":1,"v":5}}`)

	s := openStore(t)
	rep, err := Import(s, dir)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if rep.ProfilesImported() != 1 || len(rep.Warnings) != 1 {
		t.Fatalf("report = %+v", rep)
	}
	p, err := s.GetProfile(0)
	if err != nil || p.GroupID != 0 {
		t.Fatalf("orphan profile = %+v (%v)", p, err)
	}
}
