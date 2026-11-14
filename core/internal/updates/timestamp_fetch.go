// SPDX-License-Identifier: GPL-3.0-only

// timestamp_fetch.go — параллельная загрузка timestamp.json минимум из двух
// источников: побеждает валидный документ с НАИБОЛЬШЕЙ версией (свежесть
// важнее порядка источников), версия ниже сохранённой отвергается (откат
// метаданных). Один отставший или медленный источник не блокирует проверку.
package updates

import (
	"context"
	"fmt"
	"time"
)

// Умолчания фазы timestamp.
const (
	DefaultPhaseTimeout = 15 * time.Second
	DefaultGrace        = 3 * time.Second
)

// TimestampPolicy — параметры выбора timestamp.
type TimestampPolicy struct {
	Saved   int64         // сохранённая версия: меньшая отвергается
	Now     time.Time     // для expires и окна свежести
	Timeout time.Duration // предел всей фазы (0 → DefaultPhaseTimeout)
	Grace   time.Duration // ожидание остальных после первого валидного (0 → DefaultGrace)
}

// TimestampResult — принятый timestamp и источник, который его отдал.
type TimestampResult struct {
	Timestamp *Timestamp
	Source    Source
}

type tsOutcome struct {
	res *TimestampResult
	err error
	net bool // ошибка сети/источника, а не валидации
}

// FetchTimestamp опрашивает все источники параллельно и выбирает наибольшую
// валидную версию. Все источники недоступны — ErrNetwork («проверка не
// удалась»); хоть один отдал документ, но ни один не валиден, — ошибка
// валидации первого такого документа (подпись, срок, откат).
func FetchTimestamp(ctx context.Context, trust *Trust, srcs []Source, p TimestampPolicy) (*TimestampResult, error) {
	if len(srcs) == 0 {
		return nil, fmt.Errorf("%w: нет источников метаданных", ErrNetwork)
	}
	timeout, grace := p.Timeout, p.Grace
	if timeout <= 0 {
		timeout = DefaultPhaseTimeout
	}
	if grace <= 0 {
		grace = DefaultGrace
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ch := make(chan tsOutcome, len(srcs))
	for _, s := range srcs {
		go func(s Source) { ch <- fetchOneTimestamp(ctx, trust, s, p) }(s)
	}
	return collectTimestamps(ctx, ch, len(srcs), grace)
}

func collectTimestamps(ctx context.Context, ch <-chan tsOutcome, n int, grace time.Duration) (*TimestampResult, error) {
	var best *TimestampResult
	var valErr, netErr error
	var graceC <-chan time.Time
loop:
	for got := 0; got < n; got++ {
		select {
		case o := <-ch:
			switch {
			case o.res != nil:
				if best == nil {
					graceC = time.After(grace)
				}
				if best == nil || o.res.Timestamp.Version > best.Timestamp.Version {
					best = o.res
				}
			case o.net:
				netErr = firstErr(netErr, o.err)
			default:
				valErr = firstErr(valErr, o.err)
			}
		case <-graceC:
			break loop
		case <-ctx.Done():
			netErr = firstErr(netErr, ctx.Err())
			break loop
		}
	}
	switch {
	case best != nil:
		return best, nil
	case valErr != nil:
		return nil, valErr
	}
	return nil, fmt.Errorf("%w: %v", ErrNetwork, netErr)
}

func fetchOneTimestamp(ctx context.Context, trust *Trust, s Source, p TimestampPolicy) tsOutcome {
	raw, err := s.Fetch(ctx, "timestamp.json")
	if err != nil {
		return tsOutcome{err: err, net: true}
	}
	ts, err := validateTimestamp(trust, raw, p)
	if err != nil {
		return tsOutcome{err: err}
	}
	return tsOutcome{res: &TimestampResult{Timestamp: ts, Source: s}}
}

func validateTimestamp(trust *Trust, raw []byte, p TimestampPolicy) (*Timestamp, error) {
	env, err := trust.Verify(raw, RoleTimestamp)
	if err != nil {
		return nil, err
	}
	ts, err := ParseTimestamp(env.Signed)
	if err != nil {
		return nil, err
	}
	if expires(ts.Expires, p.Now) {
		return nil, fmt.Errorf("%w: timestamp истёк", ErrExpired)
	}
	// окно свежести ≤ 7 суток (§3.2): иначе подписанный timestamp с далёким
	// expires открывает freeze-атаку
	if ts.Expires.After(p.Now.Add(maxTimestampHorizon)) {
		return nil, fmt.Errorf("%w: timestamp.expires=%s", ErrTimestampHorizon, ts.Expires.UTC().Format(time.RFC3339))
	}
	if ts.Version < p.Saved {
		return nil, fmt.Errorf("%w: timestamp.version=%d < сохранённой %d", ErrSerialRollback, ts.Version, p.Saved)
	}
	return ts, nil
}

func firstErr(cur, next error) error {
	if cur != nil {
		return cur
	}
	return next
}

// maxTimestampHorizon — максимальное окно свежести timestamp (§3.2: 7 суток) плюс
// допуск на расхождение часов клиента в сутки: подписанный «сейчас + 7 суток»
// документ не должен отвергаться клиентом, у которого часы отстают. Дальше —
// ErrTimestampHorizon (api сообщает причину clock_skew: либо часы, либо метаданные).
const maxTimestampHorizon = 7*24*time.Hour + 24*time.Hour

func expires(t time.Time, now time.Time) bool {
	return !now.Before(t) // zero time = истёкший (строгость)
}
