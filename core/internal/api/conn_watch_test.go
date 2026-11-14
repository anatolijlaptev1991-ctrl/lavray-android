// SPDX-License-Identifier: GPL-3.0-only

package api

// conn_watch_test.go — closed_ids кадров WatchConnections без сети (T-79,
// ревью): закрытие объявляется ровно один раз и только для соединения,
// которое клиент видел в прошлом кадре.

import (
	"slices"
	"testing"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/traffic"
)

func newTestWatch(t *testing.T, limit int32) *connWatch {
	t.Helper()
	w, _, err := newConnWatch(&lavrayv1.WatchConnectionsRequest{ActivePageLimit: int32p(limit)})
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func openFake(src *traffic.FakeSource, id string, startMs int64) {
	src.Open(traffic.ConnInfo{ID: id, Outbound: "proxy", Destination: "x:1", StartMs: startMs})
}

func TestWatchFrameClosedExactlyOnceWithLateEvent(t *testing.T) {
	src := traffic.NewFakeSource()
	events, cancel := src.Subscribe() // событие о закрытии висит непрочитанным
	defer cancel()
	openFake(src, idA, 1)
	openFake(src, idB, 2)
	w := newTestWatch(t, 0)
	w.frame(src)
	src.Finish(idA, 10)
	if got := w.frame(src).GetClosedIds(); !slices.Equal(got, []string{idA}) {
		t.Fatalf("кадр 2: closed_ids = %v", got)
	}
	for i := 3; i < 6; i++ {
		if got := w.frame(src).GetClosedIds(); len(got) != 0 {
			t.Fatalf("кадр %d: закрытие повторено: %v", i, got)
		}
	}
	if len(events) == 0 {
		t.Fatal("тест не воспроизвёл позднее событие")
	}
}

func TestWatchFrameSkipsConnectionsClientNeverSaw(t *testing.T) {
	src := traffic.NewFakeSource()
	w := newTestWatch(t, 0)
	w.frame(src)
	openFake(src, idC, 3) // открылось и закрылось между кадрами
	src.Finish(idC, 4)
	if got := w.frame(src).GetClosedIds(); len(got) != 0 {
		t.Fatalf("клиент не видел %s, а closed_ids = %v", idC, got)
	}
}

func TestWatchFrameRestartClosesOldInstance(t *testing.T) {
	old := traffic.NewFakeSource()
	openFake(old, idA, 1)
	openFake(old, idB, 2)
	w := newTestWatch(t, 0)
	w.frame(old)
	fresh := traffic.NewFakeSource()
	openFake(fresh, idC, 5)
	snap := w.frame(fresh)
	if got := snap.GetClosedIds(); !slices.Equal(got, []string{idA, idB}) {
		t.Fatalf("после перезапуска closed_ids = %v", got)
	}
	if len(snap.GetActive()) != 1 || snap.GetActive()[0].GetId() != idC {
		t.Fatalf("активные нового инстанса: %v", snap.GetActive())
	}
	if got := w.frame(nil).GetClosedIds(); !slices.Equal(got, []string{idC}) {
		t.Fatalf("инстанс остановлен: closed_ids = %v", got)
	}
}

func TestWatchFrameTruncatedKeepsTrackingHidden(t *testing.T) {
	src := traffic.NewFakeSource()
	openFake(src, idA, 1)
	openFake(src, idB, 2)
	openFake(src, idC, 3)
	w := newTestWatch(t, 2)
	snap := w.frame(src)
	if !snap.GetTruncated() || len(snap.GetActive()) != 2 {
		t.Fatalf("усечение: truncated=%v active=%d", snap.GetTruncated(), len(snap.GetActive()))
	}
	src.Finish(idC, 9)
	snap = w.frame(src)
	if snap.GetTruncated() || !slices.Equal(snap.GetClosedIds(), []string{idC}) {
		t.Fatalf("после закрытия: truncated=%v closed=%v", snap.GetTruncated(), snap.GetClosedIds())
	}
}
