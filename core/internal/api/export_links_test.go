// SPDX-License-Identifier: GPL-3.0-only

package api

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/store"
)

// T-25 (B1-12): ExportLinks без ids и group_id отвечал invalid_argument, и
// кнопка «Копировать ссылку» без выделения при фильтре «все» не работала.
// Пустой запрос — все профили; group_id и ids сужают выборку.
func TestExportLinksEmptyRequestExportsAll(t *testing.T) {
	cs := newE2E(t)
	ctx := context.Background()

	in := func(gid int64, content string) []int64 {
		t.Helper()
		res, err := cs.profile.ImportContent(ctx, connect.NewRequest(&lavrayv1.ImportContentRequest{
			Content: content, TargetGroupId: int64p(gid),
		}))
		if err != nil {
			t.Fatalf("ImportContent: %v", err)
		}
		return res.Msg.AddedProfileIds
	}
	two := in(1, "trojan://p@a.example.org:443#A\ntrojan://p@b.example.org:443#B")
	in(store.DefaultGroupID, "trojan://p@c.example.org:443#C")

	count := func(req *lavrayv1.ExportLinksRequest) int {
		t.Helper()
		res, err := cs.profile.ExportLinks(ctx, connect.NewRequest(req))
		if err != nil {
			t.Fatalf("ExportLinks(%v): %v", req, err)
		}
		return len(res.Msg.Links)
	}
	if n := count(&lavrayv1.ExportLinksRequest{}); n != 3 {
		t.Errorf("пустой запрос: %d ссылок, ожидалось 3 (все профили)", n)
	}
	if n := count(&lavrayv1.ExportLinksRequest{GroupId: int64p(1)}); n != 2 {
		t.Errorf("group_id=1: %d ссылок, ожидалось 2", n)
	}
	if n := count(&lavrayv1.ExportLinksRequest{Ids: two[:1]}); n != 1 {
		t.Errorf("ids из одного профиля: %d ссылок, ожидалась 1", n)
	}

	// Неэкспортируемый тип (цепочка) пропускается, а не роняет запрос.
	chain := &model.Profile{
		GroupID: 1, Type: model.TypeChain, Name: "chain", Detail: &model.ChainDetail{ProfileIDs: two},
	}
	if err := cs.st.CreateProfile(chain); err != nil {
		t.Fatal(err)
	}
	if n := count(&lavrayv1.ExportLinksRequest{}); n != 3 {
		t.Errorf("с цепочкой: %d ссылок, ожидалось 3", n)
	}
}
