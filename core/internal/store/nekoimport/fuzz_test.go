// SPDX-License-Identifier: GPL-3.0-only

package nekoimport

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

// FuzzScanEntity — разбор произвольных байтов как profiles/<id>.json и
// groups/<id>.json. Инвариант: ни паники, ни профиля без типа; при успехе Remap
// на таком профиле тоже не падает. Seed — файлы фикстур.
func FuzzScanEntity(f *testing.F) {
	for _, pattern := range []string{"profiles/*.json", "groups/*.json"} {
		files, _ := filepath.Glob(filepath.Join(fixtureConfig, pattern))
		for _, file := range files {
			if b, err := os.ReadFile(file); err == nil {
				f.Add(b)
			}
		}
	}
	f.Add([]byte(`{"type":"chain","id":1,"gid":0,"bean":{"list":[1,1,2]}}`))
	f.Add([]byte(`{"type":"group","id":2,"bean":{"order":[3],"front_proxy_id":3}}`))
	f.Add([]byte(`null`))
	f.Fuzz(func(t *testing.T, data []byte) {
		if g, err := decodeNekoGroup(data); err == nil && g != nil {
			toGroup(g)
		}
		ent, err := decodeNekoEntity(data)
		if err != nil || ent == nil {
			return
		}
		if ent.Type == "group" {
			_ = oldFmtGroup(ent)
			return
		}
		p, err := mapProfile(ent)
		if err != nil {
			return
		}
		if p.Type == "" {
			t.Fatalf("профиль без типа: %q", data)
		}
		plan := &Plan{Report: newReport(),
			Groups:   []PlanGroup{{NekoID: ent.Gid, Key: "g", Group: &model.Group{ID: ent.Gid, Name: "g"}, FrontProxy: -1}},
			Profiles: []PlanProfile{{NekoID: ent.ID, NekoGroupID: ent.Gid, Key: "p", Profile: p}}}
		Remap(plan, State{GroupIDs: map[int64]bool{}, ProfileIDs: map[int64]bool{}})
	})
}
