// SPDX-License-Identifier: GPL-3.0-only

package api

// profile_list_diff_test.go — дифференциальный тест ListProfiles: новая
// реализация (фильтры и страница до декодирования) против эталона.

import (
	"context"
	"math/rand"
	"testing"

	"connectrpc.com/connect"
	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"google.golang.org/protobuf/proto"
)

var diffSearches = []string{"", "москва", "МОСКВА", "ТеСт", "ber", "🇩🇪", "trojan", "VLESS", "ß", "STRASSE",
	"İ", "i̇", "ǆ", "ǅ", "äöü", "ÄÖÜ", "node1", ".example.org", "zzz-нет", "7", " "}

func randRequest(r *rand.Rand) *lavrayv1.ListProfilesRequest {
	req := &lavrayv1.ListProfilesRequest{}
	if r.Intn(2) == 0 {
		req.GroupId = int64p(int64(r.Intn(5))) // 0 и 4 — пустые/несуществующие группы
	}
	if r.Intn(2) == 0 {
		req.Search = strp(randPick(r, diffSearches))
	}
	if r.Intn(3) == 0 {
		req.FavoritesOnly = boolp(r.Intn(2) == 0)
	}
	for n := r.Intn(3); n > 0; n-- {
		req.TagsAnyOf = append(req.TagsAnyOf, randPick(r, append([]string{"нет-такого"}, refTags...)))
	}
	if r.Intn(5) != 0 {
		req.Page = &lavrayv1.Page{
			Offset: int64(r.Intn(60)) - int64(r.Intn(3)), Limit: int32(r.Intn(40)) - int32(r.Intn(2)),
		}
		if r.Intn(20) == 0 {
			req.Page.Offset = 1 << 40
		}
	}
	return req
}

func TestListProfilesMatchesReference(t *testing.T) {
	s := seedProfiles(t, 400, 11)
	r := rand.New(rand.NewSource(5))
	nonEmpty := 0
	for i := 0; i < 1500; i++ {
		req := randRequest(r)
		want, err := refListProfiles(s, req)
		if err != nil {
			t.Fatal(err)
		}
		res, err := s.ListProfiles(context.Background(), connect.NewRequest(req))
		if err != nil {
			t.Fatalf("%v: %v", req, err)
		}
		if len(want.Profiles) > 0 {
			nonEmpty++
		}
		if !proto.Equal(want, res.Msg) {
			t.Fatalf("запрос %v: ответы различаются\nwant total=%d n=%d\ngot  total=%d n=%d",
				req, want.Total, len(want.Profiles), res.Msg.Total, len(res.Msg.Profiles))
		}
		a, _ := proto.MarshalOptions{Deterministic: true}.Marshal(want)
		b, _ := proto.MarshalOptions{Deterministic: true}.Marshal(res.Msg)
		if string(a) != string(b) {
			t.Fatalf("запрос %v: байты ответа различаются", req)
		}
	}
	if nonEmpty < 300 {
		t.Fatalf("тест вырожден: непустых ответов %d", nonEmpty)
	}
}
