// SPDX-License-Identifier: GPL-3.0-only

package store

import (
	"fmt"
	"slices"
	"testing"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

// seedPage: группа 1 — p0..p5 (p1,p3 избранные), группа 2 — q0.
func seedPage(t *testing.T) *Store {
	t.Helper()
	s := openTemp(t)
	for _, g := range []int64{1, 2} {
		if err := s.CreateGroup(&model.Group{ID: g, Name: fmt.Sprint("g", g), Kind: model.GroupBasic}); err != nil {
			t.Fatal(err)
		}
	}
	mk := func(g int64, name string, fav bool, tags ...string) {
		p := &model.Profile{GroupID: g, Type: model.TypeTrojan, Name: name, Address: name + ".example.org", Port: 443,
			Favorite: fav, Tags: tags, Detail: &model.TrojanVlessDetail{Password: "x"}}
		if err := s.CreateProfile(p); err != nil {
			t.Fatal(err)
		}
	}
	mk(1, "p0", false)
	mk(1, "p1", true, "work")
	mk(1, "Москва-2", false, "Работа")
	mk(1, "p3", true, "work", "🔥")
	mk(1, "p4", false, "🔥")
	mk(1, "p5", false)
	mk(2, "q0", false, "work")
	return s
}

func names(ps []*model.Profile) []string {
	var out []string
	for _, p := range ps {
		out = append(out, p.Name)
	}
	return out
}

func TestListProfilesPage(t *testing.T) {
	s := seedPage(t)
	cases := []struct {
		name  string
		q     ProfileQuery
		want  []string
		total int64
	}{
		{"все без предела", ProfileQuery{GroupID: -1, Limit: -1}, []string{"p0", "p1", "Москва-2", "p3", "p4", "p5", "q0"}, 7},
		{"группа", ProfileQuery{GroupID: 2, Limit: -1}, []string{"q0"}, 1},
		{"страница из середины", ProfileQuery{GroupID: 1, Offset: 2, Limit: 2}, []string{"Москва-2", "p3"}, 6},
		{"limit за концом", ProfileQuery{GroupID: 1, Offset: 4, Limit: 100}, []string{"p4", "p5"}, 6},
		{"offset ровно total", ProfileQuery{GroupID: 1, Offset: 6, Limit: 5}, nil, 6},
		{"offset больше total", ProfileQuery{GroupID: 1, Offset: 1 << 40, Limit: 5}, nil, 6},
		{"отрицательный offset как 0", ProfileQuery{GroupID: 1, Offset: -3, Limit: 1}, []string{"p0"}, 6},
		{"избранное", ProfileQuery{GroupID: -1, FavoritesOnly: true, Limit: -1}, []string{"p1", "p3"}, 2},
		{"теги", ProfileQuery{GroupID: -1, TagsAnyOf: []string{"🔥", "Работа"}, Limit: -1}, []string{"Москва-2", "p3", "p4"}, 3},
		{"теги регистрозависимы", ProfileQuery{GroupID: -1, TagsAnyOf: []string{"WORK"}, Limit: -1}, nil, 0},
		{"теги и избранное и страница", ProfileQuery{GroupID: -1, FavoritesOnly: true, TagsAnyOf: []string{"work"}, Offset: 1, Limit: 5}, []string{"p3"}, 2},
		{"поиск кириллицы без регистра", ProfileQuery{GroupID: -1, SearchLower: "москва", Limit: -1}, []string{"Москва-2"}, 1},
		{"поиск по типу", ProfileQuery{GroupID: 2, SearchLower: "troj", Limit: -1}, []string{"q0"}, 1},
		{"поиск без совпадений", ProfileQuery{GroupID: -1, SearchLower: "нет", Limit: -1}, nil, 0},
		{"поиск и limit 0", ProfileQuery{GroupID: -1, SearchLower: "p", Limit: 0}, nil, 7},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, total, err := s.ListProfilesPage(c.q)
			if err != nil {
				t.Fatal(err)
			}
			if total != c.total || !slices.Equal(names(got), c.want) {
				t.Fatalf("got %v total=%d, want %v total=%d", names(got), total, c.want, c.total)
			}
		})
	}
}

// Страница читается теми же данными, что и ListProfiles.
func TestListProfilesPageMatchesListProfiles(t *testing.T) {
	s := seedPage(t)
	all, err := s.ListProfiles(-1)
	if err != nil {
		t.Fatal(err)
	}
	got, _, err := s.ListProfilesPage(ProfileQuery{GroupID: -1, Limit: -1})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(all) {
		t.Fatalf("len %d != %d", len(got), len(all))
	}
	for i := range all {
		if all[i].ID != got[i].ID || all[i].Name != got[i].Name || all[i].Favorite != got[i].Favorite || !slices.Equal(all[i].Tags, got[i].Tags) {
			t.Fatalf("позиция %d: %+v != %+v", i, got[i], all[i])
		}
	}
}
