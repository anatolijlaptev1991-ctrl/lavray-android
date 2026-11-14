// SPDX-License-Identifier: GPL-3.0-only

package traffic

// meter_residual_test.go — тег "other" в TickSource (T-79, ревью): байты вне
// списков попадают туда ровно один раз, а дрожание между чтением итога и
// списков не создаёт ложного "other" и не завышает общий объём.

import (
	"testing"
	"time"
)

// skewSource — источник, у которого между чтением итога и чтением списков
// успевают пройти байты (как у настоящего менеджера под нагрузкой).
type skewSource struct {
	*FakeSource
	onActive func()
}

func (s skewSource) Active() []ConnInfo {
	if s.onActive != nil {
		s.onActive()
	}
	return s.FakeSource.Active()
}

func sumTotals(rates []TagRate) (up, down int64) {
	for _, r := range rates {
		up, down = up+r.UpTotal, down+r.DownTotal
	}
	return up, down
}

func TestMeterNoPhantomOtherFromReadSkew(t *testing.T) {
	fake := NewFakeSource()
	fake.Open(ConnInfo{ID: "a", Outbound: "proxy"})
	jitter := int64(0)
	src := skewSource{FakeSource: fake, onActive: func() {
		jitter = (jitter + 7) % 10 // 0..9 байт между чтениями
		fake.AddBytes("a", jitter, jitter)
	}}
	m := NewMeter()
	var last []TagRate
	for i := range 50 {
		fake.AddBytes("a", 1000, 2000)
		last = m.TickSource(t0.Add(time.Duration(i)*time.Second), src)
		if r, has := byTag(last)["other"]; has {
			t.Fatalf("такт %d: ложный other %+v", i, r)
		}
	}
	up, down := sumTotals(last)
	if wantUp, wantDown := fake.Total(); up != wantUp || down != wantDown {
		t.Fatalf("объём по тегам ↑%d ↓%d, у источника ↑%d ↓%d", up, down, wantUp, wantDown)
	}
}

func TestMeterResidualCountedOnce(t *testing.T) {
	fake := NewFakeSource()
	fake.Open(ConnInfo{ID: "a", Outbound: "proxy"})
	src := skewSource{FakeSource: fake, onActive: func() { fake.AddBytes("a", 3, 3) }}
	m := NewMeter()
	m.TickSource(t0, src)
	// "b" передала данные и пропала из списков до такта.
	fake.Open(ConnInfo{ID: "b", Outbound: "direct"})
	fake.AddBytes("b", 500, 900)
	fake.Finish("b", 0)
	fake.ClearClosed()
	// Пока байты идут между чтениями, невидимое занижено ровно на одно окно
	// дрожания (3 байта), но не завышено: other не больше 500/900.
	first := byTag(m.TickSource(t0.Add(time.Second), src))["other"]
	if first.UpTotal < 497 || first.UpTotal > 500 || first.DownTotal < 897 || first.DownTotal > 900 || first.UpBps <= 0 {
		t.Fatalf("невидимые байты: %+v", first)
	}
	for i := 2; i < 6; i++ {
		r := byTag(m.TickSource(t0.Add(time.Duration(i)*time.Second), src))["other"]
		if r.UpTotal != first.UpTotal || r.DownTotal != first.DownTotal || r.UpBps != 0 {
			t.Fatalf("такт %d: other посчитан повторно: %+v (было %+v)", i, r, first)
		}
	}
	// Дрожание прекратилось — следующий такт добирает остаток, и только его.
	src.onActive = nil
	r := byTag(m.TickSource(t0.Add(6*time.Second), src))["other"]
	if r.UpTotal != 500 || r.DownTotal != 900 {
		t.Fatalf("после дрожания: %+v", r)
	}
}

func TestMeterResidualBeforeSubscribeIsVolumeNotSpeed(t *testing.T) {
	fake := NewFakeSource()
	fake.Open(ConnInfo{ID: "old", Outbound: "proxy"})
	fake.AddBytes("old", 4000, 8000)
	fake.Finish("old", 0)
	fake.ClearClosed() // вытеснено до подписки
	m := NewMeter()
	r := byTag(m.TickSource(t0, fake))["other"]
	if r.UpTotal != 4000 || r.DownTotal != 8000 || r.UpBps != 0 || r.DownBps != 0 {
		t.Fatalf("первый такт: %+v", r)
	}
	if r := byTag(m.TickSource(t0.Add(time.Second), fake))["other"]; r.UpBps != 0 || r.UpTotal != 4000 {
		t.Fatalf("второй такт: %+v", r)
	}
}
