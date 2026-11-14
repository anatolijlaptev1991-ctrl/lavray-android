// SPDX-License-Identifier: GPL-3.0-only

package api

// profile_list_ref_test.go — эталон ListProfiles (реализация до R0-D: полная
// загрузка, фильтры и страница в Go), генератор данных и бенчмарки.

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"testing"

	"connectrpc.com/connect"
	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/inst"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/logs"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/store"
)

// refListProfiles — дословная копия прежнего Server.ListProfiles.
func refListProfiles(s *Server, req *lavrayv1.ListProfilesRequest) (*lavrayv1.ListProfilesResponse, error) {
	msg := req
	groupID := int64(-1)
	if msg.GroupId != nil {
		groupID = *msg.GroupId
	}
	list, err := s.st.ListProfiles(groupID)
	if err != nil {
		return nil, mapErr(err)
	}
	search := ""
	if msg.Search != nil {
		search = strings.ToLower(*msg.Search)
	}
	favOnly := optBool(msg.FavoritesOnly)
	tags := msg.TagsAnyOf
	out := make([]*lavrayv1.Profile, 0, len(list))
	total := int64(0)
	for _, p := range list {
		if search != "" &&
			!strings.Contains(strings.ToLower(p.Name), search) &&
			!strings.Contains(strings.ToLower(p.Address), search) &&
			!strings.Contains(strings.ToLower(string(p.Type)), search) {
			continue
		}
		if favOnly && !p.Favorite {
			continue
		}
		if len(tags) > 0 && !hasAnyTag(p.Tags, tags) {
			continue
		}
		total++
		out = append(out, profileToProto(p))
	}
	offset, limit := pageOf(msg.Page)
	if offset > int64(len(out)) {
		offset = int64(len(out))
	}
	end := int64(len(out))
	if limit >= 0 && offset+limit < end {
		end = offset + limit
	}
	out = out[offset:end]
	return &lavrayv1.ListProfilesResponse{Profiles: out, Total: total}, nil
}

func hasAnyTag(have, want []string) bool {
	set := map[string]bool{}
	for _, t := range have {
		set[t] = true
	}
	for _, t := range want {
		if set[t] {
			return true
		}
	}
	return false
}

var (
	refWords = []string{"Москва", "ТЕСТ", "сервер", "Berlin", "🇩🇪", "🚀 fast", "İstanbul", "Straße", "tokyo", "", "ÄÖÜ", "ǅ", "NODE"}
	refTags  = []string{"work", "Работа", "🔥", "home", "ДОМ", "", "a b"}
)

func randPick(r *rand.Rand, xs []string) string { return xs[r.Intn(len(xs))] }

func randProfile(r *rand.Rand, group int64, i int) *model.Profile {
	p := &model.Profile{GroupID: group, Port: 1 + r.Intn(65000), Favorite: r.Intn(4) == 0}
	p.Name = randPick(r, refWords) + " " + randPick(r, refWords) + fmt.Sprint(i)
	if r.Intn(10) == 0 {
		p.Name = ""
	}
	p.Address = fmt.Sprintf("%s%d.example.org", strings.ToLower(randPick(r, refWords[:5])), i)
	if r.Intn(2) == 0 {
		p.Type, p.Detail = model.TypeTrojan, &model.TrojanVlessDetail{Password: "secret"}
	} else {
		p.Type, p.Detail = model.TypeVLESS, &model.TrojanVlessDetail{Password: "u"}
	}
	for n := r.Intn(4); n > 0; n-- {
		p.Tags = append(p.Tags, randPick(r, refTags))
	}
	return p
}

// seedProfiles создаёт n профилей в нескольких группах одной транзакцией.
func seedProfiles(tb testing.TB, n int, seed int64) *Server {
	tb.Helper()
	dir, err := os.MkdirTemp("", "lavray-list-*")
	if err != nil {
		tb.Fatal(err)
	}
	st, err := store.Open(dir)
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { _ = st.Close(); _ = os.RemoveAll(dir) })
	for g := int64(1); g <= 3; g++ {
		if err := st.CreateGroup(&model.Group{ID: g, Name: fmt.Sprint("g", g), Kind: model.GroupBasic}); err != nil {
			tb.Fatal(err)
		}
	}
	r := rand.New(rand.NewSource(seed))
	err = st.InTx(func(tx *store.Store) error {
		for i := 0; i < n; i++ {
			if err := tx.CreateProfile(randProfile(r, int64(1+r.Intn(3)), i)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		tb.Fatal(err)
	}
	return NewServer(Deps{Store: st, Logs: logs.NewMux(10), Runner: &inst.FakeRunner{}})
}

func BenchmarkListProfiles(b *testing.B) {
	s := seedProfiles(b, 10000, 1)
	page := &lavrayv1.Page{Limit: 100}
	cases := map[string]*lavrayv1.ListProfilesRequest{
		"all_page":        {Page: page},
		"search_page":     {Search: strp("москва"), Page: page},
		"search_rare":     {Search: strp("ǅ node 9999"), Page: page},
		"fav_tags_page":   {FavoritesOnly: boolp(true), TagsAnyOf: []string{"🔥"}, Page: page},
		"group_page_deep": {GroupId: int64p(2), Page: &lavrayv1.Page{Offset: 3000, Limit: 100}},
	}
	for name, req := range cases {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := s.ListProfiles(context.Background(), connect.NewRequest(req)); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
