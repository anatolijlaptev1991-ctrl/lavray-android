// SPDX-License-Identifier: GPL-3.0-only

package api

// traffic_totals_filter_test.go — GetTrafficTotals учитывает profile_ids:
// фильтр отдаёт только названные профили; пустой — все, как раньше.

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

func TestGetTrafficTotalsProfileFilter(t *testing.T) {
	s, st := subAutoTestServer(t)
	if err := st.CreateGroup(&model.Group{ID: 1, Name: "Default", Kind: model.GroupBasic}); err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for i := 0; i < 3; i++ {
		p := &model.Profile{GroupID: 1, Type: model.TypeSOCKS, Name: "p", Address: "192.0.2.1", Port: 1000 + i,
			Detail: &model.SocksHTTPDetail{Proto: "socks5"}}
		if err := st.CreateProfile(p); err != nil {
			t.Fatal(err)
		}
		if err := st.SaveTraffic(p.ID, model.TrafficCounters{UplinkBytes: int64(10 * (i + 1)), DownlinkBytes: int64(i + 1)}); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, p.ID)
	}
	get := func(want []int64) map[int64]*lavrayv1.TrafficCounters {
		res, err := s.GetTrafficTotals(context.Background(), connect.NewRequest(&lavrayv1.GetTrafficTotalsRequest{ProfileIds: want}))
		if err != nil {
			t.Fatal(err)
		}
		return res.Msg.Totals
	}
	if got := get(nil); len(got) != 3 {
		t.Fatalf("пустой фильтр: %d профилей, нужно 3", len(got))
	}
	got := get([]int64{ids[1], 99999})
	if len(got) != 1 || got[ids[1]].GetUplinkBytes() != 20 || got[ids[1]].GetDownlinkBytes() != 2 {
		t.Fatalf("фильтр: %v", got)
	}
}
