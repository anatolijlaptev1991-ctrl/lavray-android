// SPDX-License-Identifier: GPL-3.0-only

package nekoimport

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/store"
)

// dumpDB — всё, что перенос меняет в базе: группы, профили (с id), import_map.
func dumpDB(t *testing.T, s *store.Store) []string {
	t.Helper()
	var out []string
	groups, err := s.ListGroups()
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range groups {
		out = append(out, "g:"+g.Name)
		for _, p := range allProfilesOf(t, s, g.ID) {
			out = append(out, "p:"+p.Name)
		}
	}
	rows, err := s.ImportMapRows(SourceNekoBox)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		out = append(out, "m:"+r.Kind+":"+r.SourceKey)
	}
	sort.Strings(out)
	return out
}

func allProfilesOf(t *testing.T, s *store.Store, gid int64) []*model.Profile {
	t.Helper()
	list, err := s.ListProfiles(gid)
	if err != nil {
		t.Fatal(err)
	}
	return list
}

// Scan читает только каталог: база не нужна, каталог не меняется (S).
func TestScanDoesNotWrite(t *testing.T) {
	dir := copyTree(t, fixtureConfig)
	before := snapshotDir(t, dir)
	s := openStore(t)
	dbBefore := dumpDB(t, s)

	plan, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Groups) != 2 || len(plan.Profiles) != 7 || len(plan.Routes) != 2 {
		t.Fatalf("plan: %d групп, %d профилей, %d маршрутов", len(plan.Groups), len(plan.Profiles), len(plan.Routes))
	}
	if !reflect.DeepEqual(dumpDB(t, s), dbBefore) {
		t.Error("Scan изменил базу")
	}
	if !reflect.DeepEqual(snapshotDir(t, dir), before) {
		t.Error("Scan изменил исходный каталог")
	}
}

// snapshotDir — имена и размеры файлов каталога.
func snapshotDir(t *testing.T, dir string) map[string]int64 {
	t.Helper()
	out := map[string]int64{}
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			out[path] = info.Size()
		}
		return nil
	})
	return out
}

// Отчёт сухого прогона (Scan → Remap) совпадает с отчётом настоящего переноса (D).
func TestPreviewMatchesImport(t *testing.T) {
	s := openStore(t)
	seedProfile(t, s, "local-1")
	plan, err := Scan(fixtureConfig)
	if err != nil {
		t.Fatal(err)
	}
	st, err := LoadState(s)
	if err != nil {
		t.Fatal(err)
	}
	preview := Remap(plan, st).Report
	real, err := Import(s, fixtureConfig)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(&preview, real) {
		t.Errorf("предпросмотр %+v\nне равен переносу %+v", preview, *real)
	}
	if real.Renumbered == 0 {
		t.Error("тест не проверил перенумерацию: коллизий нет")
	}
}

// Профиль LAVRay id=1 и профиль NekoBox id=1 сосуществуют; NekoBox получает
// новый id, а профиль LAVRay остаётся на месте (раньше NekoBox-профиль терялся).
func TestImportRemapsIDOnCollision(t *testing.T) {
	s := openStore(t)
	local := seedProfile(t, s, "local-1")
	if local.ID != 1 {
		t.Fatalf("первый профиль получил id %d, тест рассчитан на 1", local.ID)
	}
	rep, err := Import(s, fixtureConfig)
	if err != nil {
		t.Fatal(err)
	}
	if rep.AlreadyPresent != 0 || rep.ProfilesImported() != 7 || rep.Renumbered == 0 {
		t.Fatalf("отчёт: %+v", rep)
	}
	if p, err := s.GetProfile(1); err != nil || p.Name != "local-1" {
		t.Fatalf("профиль LAVRay id=1 затронут: %+v %v", p, err)
	}
	moved := profileByName(t, s, "vless reality")
	if moved.ID == 1 {
		t.Error("профиль NekoBox занял чужой id")
	}
}

// Цепочка и front_proxy_id указывают на итоговые id, а не на id NekoBox.
func TestRemapRewritesChainAndFrontProxy(t *testing.T) {
	s := openStore(t)
	for _, n := range []string{"a", "b", "c", "d"} { // занимают id 1..4
		seedProfile(t, s, n)
	}
	if _, err := Import(s, fixtureConfig); err != nil {
		t.Fatal(err)
	}
	chain := profileByName(t, s, "chain 3 then 2")
	cd := chain.Detail.(*model.ChainDetail)
	want := []int64{profileByName(t, s, "trojan tls").ID, profileByName(t, s, "vmess ws").ID}
	if !reflect.DeepEqual(cd.ProfileIDs, want) {
		t.Errorf("состав цепочки %v, ожидалось %v", cd.ProfileIDs, want)
	}
	groups, err := s.ListGroups()
	if err != nil {
		t.Fatal(err)
	}
	var sub *model.Group
	for _, g := range groups {
		if g.Name == "Подписка" {
			sub = g
		}
	}
	if sub == nil || sub.FrontProxyID == nil || *sub.FrontProxyID != profileByName(t, s, "vless reality").ID {
		t.Errorf("front_proxy_id группы-подписки = %+v", sub)
	}
}

// Звено цепочки, которого нет в каталоге, отбрасывается, а не указывает на чужой профиль.
func TestRemapDropsMissingChainMember(t *testing.T) {
	dir := copyTree(t, fixtureConfig)
	write(t, filepath.Join(dir, "profiles", "6.json"),
		`{"type":"chain","id":6,"gid":0,"bean":{"name":"chain","list":[3,77,2]}}`)
	s := openStore(t)
	rep, err := Import(s, dir)
	if err != nil {
		t.Fatal(err)
	}
	cd := profileByName(t, s, "chain").Detail.(*model.ChainDetail)
	if len(cd.ProfileIDs) != 2 {
		t.Errorf("состав цепочки %v, ожидались два звена", cd.ProfileIDs)
	}
	if !hasWarning(rep.Warnings, "member 77") {
		t.Errorf("нет предупреждения о звене 77: %v", rep.Warnings)
	}
}

func hasWarning(warns []string, sub string) bool {
	for _, w := range warns {
		if strings.Contains(w, sub) {
			return true
		}
	}
	return false
}

// Повторный перенос не создаёт дублей и после перенумерации (D): ключ — исходный id.
func TestImportIsIdempotentBySourceID(t *testing.T) {
	s := openStore(t)
	seedProfile(t, s, "local-1")
	if _, err := Import(s, fixtureConfig); err != nil {
		t.Fatal(err)
	}
	after := dumpDB(t, s)
	rep, err := Import(s, fixtureConfig)
	if err != nil {
		t.Fatal(err)
	}
	if rep.GroupsImported != 0 || rep.ProfilesImported() != 0 || rep.AlreadyPresent != 9 { // 2 группы + 7 профилей
		t.Fatalf("второй перенос: %+v", rep)
	}
	if !reflect.DeepEqual(dumpDB(t, s), after) {
		t.Error("второй перенос изменил базу")
	}
}

// Локальная копия удалена — повторный перенос возвращает её (D).
func TestReimportAfterLocalDelete(t *testing.T) {
	s := openStore(t)
	if _, err := Import(s, fixtureConfig); err != nil {
		t.Fatal(err)
	}
	gone := profileByName(t, s, "trojan tls")
	if err := s.DeleteProfile(gone.ID); err != nil {
		t.Fatal(err)
	}
	rep, err := Import(s, fixtureConfig)
	if err != nil {
		t.Fatal(err)
	}
	if rep.ProfilesImported() != 1 || rep.ProfilesByType["trojan"] != 1 {
		t.Fatalf("ожидался один возвращённый trojan: %+v", rep)
	}
	if n := len(allProfiles(t, s)); n != 7 {
		t.Errorf("после повторного переноса %d профилей, ожидалось 7 (без дублей)", n)
	}
	profileByName(t, s, "trojan tls")
}

// Тот же id NekoBox, но другой тип или адрес — новая запись (I).
func TestSameIDDifferentContentIsNew(t *testing.T) {
	dir := copyTree(t, fixtureConfig)
	s := openStore(t)
	if _, err := Import(s, dir); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "profiles", "3.json"),
		`{"type":"trojan","id":3,"gid":0,"bean":{"name":"trojan moved","addr":"203.0.113.99","port":443,"pass":"x"}}`)
	rep, err := Import(s, dir)
	if err != nil {
		t.Fatal(err)
	}
	if rep.ProfilesByType["trojan"] != 1 || rep.AlreadyPresent != 8 {
		t.Fatalf("ожидался один новый trojan и 8 прежних: %+v", rep)
	}
	profileByName(t, s, "trojan tls")
	profileByName(t, s, "trojan moved")
}

// Сбой посреди переноса откатывает всё: ни групп, ни профилей, ни import_map (R).
func TestImportIsOneTransaction(t *testing.T) {
	s := openStore(t)
	plan, err := Scan(fixtureConfig)
	if err != nil {
		t.Fatal(err)
	}
	st, err := LoadState(s)
	if err != nil {
		t.Fatal(err)
	}
	res := Remap(plan, st)
	last := res.Profiles[len(res.Profiles)-1].Profile
	last.GroupID = 424242 // группы нет: вставка последнего профиля упадёт
	before := dumpDB(t, s)
	if err := Apply(s, res, `[]`); err == nil {
		t.Fatal("Apply принял профиль без группы")
	}
	if !reflect.DeepEqual(dumpDB(t, s), before) {
		t.Errorf("после сбоя база изменилась:\n%v\nбыло\n%v", dumpDB(t, s), before)
	}
	if v, ok, _ := s.GetMeta(store.MetaCustomPresets); ok {
		t.Errorf("наборы записаны вопреки откату: %q", v)
	}
}

// Параллельная запись заняла id между чтением State и Apply → ErrConflict.
func TestApplyConflictIsReported(t *testing.T) {
	s := openStore(t)
	plan, err := Scan(fixtureConfig)
	if err != nil {
		t.Fatal(err)
	}
	st, err := LoadState(s)
	if err != nil {
		t.Fatal(err)
	}
	res := Remap(plan, st)
	seedProfile(t, s, "racer") // получает id 1, который Remap оставил за NekoBox
	if err := Apply(s, res, ""); err == nil || !strings.Contains(err.Error(), ErrConflict.Error()) {
		t.Fatalf("ожидался ErrConflict, получено %v", err)
	}
}

// Custom-поля переносятся неподтверждёнными: флага и хэша нет (S).
func TestCustomFieldsUnconfirmed(t *testing.T) {
	s := openStore(t)
	if _, err := Import(s, fixtureConfig); err != nil {
		t.Fatal(err)
	}
	p := profileByName(t, s, "socks custom")
	if p.CustomConfigJSON == "" || p.CustomOutboundJSON == "" {
		t.Fatalf("custom-поля потеряны: %+v", p)
	}
	if p.CustomFieldsConfirmed || p.CustomHash != "" {
		t.Errorf("custom-поля подтверждены при переносе: confirmed=%v hash=%q", p.CustomFieldsConfirmed, p.CustomHash)
	}
}

// Пустой каталог — пустой план без ошибки (E).
func TestEmptyDir(t *testing.T) {
	dir := mkTempDir(t)
	plan, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Groups)+len(plan.Profiles)+len(plan.Routes)+len(plan.Settings) != 0 {
		t.Errorf("план пустого каталога не пуст: %+v", plan)
	}
	rep, err := Import(openStore(t), dir)
	if err != nil || rep.GroupsImported != 0 || rep.ProfilesImported() != 0 {
		t.Errorf("перенос пустого каталога: %+v %v", rep, err)
	}
}

// Сбой в самом конце переноса (после записи профилей, настроек и наборов)
// откатывает и настройки, и meta с наборами, а не только профили (R).
func TestImportRollsBackSettingsAndMeta(t *testing.T) {
	s := openStore(t)
	plan, err := Scan(fixtureConfig)
	if err != nil {
		t.Fatal(err)
	}
	st, err := LoadState(s)
	if err != nil {
		t.Fatal(err)
	}
	res := Remap(plan, st)
	res.Settings = []PlanSetting{{Key: "inbound.port", JSON: "3090"}}
	// kind='bogus' нарушает CHECK таблицы import_map: запись падает последней.
	res.PresetRows = []store.ImportMapRow{{Source: SourceNekoBox, Kind: "bogus", SourceKey: "k", LocalID: "custom:x"}}
	before := dumpDB(t, s)
	if err := Apply(s, res, `[{"id":"custom:x"}]`); err == nil {
		t.Fatal("Apply принял строку import_map с неверным kind")
	}
	if v, ok, _ := s.GetSetting("inbound.port"); ok {
		t.Errorf("настройка осталась после отката: %q", v)
	}
	if v, ok, _ := s.GetMeta(store.MetaCustomPresets); ok {
		t.Errorf("наборы остались после отката: %q", v)
	}
	if !reflect.DeepEqual(dumpDB(t, s), before) {
		t.Error("база изменилась после отката")
	}
}
