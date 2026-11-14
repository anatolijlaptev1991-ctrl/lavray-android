// SPDX-License-Identifier: GPL-3.0-only

package api

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

// D-04: ResetTraffic ставил p.Traffic = {} и звал UpdateProfile, а тот не
// пишет uplink_total/downlink_total, поэтому счётчики оставались прежними.
func TestResetTrafficZeroesCounters(t *testing.T) {
	cs := newE2E(t)
	ctx := context.Background()

	mk := func(name string, up, down int64) int64 {
		p := &model.Profile{
			GroupID: 1, Type: model.TypeSOCKS, Name: name, Address: "127.0.0.1", Port: 1080,
			Detail:  &model.SocksHTTPDetail{Proto: "socks5"},
			Traffic: model.TrafficCounters{UplinkBytes: up, DownlinkBytes: down},
		}
		if err := cs.st.CreateProfile(p); err != nil {
			t.Fatal(err)
		}
		return p.ID
	}
	a, b := mk("a", 1000, 2000), mk("b", 30, 40)
	traffic := func(id int64) model.TrafficCounters {
		t.Helper()
		p, err := cs.st.GetProfile(id)
		if err != nil {
			t.Fatal(err)
		}
		return p.Traffic
	}
	if got := traffic(a); got != (model.TrafficCounters{UplinkBytes: 1000, DownlinkBytes: 2000}) {
		t.Fatalf("исходные счётчики a: %+v", got)
	}

	// Только выбранный профиль.
	if _, err := cs.profile.ResetTraffic(ctx, connect.NewRequest(&lavrayv1.ResetTrafficRequest{Ids: []int64{a}})); err != nil {
		t.Fatal(err)
	}
	if got := traffic(a); got != (model.TrafficCounters{}) {
		t.Fatalf("a после сброса: %+v, ожидалось {0 0}", got)
	}
	if got := traffic(b); got != (model.TrafficCounters{UplinkBytes: 30, DownlinkBytes: 40}) {
		t.Fatalf("b не должен меняться при сбросе a: %+v", got)
	}

	// Пустой список = все профили; несуществующий id не ломает запрос.
	if _, err := cs.profile.ResetTraffic(ctx, connect.NewRequest(&lavrayv1.ResetTrafficRequest{})); err != nil {
		t.Fatal(err)
	}
	if got := traffic(b); got != (model.TrafficCounters{}) {
		t.Fatalf("b после сброса всех: %+v, ожидалось {0 0}", got)
	}
	if _, err := cs.profile.ResetTraffic(ctx, connect.NewRequest(&lavrayv1.ResetTrafficRequest{Ids: []int64{999999}})); err != nil {
		t.Fatalf("несуществующий id: %v", err)
	}

	// Клиент видит нули и через API.
	got, err := cs.profile.GetProfile(ctx, connect.NewRequest(&lavrayv1.GetProfileRequest{Id: b}))
	if err != nil {
		t.Fatal(err)
	}
	if tr := got.Msg.Traffic; tr != nil && (tr.UplinkBytes != 0 || tr.DownlinkBytes != 0) {
		t.Fatalf("GetProfile отдаёт ненулевой трафик после сброса: %+v", tr)
	}
}
