// SPDX-License-Identifier: GPL-3.0-only

package api

// preset_service_test.go — CRUD пользовательских наборов маршрутов (T-80/Э-1):
// Create/Update/Delete/ResetBuiltin через connect-клиента на настоящем сервере.

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/presets"
)

var bg = context.Background()

// copyOf — CreatePreset из источника (встроенного или своего).
func copyOf(t *testing.T, cs *clients, src string) *lavrayv1.RoutingPreset {
	t.Helper()
	res, err := cs.routing.CreatePreset(bg, connect.NewRequest(&lavrayv1.CreatePresetRequest{BuiltinSourceId: &src}))
	if err != nil {
		t.Fatalf("CreatePreset(copy of %s): %v", src, err)
	}
	return res.Msg
}

func fromValues(t *testing.T, cs *clients, p *lavrayv1.RoutingPreset) (*lavrayv1.RoutingPreset, error) {
	t.Helper()
	res, err := cs.routing.CreatePreset(bg, connect.NewRequest(&lavrayv1.CreatePresetRequest{Preset: p}))
	if err != nil {
		return nil, err
	}
	return res.Msg, nil
}

func getPreset(t *testing.T, cs *clients, id string) *lavrayv1.RoutingPreset {
	t.Helper()
	res, err := cs.routing.GetPreset(bg, connect.NewRequest(&lavrayv1.GetPresetRequest{Id: id}))
	if err != nil {
		t.Fatalf("GetPreset(%s): %v", id, err)
	}
	return res.Msg
}

func updatePreset(cs *clients, p *lavrayv1.RoutingPreset, rev int32) (*lavrayv1.UpdatePresetResponse, error) {
	res, err := cs.routing.UpdatePreset(bg, connect.NewRequest(&lavrayv1.UpdatePresetRequest{Preset: p, ExpectedRevision: rev}))
	if err != nil {
		return nil, err
	}
	return res.Msg, nil
}

func listIDs(t *testing.T, cs *clients) (ids []string, byID map[string]*lavrayv1.RoutingPreset) {
	t.Helper()
	res, err := cs.routing.ListPresets(bg, connect.NewRequest(&lavrayv1.ListPresetsRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	byID = map[string]*lavrayv1.RoutingPreset{}
	for _, p := range res.Msg.Presets {
		ids = append(ids, p.Id)
		byID[p.Id] = p
	}
	return ids, byID
}

func wantCode(t *testing.T, err error, want connect.Code, what string) {
	t.Helper()
	if got := connect.CodeOf(err); err == nil || got != want {
		t.Fatalf("%s: код %v (err=%v), ожидался %v", what, got, err, want)
	}
}

// TestPresetCRUD: копия lav-ru видна в ListPresets и GetPreset с builtin=false,
// источником и теми же правилами; изменение копии не трогает встроенный набор.
func TestPresetCRUD(t *testing.T) {
	cs := newE2E(t)
	cp := copyOf(t, cs, "lav-ru")
	if cp.Builtin || !strings.HasPrefix(cp.Id, "custom:") || cp.Revision != 1 {
		t.Fatalf("копия: builtin=%v id=%q rev=%d", cp.Builtin, cp.Id, cp.Revision)
	}
	if cp.BuiltinSourceId != "lav-ru" || cp.BuiltinSourceRevision == 0 {
		t.Fatalf("источник копии: %q rev %d", cp.BuiltinSourceId, cp.BuiltinSourceRevision)
	}
	ids, byID := listIDs(t, cs)
	if len(ids) != len(presets.BuiltinFiles)+1 || ids[len(presets.BuiltinFiles)] != cp.Id || byID[cp.Id].Builtin {
		t.Fatalf("ListPresets: %v", ids)
	}
	orig := getPreset(t, cs, "lav-ru")
	if !orig.Builtin || len(cp.DirectDomain) != len(orig.DirectDomain) || len(cp.ProcessRules) != len(orig.ProcessRules) {
		t.Fatalf("копия отличается от источника: direct %d/%d, process %d/%d",
			len(cp.DirectDomain), len(orig.DirectDomain), len(cp.ProcessRules), len(orig.ProcessRules))
	}
	// Правка копии.
	edited := proto.Clone(cp).(*lavrayv1.RoutingPreset)
	edited.DirectDomain = append(edited.DirectDomain, "added.example")
	upd, err := updatePreset(cs, edited, cp.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if got := getPreset(t, cs, cp.Id); len(got.DirectDomain) != len(cp.DirectDomain)+1 || got.Revision != upd.NewRevision {
		t.Fatalf("после Update: domains %d, rev %d", len(got.DirectDomain), got.Revision)
	}
	if got := getPreset(t, cs, "lav-ru"); len(got.DirectDomain) != len(orig.DirectDomain) {
		t.Fatal("встроенный набор изменился вместе с копией")
	}
	// Удаление.
	del, err := cs.routing.DeletePreset(bg, connect.NewRequest(&lavrayv1.DeletePresetRequest{Id: cp.Id}))
	if err != nil || del.Msg.NeedsProfileRestart || del.Msg.NewActiveId != "" {
		t.Fatalf("Delete: %+v %v", del, err)
	}
	if ids, _ := listIDs(t, cs); len(ids) != len(presets.BuiltinFiles) {
		t.Fatalf("после удаления: %v", ids)
	}
	_, err = cs.routing.GetPreset(bg, connect.NewRequest(&lavrayv1.GetPresetRequest{Id: cp.Id}))
	wantCode(t, err, connect.CodeNotFound, "GetPreset удалённого")
}

// TestCreateFromValuesAndName: набор из значений получает id из имени (кириллица
// транслитом) и суффикс при совпадении; имя 64 символа — предел.
func TestCreateFromValuesAndName(t *testing.T) {
	cs := newE2E(t)
	mk := func(name string) (*lavrayv1.RoutingPreset, error) {
		return fromValues(t, cs, &lavrayv1.RoutingPreset{DisplayName: name, ProxyDomain: []string{"a.example"}})
	}
	a, err := mk("Мой набор")
	if err != nil || a.Id != "custom:moy-nabor" || a.DisplayName != "Мой набор" || a.BuiltinSourceId != "" {
		t.Fatalf("первый: %+v %v", a, err)
	}
	b, err := mk("Мой набор")
	if err != nil || b.Id != "custom:moy-nabor-2" {
		t.Fatalf("второй: %+v %v", b, err)
	}
	if _, err := mk(strings.Repeat("я", 65)); connect.CodeOf(err) != connect.CodeInvalidArgument || !strings.Contains(err.Error(), "name") {
		t.Fatalf("65 символов: %v", err)
	}
	if _, err := mk("   "); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("пустое имя: %v", err)
	}
	if _, err := mk(strings.Repeat("я", 64)); err != nil {
		t.Fatalf("64 символа: %v", err)
	}
	// Ошибка проверки называет путь поля.
	_, err = fromValues(t, cs, &lavrayv1.RoutingPreset{DisplayName: "Bad", DirectDomain: []string{"geoip:ru"}})
	if connect.CodeOf(err) != connect.CodeInvalidArgument || !strings.Contains(err.Error(), "direct_domain") {
		t.Fatalf("путь поля: %v", err)
	}
}

// TestCreateSourceXorValues (I): ровно одно из builtin_source_id / preset.
func TestCreateSourceXorValues(t *testing.T) {
	cs := newE2E(t)
	src := "global"
	cases := map[string]*lavrayv1.CreatePresetRequest{
		"оба":        {BuiltinSourceId: &src, Preset: &lavrayv1.RoutingPreset{DisplayName: "X"}},
		"ни одного":  {},
		"пустой id":  {BuiltinSourceId: new(string)},
		"пустой id+": {BuiltinSourceId: new(string), Preset: &lavrayv1.RoutingPreset{DisplayName: "X"}},
	}
	for name, req := range cases {
		_, err := cs.routing.CreatePreset(bg, connect.NewRequest(req))
		if name == "пустой id+" {
			// пустой источник = источник не задан, остаются значения → создаётся
			if err != nil {
				t.Errorf("%s: %v", name, err)
			}
			continue
		}
		wantCode(t, err, connect.CodeInvalidArgument, name)
	}
	missing := "no-such-preset"
	_, err := cs.routing.CreatePreset(bg, connect.NewRequest(&lavrayv1.CreatePresetRequest{BuiltinSourceId: &missing}))
	wantCode(t, err, connect.CodeNotFound, "нет источника")
}

// TestCreateLimit (B): сотый набор создаётся, сто первый — RESOURCE_EXHAUSTED;
// после удаления одного место освобождается.
func TestCreateLimit(t *testing.T) {
	cs := newE2E(t)
	var last *lavrayv1.RoutingPreset
	for i := 0; i < 100; i++ {
		last = copyOf(t, cs, "global")
	}
	src := "global"
	_, err := cs.routing.CreatePreset(bg, connect.NewRequest(&lavrayv1.CreatePresetRequest{BuiltinSourceId: &src}))
	wantCode(t, err, connect.CodeResourceExhausted, "101-й набор")
	if ids, _ := listIDs(t, cs); len(ids) != len(presets.BuiltinFiles)+100 {
		t.Fatalf("наборов %d, ожидалось %d", len(ids), len(presets.BuiltinFiles)+100)
	}
	if _, err := cs.routing.DeletePreset(bg, connect.NewRequest(&lavrayv1.DeletePresetRequest{Id: last.Id})); err != nil {
		t.Fatal(err)
	}
	copyOf(t, cs, "global")
}

// TestCreateTotalSizeLimit (B): все наборы вместе — до 4 МиБ (RESOURCE_EXHAUSTED),
// отказ ничего не записывает.
func TestCreateTotalSizeLimit(t *testing.T) {
	cs := newE2E(t)
	big := func(name string) *lavrayv1.RoutingPreset {
		p := &lavrayv1.RoutingPreset{DisplayName: name}
		for i := 0; i < 10000; i++ {
			d := fmt.Sprintf("host-%05d.example.org", i)
			p.ProxyDomain, p.DirectDomain, p.BlockDomain = append(p.ProxyDomain, d), append(p.DirectDomain, d), append(p.BlockDomain, d)
			p.ProxyIp = append(p.ProxyIp, fmt.Sprintf("10.%d.%d.0/24", i/256, i%256))
			p.DirectIp = append(p.DirectIp, fmt.Sprintf("172.%d.%d.0/24", i/256, i%256))
			p.BlockIp = append(p.BlockIp, fmt.Sprintf("192.%d.%d.0/24", i/256, i%256))
		}
		return p
	}
	var made int
	var err error
	for i := 0; i < 6 && err == nil; i++ {
		_, err = fromValues(t, cs, big(fmt.Sprintf("Big %d", i)))
		if err == nil {
			made++
		}
	}
	wantCode(t, err, connect.CodeResourceExhausted, "превышение 4 МиБ")
	if made < 1 || made > 5 {
		t.Fatalf("создано %d больших наборов", made)
	}
	if ids, _ := listIDs(t, cs); len(ids) != len(presets.BuiltinFiles)+made {
		t.Fatalf("отказ изменил список: %d наборов, создано %d", len(ids), made)
	}
}

// TestUpdateBuiltinIsRejected: встроенный набор не правится ни по id, ни через
// подмену флага builtin в запросе.
func TestUpdateBuiltinIsRejected(t *testing.T) {
	cs := newE2E(t)
	orig := getPreset(t, cs, "lav-ru")
	edit := proto.Clone(orig).(*lavrayv1.RoutingPreset)
	edit.DirectDomain = []string{"x.example"}
	_, err := updatePreset(cs, edit, orig.Revision)
	wantCode(t, err, connect.CodeFailedPrecondition, "Update встроенного")
	edit.Builtin = false
	_, err = updatePreset(cs, edit, orig.Revision)
	wantCode(t, err, connect.CodeFailedPrecondition, "Update встроенного с builtin=false")
	if got := getPreset(t, cs, "lav-ru"); len(got.DirectDomain) != len(orig.DirectDomain) {
		t.Fatal("встроенный набор изменился")
	}
	_, err = updatePreset(cs, &lavrayv1.RoutingPreset{Id: "custom:none", DisplayName: "n"}, 1)
	wantCode(t, err, connect.CodeNotFound, "Update несуществующего")
	_, err = cs.routing.UpdatePreset(bg, connect.NewRequest(&lavrayv1.UpdatePresetRequest{}))
	wantCode(t, err, connect.CodeInvalidArgument, "Update без набора")
}

// TestUpdateIncrementsRevision: ревизия растёт на 1 при каждой записи; источник
// и идентичность нельзя подменить из запроса.
func TestUpdateIncrementsRevision(t *testing.T) {
	cs := newE2E(t)
	cp := copyOf(t, cs, "lav-ru")
	cur := cp
	for i := 1; i <= 3; i++ {
		next := proto.Clone(cur).(*lavrayv1.RoutingPreset)
		next.BlockDomain = append(next.BlockDomain, fmt.Sprintf("b%d.example", i))
		next.BuiltinSourceId, next.BuiltinSourceRevision, next.Builtin, next.SourceUpdated = "global", 99, true, true
		res, err := updatePreset(cs, next, cur.Revision)
		if err != nil {
			t.Fatal(err)
		}
		if res.NewRevision != cur.Revision+1 || res.Preset.Revision != res.NewRevision {
			t.Fatalf("итерация %d: new_revision=%d preset.revision=%d (было %d)", i, res.NewRevision, res.Preset.Revision, cur.Revision)
		}
		if res.Preset.Builtin || res.Preset.BuiltinSourceId != "lav-ru" || res.Preset.BuiltinSourceRevision != cp.BuiltinSourceRevision || res.Preset.SourceUpdated {
			t.Fatalf("клиент подменил источник: %+v", res.Preset)
		}
		cur = res.Preset
	}
	if got := getPreset(t, cs, cp.Id); got.Revision != 4 || got.Id != cp.Id {
		t.Fatalf("итог: rev %d id %q", got.Revision, got.Id)
	}
}

// TestRenameKeepsListsAndLanguages: переименование меняет имя и id не трогает;
// запрос без имени-изменения хранит прежние переводы имени.
func TestRenameAndEmptyDNSKeepsStored(t *testing.T) {
	cs := newE2E(t)
	cp := copyOf(t, cs, "lav-ru")
	edit := proto.Clone(cp).(*lavrayv1.RoutingPreset)
	edit.DisplayName = "Новое имя"
	edit.Dns = nil // клиент не прислал DNS — хранится прежний
	res, err := updatePreset(cs, edit, cp.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if res.Preset.Id != cp.Id || res.Preset.DisplayName != "Новое имя" {
		t.Fatalf("после переименования: %q / %q", res.Preset.Id, res.Preset.DisplayName)
	}
	if !proto.Equal(res.Preset.Dns, cp.Dns) {
		t.Fatalf("DNS потерян: %v ≠ %v", res.Preset.Dns, cp.Dns)
	}
	_, err = updatePreset(cs, func() *lavrayv1.RoutingPreset {
		e := proto.Clone(res.Preset).(*lavrayv1.RoutingPreset)
		e.DisplayName = " "
		return e
	}(), res.NewRevision)
	wantCode(t, err, connect.CodeInvalidArgument, "пустое имя при Update")
}

// TestUpdatePresetConflictIsAborted: устаревшая ревизия — ABORTED, набор не меняется.
func TestUpdatePresetConflictIsAborted(t *testing.T) {
	cs := newE2E(t)
	cp := copyOf(t, cs, "global")
	first := proto.Clone(cp).(*lavrayv1.RoutingPreset)
	first.DirectDomain = []string{"one.example"}
	if _, err := updatePreset(cs, first, cp.Revision); err != nil {
		t.Fatal(err)
	}
	stale := proto.Clone(cp).(*lavrayv1.RoutingPreset)
	stale.DirectDomain = []string{"two.example"}
	_, err := updatePreset(cs, stale, cp.Revision)
	wantCode(t, err, connect.CodeAborted, "устаревшая ревизия")
	if got := getPreset(t, cs, cp.Id); len(got.DirectDomain) != 1 || got.DirectDomain[0] != "one.example" {
		t.Fatalf("конфликтная правка просочилась: %v", got.DirectDomain)
	}
	_, err = updatePreset(cs, stale, cp.Revision+5)
	wantCode(t, err, connect.CodeAborted, "ревизия из будущего")
}

// TestConcurrentUpdatesExactlyOneAborted (C): N параллельных правок с одной
// ревизией — ровно одна проходит, остальные ABORTED, итог — ревизия +1.
func TestConcurrentUpdatesExactlyOneAborted(t *testing.T) {
	cs := newE2E(t)
	cp := copyOf(t, cs, "global")
	const n = 8
	var wg sync.WaitGroup
	codes := make([]connect.Code, n)
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			p := proto.Clone(cp).(*lavrayv1.RoutingPreset)
			p.DirectDomain = []string{fmt.Sprintf("w%d.example", i)}
			<-start
			if _, err := updatePreset(cs, p, cp.Revision); err != nil {
				codes[i] = connect.CodeOf(err)
			}
		}(i)
	}
	close(start)
	wg.Wait()
	ok, aborted := 0, 0
	for _, c := range codes {
		switch c {
		case 0:
			ok++
		case connect.CodeAborted:
			aborted++
		default:
			t.Errorf("неожиданный код %v", c)
		}
	}
	if ok != 1 || aborted != n-1 {
		t.Fatalf("успешных %d, ABORTED %d из %d", ok, aborted, n)
	}
	if got := getPreset(t, cs, cp.Id); got.Revision != cp.Revision+1 || len(got.DirectDomain) != 1 {
		t.Fatalf("итог: rev %d, domains %v", got.Revision, got.DirectDomain)
	}
}

// TestUpdateActiveNeedsRestart: правка активного набора при работающем
// соединении просит перезапуск; неактивного или при остановленном ядре — нет.
func TestUpdateActiveNeedsRestart(t *testing.T) {
	f := newSvcFixture(t, nil)
	s := f.srv
	cp, err := s.CreatePreset(bg, connect.NewRequest(&lavrayv1.CreatePresetRequest{BuiltinSourceId: strPtr("global")}))
	if err != nil {
		t.Fatal(err)
	}
	upd := func(rev int32) *lavrayv1.UpdatePresetResponse {
		p := proto.Clone(cp.Msg).(*lavrayv1.RoutingPreset)
		p.DirectDomain = []string{fmt.Sprintf("r%d.example", rev)}
		res, err := s.UpdatePreset(bg, connect.NewRequest(&lavrayv1.UpdatePresetRequest{Preset: p, ExpectedRevision: rev}))
		if err != nil {
			t.Fatal(err)
		}
		return res.Msg
	}
	activate(t, s, cp.Msg.Id)
	if upd(1).NeedsProfileRestart {
		t.Error("ядро остановлено — перезапуск не нужен")
	}
	pr := f.trojan(t)
	if _, err := s.Connect(bg, connect.NewRequest(&lavrayv1.ConnectRequest{ProfileId: &pr.ID})); err != nil {
		t.Fatal(err)
	}
	if !upd(2).NeedsProfileRestart {
		t.Error("активный набор изменён при работающем соединении — нужен перезапуск")
	}
	activate(t, s, "global")
	if upd(3).NeedsProfileRestart {
		t.Error("правка неактивного набора не требует перезапуска")
	}
}

func strPtr(s string) *string { return &s }

// TestDeleteBuiltinIsRejected: встроенные наборы не удаляются; чужой id — NOT_FOUND.
func TestDeleteBuiltinIsRejected(t *testing.T) {
	cs := newE2E(t)
	for _, id := range []string{"lav-ru", "global", "all-direct"} {
		_, err := cs.routing.DeletePreset(bg, connect.NewRequest(&lavrayv1.DeletePresetRequest{Id: id}))
		wantCode(t, err, connect.CodeFailedPrecondition, "Delete "+id)
	}
	_, err := cs.routing.DeletePreset(bg, connect.NewRequest(&lavrayv1.DeletePresetRequest{Id: "custom:none"}))
	wantCode(t, err, connect.CodeNotFound, "Delete несуществующего")
	if ids, _ := listIDs(t, cs); len(ids) != len(presets.BuiltinFiles) {
		t.Fatalf("встроенные пропали: %v", ids)
	}
}

// TestDeleteActivePresetFallsBackToGlobal: удаление активного переключает на
// global и просит перезапуск, если соединение работает; id можно занять снова,
// и переопределение DNS удалённого набора на новый не переходит.
func TestDeleteActivePresetFallsBackToGlobal(t *testing.T) {
	f := newSvcFixture(t, nil)
	s := f.srv
	made, err := s.CreatePreset(bg, connect.NewRequest(&lavrayv1.CreatePresetRequest{BuiltinSourceId: strPtr("lav-ru")}))
	if err != nil {
		t.Fatal(err)
	}
	id := made.Msg.Id
	activate(t, s, id)
	if err := setDNS(t, s, "tunnel", nil); err != nil {
		t.Fatal(err)
	}
	pr := f.trojan(t)
	if _, err := s.Connect(bg, connect.NewRequest(&lavrayv1.ConnectRequest{ProfileId: &pr.ID})); err != nil {
		t.Fatal(err)
	}
	del, err := s.DeletePreset(bg, connect.NewRequest(&lavrayv1.DeletePresetRequest{Id: id}))
	if err != nil {
		t.Fatal(err)
	}
	if del.Msg.NewActiveId != "global" || !del.Msg.NeedsProfileRestart {
		t.Fatalf("Delete активного: %+v", del.Msg)
	}
	if got := s.activePresetID(); got != "global" {
		t.Fatalf("активен %q", got)
	}
	again, err := s.CreatePreset(bg, connect.NewRequest(&lavrayv1.CreatePresetRequest{BuiltinSourceId: strPtr("lav-ru")}))
	if err != nil || again.Msg.Id != id {
		t.Fatalf("id не освободился: %v %v", again, err)
	}
	activate(t, s, id)
	if c := getDNS(t, s); c.GetChoice() != "auto" {
		t.Fatalf("переопределение DNS удалённого набора перешло на новый: %q", c.GetChoice())
	}
}

// TestResetCopyRestoresSource: сброс возвращает копию к источнику, но имя
// остаётся; ревизия растёт, запись источника обновляется.
func TestResetCopyRestoresSource(t *testing.T) {
	cs := newE2E(t)
	cp := copyOf(t, cs, "lav-ru")
	edit := proto.Clone(cp).(*lavrayv1.RoutingPreset)
	edit.DisplayName = "Мой RU"
	edit.DirectDomain, edit.ProcessRules, edit.DefaultOutbound = []string{"only.example"}, nil, lavrayv1.Outbound_OUTBOUND_BLOCK
	upd, err := updatePreset(cs, edit, cp.Revision)
	if err != nil {
		t.Fatal(err)
	}
	res, err := cs.routing.ResetBuiltinPreset(bg, connect.NewRequest(&lavrayv1.ResetBuiltinPresetRequest{Id: cp.Id}))
	if err != nil {
		t.Fatal(err)
	}
	got, orig := res.Msg, getPreset(t, cs, "lav-ru")
	if got.Id != cp.Id || got.DisplayName != "Мой RU" || got.Builtin || got.Revision != upd.NewRevision+1 {
		t.Fatalf("после сброса: id=%q name=%q builtin=%v rev=%d", got.Id, got.DisplayName, got.Builtin, got.Revision)
	}
	if fmt.Sprint(got.DirectDomain) != fmt.Sprint(orig.DirectDomain) || len(got.ProcessRules) != len(orig.ProcessRules) ||
		got.DefaultOutbound != orig.DefaultOutbound || !proto.Equal(got.Dns, orig.Dns) {
		t.Fatal("сброс не вернул значения источника")
	}
	if got.BuiltinSourceId != "lav-ru" || got.BuiltinSourceRevision != orig.Revision || got.SourceUpdated {
		t.Fatalf("источник: %q/%d updated=%v", got.BuiltinSourceId, got.BuiltinSourceRevision, got.SourceUpdated)
	}
	if again := getPreset(t, cs, cp.Id); again.Revision != got.Revision {
		t.Fatal("сброс не сохранён")
	}
}

// TestResetWithoutSourcePrecondition (I): сбросить можно только копию со
// встроенным источником; id встроенного и набор с нуля — FAILED_PRECONDITION.
func TestResetWithoutSourcePrecondition(t *testing.T) {
	cs := newE2E(t)
	scratch, err := fromValues(t, cs, &lavrayv1.RoutingPreset{DisplayName: "С нуля"})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"lav-ru", scratch.Id} {
		_, err := cs.routing.ResetBuiltinPreset(bg, connect.NewRequest(&lavrayv1.ResetBuiltinPresetRequest{Id: id}))
		wantCode(t, err, connect.CodeFailedPrecondition, "Reset "+id)
	}
	_, err = cs.routing.ResetBuiltinPreset(bg, connect.NewRequest(&lavrayv1.ResetBuiltinPresetRequest{Id: "custom:none"}))
	wantCode(t, err, connect.CodeNotFound, "Reset несуществующего")
}

// TestSourceUpdatedFlag: если ревизия встроенного источника выше записанной в
// копии, ListPresets/GetPreset отдают source_updated=true.
func TestSourceUpdatedFlag(t *testing.T) {
	f := newSvcFixture(t, nil)
	s := f.srv
	made, err := s.CreatePreset(bg, connect.NewRequest(&lavrayv1.CreatePresetRequest{BuiltinSourceId: strPtr("global")}))
	if err != nil || made.Msg.SourceUpdated {
		t.Fatalf("свежая копия: %+v %v", made, err)
	}
	if err := s.presetRepo.Modify(func(list []*presets.Preset) ([]*presets.Preset, error) {
		list[0].SourceRevision-- // «копия снята со старой ревизии»
		return list, nil
	}); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetPreset(bg, connect.NewRequest(&lavrayv1.GetPresetRequest{Id: made.Msg.Id}))
	if err != nil || !got.Msg.SourceUpdated {
		t.Fatalf("source_updated не выставлен: %+v %v", got, err)
	}
	list, _ := s.ListPresets(bg, connect.NewRequest(&lavrayv1.ListPresetsRequest{}))
	for _, p := range list.Msg.Presets {
		if p.SourceUpdated != (p.Id == made.Msg.Id) {
			t.Errorf("%s: source_updated=%v", p.Id, p.SourceUpdated)
		}
	}
}

// TestCorruptMetaDataLoss (R): повреждённая запись — DATA_LOSS на каждом пути,
// встроенные наборы по прямому id по-прежнему читаются, запись не затирается.
func TestCorruptMetaDataLoss(t *testing.T) {
	cs := newE2E(t)
	const garbage = `[{"id":"custom:a", broken`
	if err := cs.st.SetMeta(metaCustomPresets, garbage); err != nil {
		t.Fatal(err)
	}
	src := "global"
	calls := map[string]func() error{
		"ListPresets": func() error {
			_, err := cs.routing.ListPresets(bg, connect.NewRequest(&lavrayv1.ListPresetsRequest{}))
			return err
		},
		"CreatePreset": func() error {
			_, err := cs.routing.CreatePreset(bg, connect.NewRequest(&lavrayv1.CreatePresetRequest{BuiltinSourceId: &src}))
			return err
		},
		"UpdatePreset": func() error {
			_, err := updatePreset(cs, &lavrayv1.RoutingPreset{Id: "custom:a", DisplayName: "a"}, 1)
			return err
		},
		"DeletePreset": func() error {
			_, err := cs.routing.DeletePreset(bg, connect.NewRequest(&lavrayv1.DeletePresetRequest{Id: "custom:a"}))
			return err
		},
		"GetPreset": func() error {
			_, err := cs.routing.GetPreset(bg, connect.NewRequest(&lavrayv1.GetPresetRequest{Id: "custom:a"}))
			return err
		},
	}
	for name, call := range calls {
		wantCode(t, call(), connect.CodeDataLoss, name)
	}
	if _, err := cs.routing.GetPreset(bg, connect.NewRequest(&lavrayv1.GetPresetRequest{Id: "global"})); err != nil {
		t.Errorf("встроенный набор недоступен из-за порчи пользовательских: %v", err)
	}
	if raw, _, _ := cs.st.GetMeta(metaCustomPresets); raw != garbage {
		t.Fatalf("повреждённая запись перезаписана: %q", raw)
	}
	// Элемент null внутри массива — тоже порча.
	if err := cs.st.SetMeta(metaCustomPresets, `[null]`); err != nil {
		t.Fatal(err)
	}
	wantCode(t, calls["ListPresets"](), connect.CodeDataLoss, "[null]")
}

// ResetBuiltinPreset приводит сброшенную копию к хранимому виду: custom-правила
// встроенного набора лежат как {"rules":[…]}, а в копии хранится массив.
func TestResetNormalizesCustomRules(t *testing.T) {
	cs := newE2E(t)
	orig := getPreset(t, cs, "lav-ru")
	if !strings.HasPrefix(orig.GetCustomRulesJson(), "{") {
		t.Skipf("у lav-ru custom_rules уже не объект: %.40q", orig.GetCustomRulesJson())
	}
	cp := copyOf(t, cs, "lav-ru")
	edit := proto.Clone(cp).(*lavrayv1.RoutingPreset)
	edit.CustomRulesJson = nil
	upd, err := updatePreset(cs, edit, cp.Revision)
	if err != nil {
		t.Fatal(err)
	}
	res, err := cs.routing.ResetBuiltinPreset(bg, connect.NewRequest(&lavrayv1.ResetBuiltinPresetRequest{Id: cp.Id}))
	if err != nil {
		t.Fatal(err)
	}
	got := res.Msg.GetCustomRulesJson()
	if !strings.HasPrefix(got, "[") || strings.Contains(got, `"rules"`) {
		t.Fatalf("после сброса custom_rules = %.60q, ожидался массив", got)
	}
	// Те же правила, что у источника, и сборка конфига их принимает.
	want, _ := presets.NormalizeCustomRules(orig.GetCustomRulesJson())
	if got != want {
		t.Fatalf("правила копии отличаются от источника")
	}
	if stored := getPreset(t, cs, cp.Id); stored.GetCustomRulesJson() != got || stored.Revision != upd.NewRevision+1 {
		t.Fatalf("сохранено другое: rev %d", stored.Revision)
	}
}
