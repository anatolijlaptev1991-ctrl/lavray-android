// SPDX-License-Identifier: GPL-3.0-only

package api

// conn_watch_ref_test.go — эталон кадра WatchConnections (полная сортировка
// всех соединений), дифференциальный тест и бенчмарк.

import (
	"fmt"
	"math/rand"
	"sort"
	"testing"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/traffic"
	"google.golang.org/protobuf/proto"
)

// refFrame — прежний connWatch.frame; prev ведёт сам эталон.
func refFrame(w *connWatch, prev map[string]struct{}, src traffic.Source) (*lavrayv1.ConnectionSnapshot, map[string]struct{}) {
	var all []traffic.ConnInfo
	if src != nil {
		all = sortedActive(src, w.filter)
	}
	cur := make(map[string]struct{}, len(all))
	for _, c := range all {
		cur[c.ID] = struct{}{}
	}
	ref := &connWatch{prev: prev}
	snap := &lavrayv1.ConnectionSnapshot{ClosedIds: ref.closedIDs(cur)}
	if len(all) > w.limit {
		all, snap.Truncated = all[:w.limit], true
	}
	for _, c := range all {
		snap.Active = append(snap.Active, connToProto(c))
	}
	return snap, cur
}

func fillConns(src *traffic.FakeSource, r *rand.Rand, n, ties int, base int) {
	for i := 0; i < n; i++ {
		src.Open(traffic.ConnInfo{
			ID: fmt.Sprintf("c%06d", r.Intn(1<<30)), Outbound: []string{"proxy", "direct", "Proxy-2"}[r.Intn(3)],
			Destination: fmt.Sprintf("h%d.example:%d", i, 80+r.Intn(3)), StartMs: int64(base + r.Intn(ties)),
		})
	}
}

func TestWatchFrameMatchesFullSort(t *testing.T) {
	r := rand.New(rand.NewSource(7))
	for iter := 0; iter < 300; iter++ {
		src := traffic.NewFakeSource()
		msg := &lavrayv1.WatchConnectionsRequest{ActivePageLimit: int32p(int32(r.Intn(40)))}
		if r.Intn(3) == 0 {
			msg.OutboundFilter = strp("PROXY")
		}
		w := newTestWatchMsg(t, msg)
		prev := map[string]struct{}{}
		for frame := 0; frame < 4; frame++ {
			fillConns(src, r, r.Intn(120), 1+r.Intn(15), 1000) // мало значений StartMs: много равных ключей
			for _, c := range src.Active() {
				if r.Intn(5) == 0 {
					src.Finish(c.ID, 5000)
				}
			}
			want, nextPrev := refFrame(w, prev, src)
			prev = nextPrev
			got := w.frame(src)
			if !proto.Equal(want, got) {
				t.Fatalf("iter %d frame %d: кадры различаются\nwant %v\ngot  %v", iter, frame, want, got)
			}
		}
	}
}

func newTestWatchMsg(t *testing.T, msg *lavrayv1.WatchConnectionsRequest) *connWatch {
	t.Helper()
	w, _, err := newConnWatch(msg)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func BenchmarkWatchFrame(b *testing.B) {
	src := traffic.NewFakeSource()
	fillConns(src, rand.New(rand.NewSource(3)), 5000, 3000, 1000)
	w, _, err := newConnWatch(&lavrayv1.WatchConnectionsRequest{})
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w.frame(src)
	}
}

func TestFirstConnsMatchesSortPrefix(t *testing.T) {
	r := rand.New(rand.NewSource(9))
	for iter := 0; iter < 2000; iter++ {
		src := traffic.NewFakeSource()
		fillConns(src, r, r.Intn(80), 1+r.Intn(6), 0)
		all := src.Active()
		k := r.Intn(90)
		want := append([]traffic.ConnInfo(nil), all...)
		sort.Slice(want, func(i, j int) bool { return connBefore(&want[i], &want[j]) })
		want = want[:min(k, len(want))]
		got := firstConns(all, k)
		if len(got) != len(want) {
			t.Fatalf("k=%d n=%d: len %d != %d", k, len(all), len(got), len(want))
		}
		for i := range want {
			if got[i].ID != want[i].ID {
				t.Fatalf("k=%d n=%d: позиция %d: %s != %s", k, len(all), i, got[i].ID, want[i].ID)
			}
		}
	}
}
