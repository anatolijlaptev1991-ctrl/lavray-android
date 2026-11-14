// SPDX-License-Identifier: GPL-3.0-only

package nekoimport

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// scan_read.go — чтение групп и профилей каталога: файл → структура NekoBox →
// модель. Негодный файл попадает в отчёт (Skipped) и не прерывает остальные;
// фатально только превышение пределов (ErrLimits).

// idFile — файл <id>.json каталога.
type idFile struct {
	id   int64
	name string
}

// intJSONFiles — файлы <id>.json по возрастанию id (аналог filterIntJsonFile,
// Database.cpp:17): pm.json, nekobox.json и нечисловые имена игнорируются.
func intJSONFiles(names []string) []idFile {
	var out []idFile
	for _, name := range names {
		if !strings.HasSuffix(strings.ToLower(name), ".json") {
			continue
		}
		id, err := strconv.ParseInt(strings.TrimSuffix(name, filepath.Ext(name)), 10, 64)
		if err != nil || id < 0 {
			continue
		}
		out = append(out, idFile{id: id, name: name})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].id < out[j].id })
	return out
}

// readTabOrder: groups/pm.json → {"groups":[id…]} (Database.cpp:13).
func (sc *scanner) readTabOrder() []int64 {
	b, err := sc.rd.readFile("groups", "pm.json")
	if err != nil {
		return nil
	}
	var pm struct {
		Groups []int64 `json:"groups"`
	}
	if json.Unmarshal(b, &pm) != nil {
		return nil
	}
	return pm.Groups
}

// load читает файл и при неудаче заносит его в Skipped. fatal != nil — предел.
func (sc *scanner) load(sub string, f idFile) (data []byte, rel string, fatal error) {
	rel = filepath.Join(sub, f.name)
	b, err := sc.rd.readFile(sub, f.name)
	switch {
	case errors.Is(err, ErrLimits):
		return nil, rel, err
	case err != nil:
		sc.plan.Report.skip(rel, f.id, "", err.Error())
		return nil, rel, nil
	}
	return b, rel, nil
}

// readGroups читает groups/<id>.json нового формата.
func (sc *scanner) readGroups() (map[int64]*nekoGroup, error) {
	names, err := sc.rd.names("groups")
	if err != nil {
		return nil, err
	}
	raw := map[int64]*nekoGroup{}
	for _, f := range intJSONFiles(names) {
		b, rel, fatal := sc.load("groups", f)
		if fatal != nil {
			return nil, fatal
		}
		if b == nil {
			continue
		}
		ng, err := decodeNekoGroup(b)
		if err != nil {
			sc.plan.Report.skip(rel, f.id, "", err.Error())
			continue
		}
		if ng.ID != f.id { // LoadManager: corrupted group → пропуск
			sc.plan.Report.skip(rel, f.id, "", fmt.Sprintf("id mismatch: file %d, content %d", f.id, ng.ID))
			continue
		}
		raw[f.id] = ng
	}
	return raw, nil
}

// readProfiles читает profiles/<id>.json; type=group — группа старого формата,
// она добавляется в raw. Возвращает профили по id.
func (sc *scanner) readProfiles(raw map[int64]*nekoGroup) (map[int64]*PlanProfile, error) {
	names, err := sc.rd.names("profiles")
	if err != nil {
		return nil, err
	}
	rep := &sc.plan.Report
	items := map[int64]*PlanProfile{}
	for _, f := range intJSONFiles(names) {
		b, rel, fatal := sc.load("profiles", f)
		if fatal != nil {
			return nil, fatal
		}
		if b == nil {
			continue
		}
		ent, err := decodeNekoEntity(b)
		if err != nil {
			rep.skip(rel, f.id, "", err.Error())
			continue
		}
		if ent.ID != f.id {
			rep.skip(rel, f.id, ent.Type, fmt.Sprintf("id mismatch: file %d, content %d", f.id, ent.ID))
			continue
		}
		if ent.Type == "group" { // старый формат: группа как профиль
			sc.addOldFormatGroup(raw, ent)
			continue
		}
		p, err := mapProfile(ent)
		if err != nil {
			rep.skip(rel, f.id, ent.Type, err.Error())
			continue
		}
		items[f.id] = &PlanProfile{NekoID: f.id, NekoGroupID: ent.Gid, Key: SourceKey(f.id, ent.Type, p.Address), Profile: p}
	}
	return items, nil
}

func (sc *scanner) addOldFormatGroup(raw map[int64]*nekoGroup, ent *nekoEntity) {
	g := oldFmtGroup(ent)
	if _, exists := raw[g.ID]; exists {
		sc.plan.Report.warn("old-format group %d ignored: groups/%d.json already imported", g.ID, g.ID)
		return
	}
	raw[g.ID] = g
	sc.plan.Report.OldFmtGroups++
}

// groupsOf — модельные группы по id вместе с front_proxy_id (-1 = нет).
func groupsOf(raw map[int64]*nekoGroup) map[int64]*PlanGroup {
	out := make(map[int64]*PlanGroup, len(raw))
	for gid, ng := range raw {
		g, fp := toGroup(ng)
		out[gid] = &PlanGroup{NekoID: gid, Key: SourceKey(gid, "group", ng.URL), Group: g, FrontProxy: fp}
	}
	return out
}
