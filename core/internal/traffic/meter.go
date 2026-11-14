// SPDX-License-Identifier: GPL-3.0-only

package traffic

import (
	"math"
	"sort"
	"time"
)

// otherTag — тег для соединения, у которого исходящий не определён, и для
// байт, которых нет в списках соединений (TickSource).
const otherTag = "other"

// Meter считает скорости по тегам из разности счётчиков соединений между
// двумя вызовами Tick. Один Meter — один потребитель (один поток Subscribe):
// у каждого свой интервал и своя точка отсчёта. Не потокобезопасен.
type Meter struct {
	started bool
	prevAt  time.Time
	gen     uint64
	seen    map[string]seenConn
	tags    map[string]*tagAcc
	// Нарастающие итоги для TickSource: сколько байт дали списки соединений за
	// всё время и сколько уже отнесено к "other" как невидимое в списках.
	listUp, listDown int64
	resUp, resDown   int64
}

// seenConn — что известно о соединении с прошлого Tick.
type seenConn struct {
	up, down int64
	gen      uint64
}

// tagAcc — накопители одного тега: прибавка за такт и итог с начала работы.
type tagAcc struct {
	dUp, dDown         int64 // байты, перенесённые за этот такт (для скорости)
	upTotal, downTotal int64
}

// NewMeter создаёт пустой счётчик: первый Tick только запоминает состояние.
func NewMeter() *Meter {
	return &Meter{seen: map[string]seenConn{}, tags: map[string]*tagAcc{}}
}

// Tick принимает состояние источника на момент now и возвращает по каждому
// известному тегу скорость за прошедшее время и накопленный объём.
//
//   - Первый Tick скорость не считает (нет базы для разности), а объём берёт
//     как есть: соединения жили до подписки.
//   - Скорость = прибавка байт / фактическое время между Tick, а не заданный
//     интервал: тик с запозданием не завышает и не занижает её.
//   - Соединение, закрывшееся между тактами, учитывается ОДИН раз: на том Tick,
//     где оно впервые появилось закрытым; в следующих его разность нулевая.
//   - Счётчик, ушедший назад (сброс), даёт 0, а не отрицательное число.
//   - Соединения, пропавшие и из active, и из closed, забываются: карта не растёт.
func (m *Meter) Tick(now time.Time, active, closed []ConnInfo) []TagRate {
	return m.tick(now, active, closed, nil)
}

// totals — итоги источника на момент такта.
type totals struct{ up, down int64 }

// TickSource — Tick по источнику с поправкой на его итоги. Список закрытых
// соединений источника неполон: sing-box очищает его при сборке мусора и
// ограничивает по числу, поэтому короткое соединение может закрыться и пропасть
// между двумя тактами. Итоги (Total) такие соединения помнят, так что байты,
// которых нет в списках, но есть в итогах, записываются в тег "other": общая
// скорость остаётся верной, а разбивка по тегам — лучшей из возможных.
//
// Итог читается ДО списков. Байты, пришедшие между чтениями, тогда есть в
// списках, но ещё нет в итоге: они уменьшают невидимое, а не попадают в "other"
// (а на следующем такте — второй раз, под своим тегом).
func (m *Meter) TickSource(now time.Time, src Source) []TagRate {
	up, down := src.Total()
	active, closed := src.Active(), src.Closed()
	return m.tick(now, active, closed, &totals{up, down})
}

func (m *Meter) tick(now time.Time, active, closed []ConnInfo, tot *totals) []TagRate {
	first := !m.started
	dt := now.Sub(m.prevAt).Seconds()
	m.gen++
	for _, acc := range m.tags {
		acc.dUp, acc.dDown = 0, 0
	}
	m.observe(active, first)
	m.observe(closed, first)
	for id, c := range m.seen {
		if c.gen != m.gen {
			delete(m.seen, id)
		}
	}
	if tot != nil {
		m.addResidual(tot, first)
	}
	m.started, m.prevAt = true, now
	return m.rates(first, dt)
}

// addResidual относит к тегу "other" байты итогов, которых нет в списках.
// Невидимое — нарастающий итог (итог источника минус всё, что дали списки), и
// "other" получает только его рост сверх уже отнесённого: байт попадает туда
// не больше одного раза, а дрожание между чтениями итога и списков (списки
// впереди итога) лишь уменьшает невидимое. Первый такт кладёт вытесненное до
// подписки в объём, не в скорость.
func (m *Meter) addResidual(tot *totals, first bool) {
	dUp := nonNegative(nonNegative(tot.up-m.listUp) - m.resUp)
	dDown := nonNegative(nonNegative(tot.down-m.listDown) - m.resDown)
	if dUp == 0 && dDown == 0 {
		return
	}
	m.resUp, m.resDown = addSat(m.resUp, dUp), addSat(m.resDown, dDown)
	acc := m.accFor(otherTag)
	acc.upTotal, acc.downTotal = addSat(acc.upTotal, dUp), addSat(acc.downTotal, dDown)
	if !first {
		acc.dUp, acc.dDown = addSat(acc.dUp, dUp), addSat(acc.dDown, dDown)
	}
}

// observe применяет разность по каждому соединению списка.
func (m *Meter) observe(conns []ConnInfo, first bool) {
	for i := range conns {
		c := &conns[i]
		prev, known := m.seen[c.ID]
		if prev.gen == m.gen {
			continue // тот же id в обоих списках: считаем один раз
		}
		dUp, dDown := nonNegative(c.Up), nonNegative(c.Down) // новое: всё передано за такт
		if known {
			dUp, dDown = nonNegative(c.Up-prev.up), nonNegative(c.Down-prev.down)
		}
		m.listUp, m.listDown = addSat(m.listUp, dUp), addSat(m.listDown, dDown)
		acc := m.tagAcc(c)
		acc.upTotal = addSat(acc.upTotal, dUp)
		acc.downTotal = addSat(acc.downTotal, dDown)
		if !first {
			acc.dUp = addSat(acc.dUp, dUp)
			acc.dDown = addSat(acc.dDown, dDown)
		}
		m.seen[c.ID] = seenConn{up: c.Up, down: c.Down, gen: m.gen}
	}
}

func (m *Meter) tagAcc(c *ConnInfo) *tagAcc {
	if c.Outbound == "" {
		return m.accFor(otherTag)
	}
	return m.accFor(c.Outbound)
}

func (m *Meter) accFor(tag string) *tagAcc {
	acc, ok := m.tags[tag]
	if !ok {
		acc = &tagAcc{}
		m.tags[tag] = acc
	}
	return acc
}

// rates собирает итог такта, теги по алфавиту.
func (m *Meter) rates(first bool, dt float64) []TagRate {
	out := make([]TagRate, 0, len(m.tags))
	for tag, acc := range m.tags {
		r := TagRate{Tag: tag, UpTotal: acc.upTotal, DownTotal: acc.downTotal}
		if !first && dt > 0 {
			r.UpBps = float64(acc.dUp) / dt
			r.DownBps = float64(acc.dDown) / dt
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Tag < out[j].Tag })
	return out
}

func nonNegative(v int64) int64 {
	if v < 0 {
		return 0
	}
	return v
}

// addSat складывает неотрицательные числа без переполнения: на мусорных
// счётчиках источника итог упирается в потолок, а не уходит в минус.
func addSat(a, b int64) int64 {
	if a > math.MaxInt64-b {
		return math.MaxInt64
	}
	return a + b
}
