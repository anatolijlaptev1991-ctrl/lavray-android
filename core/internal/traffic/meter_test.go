// SPDX-License-Identifier: GPL-3.0-only

package traffic

import (
	"encoding/binary"
	"fmt"
	"math"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)

func conn(id, tag string, up, down int64) ConnInfo {
	return ConnInfo{ID: id, Outbound: tag, Up: up, Down: down}
}

func closedConn(id, tag string, up, down int64) ConnInfo {
	c := conn(id, tag, up, down)
	c.EndMs = 1
	return c
}

// byTag — результат Tick по тегам.
func byTag(rates []TagRate) map[string]TagRate {
	out := map[string]TagRate{}
	for _, r := range rates {
		out[r.Tag] = r
	}
	return out
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

// Первый такт не даёт всплеска: у соединения уже 50 МБ, но базы для разности
// нет, скорость 0, а объём виден.
func TestMeterFirstTickNoSpike(t *testing.T) {
	m := NewMeter()
	got := byTag(m.Tick(t0, []ConnInfo{conn("a", "proxy", 50<<20, 80<<20)}, nil))
	r := got["proxy"]
	if r.UpBps != 0 || r.DownBps != 0 {
		t.Fatalf("первый такт: скорость ↑%v ↓%v, ожидался 0", r.UpBps, r.DownBps)
	}
	if r.UpTotal != 50<<20 || r.DownTotal != 80<<20 {
		t.Fatalf("первый такт: объём ↑%d ↓%d", r.UpTotal, r.DownTotal)
	}
	if len(NewMeter().Tick(t0, nil, nil)) != 0 {
		t.Fatal("пустой источник даёт теги")
	}
}

func TestMeterRateFromDeltas(t *testing.T) {
	m := NewMeter()
	m.Tick(t0, []ConnInfo{conn("a", "proxy", 1000, 2000), conn("d", "direct", 0, 0)}, nil)
	got := byTag(m.Tick(t0.Add(2*time.Second), []ConnInfo{
		conn("a", "proxy", 3000, 6000),
		conn("b", "proxy", 500, 100), // появилось за такт: все байты — прирост
		conn("d", "direct", 0, 400),
	}, nil))
	if p := got["proxy"]; !near(p.UpBps, 1250) || !near(p.DownBps, 2050) {
		t.Fatalf("proxy: ↑%v ↓%v, ожидалось 1250/2050 Б/с", p.UpBps, p.DownBps)
	}
	if p := got["proxy"]; p.UpTotal != 3500 || p.DownTotal != 6100 {
		t.Fatalf("proxy: объём ↑%d ↓%d", p.UpTotal, p.DownTotal)
	}
	if d := got["direct"]; !near(d.DownBps, 200) || d.UpBps != 0 {
		t.Fatalf("direct: %+v", d)
	}
	// Покой: счётчики те же — скорость 0, объём прежний.
	idle := byTag(m.Tick(t0.Add(3*time.Second), []ConnInfo{conn("a", "proxy", 3000, 6000), conn("b", "proxy", 500, 100), conn("d", "direct", 0, 400)}, nil))
	if idle["proxy"].DownBps != 0 || idle["proxy"].DownTotal != 6100 {
		t.Fatalf("покой: %+v", idle["proxy"])
	}
}

// Соединение закрылось между тактами: прирост считается один раз, на такте, где
// оно впервые видно как закрытое.
func TestMeterCountsClosedBetweenTicksOnce(t *testing.T) {
	m := NewMeter()
	m.Tick(t0, []ConnInfo{conn("a", "proxy", 100, 100)}, nil)
	gone := closedConn("a", "proxy", 600, 100)
	got := byTag(m.Tick(t0.Add(time.Second), nil, []ConnInfo{gone}))
	if !near(got["proxy"].UpBps, 500) || got["proxy"].UpTotal != 600 {
		t.Fatalf("закрытое между тактами: %+v", got["proxy"])
	}
	for i := 2; i <= 4; i++ { // оно остаётся в списке закрытых — больше не считается
		got = byTag(m.Tick(t0.Add(time.Duration(i)*time.Second), nil, []ConnInfo{gone}))
		if got["proxy"].UpBps != 0 || got["proxy"].UpTotal != 600 {
			t.Fatalf("такт %d: повторный учёт закрытого: %+v", i, got["proxy"])
		}
	}
	// Открылось и закрылось целиком между тактами — тоже один раз.
	quick := closedConn("q", "direct", 40, 60)
	got = byTag(m.Tick(t0.Add(5*time.Second), nil, []ConnInfo{gone, quick}))
	if !near(got["direct"].DownBps, 60) {
		t.Fatalf("быстрое соединение: %+v", got["direct"])
	}
	got = byTag(m.Tick(t0.Add(6*time.Second), nil, []ConnInfo{gone, quick}))
	if got["direct"].DownBps != 0 || got["direct"].DownTotal != 60 {
		t.Fatalf("быстрое соединение посчитано повторно: %+v", got["direct"])
	}
}

// Счётчик ушёл назад (сброс): скорость не отрицательная, объём не убывает.
func TestMeterCounterResetNoNegative(t *testing.T) {
	m := NewMeter()
	m.Tick(t0, []ConnInfo{conn("a", "proxy", 10_000, 10_000)}, nil)
	got := byTag(m.Tick(t0.Add(time.Second), []ConnInfo{conn("a", "proxy", 100, 50)}, nil))
	r := got["proxy"]
	if r.UpBps < 0 || r.DownBps < 0 || r.UpTotal != 10_000 || r.DownTotal != 10_000 {
		t.Fatalf("после сброса: %+v", r)
	}
	// База сдвинулась на новое значение: дальше обычный прирост.
	got = byTag(m.Tick(t0.Add(2*time.Second), []ConnInfo{conn("a", "proxy", 400, 50)}, nil))
	if !near(got["proxy"].UpBps, 300) || got["proxy"].UpTotal != 10_300 {
		t.Fatalf("после сброса прирост не считается: %+v", got["proxy"])
	}
	// Отрицательный счётчик у нового соединения — мусор источника, не минус в итоге.
	neg := byTag(m.Tick(t0.Add(3*time.Second), []ConnInfo{conn("n", "proxy", -5, -5)}, nil))
	if neg["proxy"].UpBps < 0 || neg["proxy"].UpTotal < 0 {
		t.Fatalf("отрицательный счётчик прошёл в итог: %+v", neg["proxy"])
	}
}

// Такт пришёл с запозданием: скорость делится на фактическое время.
func TestMeterJitterNormalized(t *testing.T) {
	m := NewMeter()
	m.Tick(t0, []ConnInfo{conn("a", "proxy", 0, 0)}, nil)
	got := byTag(m.Tick(t0.Add(1500*time.Millisecond), []ConnInfo{conn("a", "proxy", 1500, 0)}, nil))
	if !near(got["proxy"].UpBps, 1000) {
		t.Fatalf("1500 Б за 1.5 с = %v Б/с, ожидалось 1000", got["proxy"].UpBps)
	}
	// Время не прошло или пошло назад: скорости нет, но и всплеска нет.
	same := byTag(m.Tick(t0.Add(1500*time.Millisecond), []ConnInfo{conn("a", "proxy", 9000, 0)}, nil))
	back := byTag(m.Tick(t0, []ConnInfo{conn("a", "proxy", 9500, 0)}, nil))
	for name, r := range map[string]TagRate{"то же время": same["proxy"], "время назад": back["proxy"]} {
		if r.UpBps != 0 || math.IsNaN(r.UpBps) || math.IsInf(r.UpBps, 0) {
			t.Errorf("%s: скорость %v", name, r.UpBps)
		}
	}
}

// Пропавшие и из active, и из closed соединения забываются.
func TestMeterPrunesForgotten(t *testing.T) {
	m := NewMeter()
	var many []ConnInfo
	for i := 0; i < 1000; i++ {
		many = append(many, conn(fmt.Sprint("c", i), "proxy", 10, 10))
	}
	m.Tick(t0, many, nil)
	if len(m.seen) != 1000 {
		t.Fatalf("запомнено %d, ожидалось 1000", len(m.seen))
	}
	m.Tick(t0.Add(time.Second), many[:3], nil)
	if len(m.seen) != 3 {
		t.Fatalf("забытые не удалены: %d", len(m.seen))
	}
	m.Tick(t0.Add(2*time.Second), nil, nil)
	if len(m.seen) != 0 {
		t.Fatalf("после пустого такта осталось %d", len(m.seen))
	}
	// Объём тега с начала работы счётчика сохраняется.
	if got := byTag(m.Tick(t0.Add(3*time.Second), nil, nil)); got["proxy"].UpTotal != 10_000 {
		t.Fatalf("объём тега потерян: %+v", got["proxy"])
	}
}

func TestMeterTagsSortedAndOtherTag(t *testing.T) {
	m := NewMeter()
	got := m.Tick(t0, []ConnInfo{conn("1", "proxy", 1, 1), conn("2", "", 2, 2), conn("3", "direct", 3, 3)}, nil)
	var tags []string
	for _, r := range got {
		tags = append(tags, r.Tag)
	}
	if fmt.Sprint(tags) != "[direct other proxy]" {
		t.Fatalf("теги: %v", tags)
	}
}

// Короткое соединение закрылось и пропало из списков между тактами (sing-box
// очищает список закрытых при сборке мусора): прирост итогов источника попадает
// в тег "other", общая скорость остаётся верной.
func TestMeterResidualFromTotals(t *testing.T) {
	src := NewFakeSource()
	src.Open(ConnInfo{ID: "a", Outbound: "proxy"})
	src.AddBytes("a", 100, 200)
	m := NewMeter()
	m.TickSource(t0, src)

	// Соединение "b" открылось, передало данные и исчезло без следа в списках.
	src.Open(ConnInfo{ID: "b", Outbound: "proxy"})
	src.AddBytes("b", 3000, 6000)
	src.Finish("b", 0)
	src.ClearClosed()         // список закрытых очищен, байты остались в итогах
	src.AddBytes("a", 50, 70) // а "a" продолжает работать и видна
	rates := byTag(m.TickSource(t0.Add(time.Second), src))
	if r := rates["proxy"]; !near(r.UpBps, 50) || !near(r.DownBps, 70) {
		t.Fatalf("видимое соединение: %+v", r)
	}
	if r := rates["other"]; !near(r.UpBps, 3000) || !near(r.DownBps, 6000) || r.UpTotal != 3000 {
		t.Fatalf("невидимые байты должны попасть в other: %+v", r)
	}
	var up, down float64
	for _, r := range rates {
		up += r.UpBps
		down += r.DownBps
	}
	if !near(up, 3050) || !near(down, 6070) {
		t.Fatalf("общая скорость ↑%v ↓%v, ожидалось 3050/6070", up, down)
	}

	// Всё видно: поправки нет и тега other не появляется.
	clean := NewMeter()
	s2 := NewFakeSource()
	s2.Open(ConnInfo{ID: "x", Outbound: "direct"})
	clean.TickSource(t0, s2)
	s2.AddBytes("x", 10, 20)
	if _, has := byTag(clean.TickSource(t0.Add(time.Second), s2))["other"]; has {
		t.Fatal("тег other появился без причины")
	}
	// Итоги ушли назад (источник заменили): отрицательной поправки нет.
	back := NewMeter()
	back.TickSource(t0, s2)
	empty := NewFakeSource()
	for _, r := range back.TickSource(t0.Add(time.Second), empty) {
		if r.UpBps < 0 || r.DownBps < 0 || r.UpTotal < 0 {
			t.Fatalf("отрицательное значение: %+v", r)
		}
	}
}

// FuzzMeterNeverNegative: какие бы счётчики ни прислал источник, скорости и
// объёмы не отрицательны и не NaN.
func FuzzMeterNeverNegative(f *testing.F) {
	f.Add([]byte{1, 0, 0, 0, 0, 0, 0, 0, 10, 0, 0, 0, 0, 0, 0, 0, 20})
	f.Add([]byte{3, 255, 255, 255, 255, 255, 255, 255, 255, 1, 0, 0, 0, 0, 0, 0, 0})
	f.Add([]byte{2, 127, 255, 255, 255, 255, 255, 255, 255, 127, 255, 255, 255, 255, 255, 255, 255})
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		m := NewMeter()
		now := t0
		for len(data) >= 17 {
			id, up, down := data[0], binary.BigEndian.Uint64(data[1:9]), binary.BigEndian.Uint64(data[9:17])
			data = data[17:]
			c := conn(fmt.Sprint(id%5), []string{"proxy", "direct", ""}[id%3], int64(up), int64(down))
			var active, closed []ConnInfo
			if id%2 == 0 {
				active = []ConnInfo{c}
			} else {
				closed = []ConnInfo{c}
			}
			now = now.Add(time.Duration(int64(up%3000)-1000) * time.Millisecond) // и назад тоже
			for _, r := range m.Tick(now, active, closed) {
				if r.UpBps < 0 || r.DownBps < 0 || math.IsNaN(r.UpBps) || math.IsNaN(r.DownBps) ||
					math.IsInf(r.UpBps, 0) || math.IsInf(r.DownBps, 0) || r.UpTotal < 0 || r.DownTotal < 0 {
					t.Fatalf("недопустимое значение: %+v", r)
				}
			}
		}
	})
}

func BenchmarkMeterTick10k(b *testing.B) {
	const n = 10_000
	conns := make([]ConnInfo, n)
	for i := range conns {
		conns[i] = conn(fmt.Sprint("c", i), []string{"proxy", "direct"}[i%2], 0, 0)
	}
	m := NewMeter()
	m.Tick(t0, conns, nil)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 1; i <= b.N; i++ {
		for j := range conns {
			conns[j].Up += 1500
			conns[j].Down += 4500
		}
		m.Tick(t0.Add(time.Duration(i)*time.Second), conns, nil)
	}
}
