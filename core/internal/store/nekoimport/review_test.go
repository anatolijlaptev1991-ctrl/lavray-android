// SPDX-License-Identifier: GPL-3.0-only

package nekoimport

// review_test.go — регрессионные тесты замечаний ревью T-78: id группы 0,
// профиль в order чужой группы, страна профиля, front_proxy_id по умолчанию,
// группа без имени, повторное использование освобождённых id, пределы чтения.

import (
	"path/filepath"
	"testing"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/store"
)

// groupByName — группа хранилища по имени.
func groupByName(t *testing.T, s *store.Store, name string) *model.Group {
	t.Helper()
	groups, err := s.ListGroups()
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range groups {
		if g.Name == name {
			return g
		}
	}
	t.Fatalf("группа %q не найдена", name)
	return nil
}

// Группа NekoBox с id 0 и front_proxy_id переносится: раньше UpdateGroup
// отвергал id 0, и перенос падал после успешного предпросмотра.
func TestFrontProxyOfGroupZero(t *testing.T) {
	dir := copyTree(t, fixtureConfig)
	write(t, filepath.Join(dir, "groups", "0.json"),
		`{"id":0,"name":"Основная","order":[1,2,3,6],"front_proxy_id":2}`)
	s := openStore(t)
	if _, err := Import(s, dir); err != nil {
		t.Fatalf("перенос группы 0 с front_proxy_id: %v", err)
	}
	g := groupByName(t, s, "Основная")
	if g.ID != 0 || g.FrontProxyID == nil || *g.FrontProxyID != profileByName(t, s, "vmess ws").ID {
		t.Errorf("группа 0: id=%d front_proxy=%v", g.ID, g.FrontProxyID)
	}
}

// Профиль, перечисленный в order группы A, но принадлежащий группе B (gid),
// попадает в план один раз — в B; цепочка на него указывает на оригинал.
func TestProfileInForeignOrderIsPlacedOnce(t *testing.T) {
	dir := mkTempDir(t)
	write(t, filepath.Join(dir, "groups", "pm.json"), `{"groups":[0,1]}`)
	write(t, filepath.Join(dir, "groups", "0.json"), `{"id":0,"name":"A","order":[1,2,4],"front_proxy_id":-1}`)
	write(t, filepath.Join(dir, "groups", "1.json"), `{"id":1,"name":"B","order":[2,3],"front_proxy_id":-1}`)
	for _, p := range []struct {
		id, gid int
		name    string
	}{{1, 0, "p1"}, {2, 1, "p2"}, {3, 1, "p3"}} {
		write(t, filepath.Join(dir, "profiles", itoa(p.id)+".json"),
			`{"type":"socks","id":`+itoa(p.id)+`,"gid":`+itoa(p.gid)+`,"bean":{"name":"`+p.name+`","addr":"1.1.1.`+itoa(p.id)+`","port":1,"v":5}}`)
	}
	write(t, filepath.Join(dir, "profiles", "4.json"), `{"type":"chain","id":4,"gid":0,"bean":{"name":"chain","list":[2]}}`)

	plan, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[int64]int{}
	for _, p := range plan.Profiles {
		seen[p.NekoID]++
		if p.NekoID == 2 && p.NekoGroupID != 1 {
			t.Errorf("профиль 2 в группе %d, ожидалась 1", p.NekoGroupID)
		}
	}
	for id := int64(1); id <= 4; id++ {
		if seen[id] != 1 {
			t.Errorf("профиль %d встречается %d раз, ожидался один", id, seen[id])
		}
	}
	s := openStore(t)
	if _, err := Import(s, dir); err != nil {
		t.Fatal(err)
	}
	if n := len(allProfiles(t, s)); n != 4 {
		t.Errorf("профилей в базе %d, ожидалось 4", n)
	}
	cd := profileByName(t, s, "chain").Detail.(*model.ChainDetail)
	if len(cd.ProfileIDs) != 1 || cd.ProfileIDs[0] != profileByName(t, s, "p2").ID {
		t.Errorf("цепочка %v указывает не на оригинал p2", cd.ProfileIDs)
	}
}

func itoa(n int) string { return string(rune('0' + n)) }

// Страна профиля определяется при переносе (A17), группировка «по странам» работает.
func TestImportedProfileGeo(t *testing.T) {
	dir := mkTempDir(t)
	write(t, filepath.Join(dir, "groups", "0.json"), `{"id":0,"name":"G","front_proxy_id":-1}`)
	write(t, filepath.Join(dir, "profiles", "1.json"),
		`{"type":"socks","id":1,"gid":0,"bean":{"name":"[NL] fast","addr":"x.com","port":1,"v":5}}`)
	s := openStore(t)
	if _, err := Import(s, dir); err != nil {
		t.Fatal(err)
	}
	if g := profileByName(t, s, "[NL] fast").Geo; g != "nl" {
		t.Errorf("страна профиля %q, ожидалась nl", g)
	}
}

// Группа без front_proxy_id не идёт через профиль 0: ключ по умолчанию — -1.
func TestMissingFrontProxyMeansNone(t *testing.T) {
	dir := mkTempDir(t)
	write(t, filepath.Join(dir, "groups", "0.json"), `{"id":0,"name":"G"}`)
	write(t, filepath.Join(dir, "profiles", "0.json"),
		`{"type":"socks","id":0,"gid":0,"bean":{"name":"zero","addr":"1.1.1.1","port":1,"v":5}}`)
	plan, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Groups) != 1 || plan.Groups[0].FrontProxy != -1 {
		t.Fatalf("front_proxy группы без ключа = %+v, ожидалось -1", plan.Groups)
	}
	s := openStore(t)
	if _, err := Import(s, dir); err != nil {
		t.Fatal(err)
	}
	if g := groupByName(t, s, "G"); g.FrontProxyID != nil {
		t.Errorf("группа получила front_proxy_id %d", *g.FrontProxyID)
	}
}

// Группа без имени получает «Group <id>» и не роняет перенос.
func TestGroupWithoutNameGetsDefault(t *testing.T) {
	dir := mkTempDir(t)
	write(t, filepath.Join(dir, "groups", "7.json"), `{"id":7,"front_proxy_id":-1}`)
	s := openStore(t)
	if _, err := Import(s, dir); err != nil {
		t.Fatalf("перенос группы без имени: %v", err)
	}
	groupByName(t, s, "Group 7")
}

// Освобождённый локальный id не возвращается в оборот: профиль NekoBox с таким
// номером получает новый, иначе ссылки на удалённый профиль указали бы на него.
func TestDeletedLocalIDIsNotReused(t *testing.T) {
	s := openStore(t)
	for _, n := range []string{"a", "b", "c"} {
		seedProfile(t, s, n)
	}
	if err := s.DeleteProfile(3); err != nil {
		t.Fatal(err)
	}
	if _, err := Import(s, fixtureConfig); err != nil {
		t.Fatal(err)
	}
	if p, err := s.GetProfile(3); err == nil {
		t.Errorf("id 3 занят импортированным профилем %q: ссылки на удалённый «c» попадут на него", p.Name)
	}
	if n := len(allProfiles(t, s)); n != 3-1+7 {
		t.Errorf("профилей %d, ожидалось %d", n, 3-1+7)
	}
}

// Свежая база (нет строк, отметка 0) сохраняет id NekoBox как есть.
func TestFreshTableKeepsNekoBoxIDs(t *testing.T) {
	s := openStore(t)
	if _, err := Import(s, fixtureConfig); err != nil {
		t.Fatal(err)
	}
	if p, err := s.GetProfile(1); err != nil || p.Name != "vless reality" {
		t.Errorf("в свежей базе профиль 1 = %+v %v", p, err)
	}
}

// check_include_pre не пишет ключи update.*: канал обновлений выбирает сам
// LAVRay (состояние обновлений переживает перенос и восстановление).
func TestNoUpdateKeysFromNekoBox(t *testing.T) {
	for _, r := range settingRules {
		if len(r.lavray) >= 7 && r.lavray[:7] == "update." {
			t.Errorf("правило %s → %s пишет ключ update.*", r.neko, r.lavray)
		}
	}
	list, _, warns := settingsOf(t, `{"check_include_pre":true}`)
	if len(list) != 0 {
		t.Errorf("check_include_pre перенесён: %v", list)
	}
	if !hasWarning(warns, "check_include_pre не переносится") {
		t.Errorf("нет предупреждения: %v", warns)
	}
}

// Каталог читается порциями и останавливается на пределе, а не загружает
// миллионы записей целиком.
func TestNamesStopAtTheCap(t *testing.T) {
	dir := mkTempDir(t)
	for i := 0; i < 50; i++ {
		write(t, filepath.Join(dir, "profiles", "f"+itoa(i%10)+itoa(i/10)+".json"), `{}`)
	}
	saved := readChunk
	readChunk = 4
	t.Cleanup(func() { readChunk = saved })
	rd := newReader(dir, Limits{MaxFiles: 5, MaxFileBytes: 1 << 20, MaxTotalBytes: 1 << 20})
	if _, err := rd.names("profiles"); err == nil {
		t.Fatal("предел числа файлов не сработал")
	}
	if rd.files > 5+readChunk {
		t.Errorf("прочитано %d записей при пределе 5 и порции %d: каталог прочитан целиком", rd.files, readChunk)
	}
}
