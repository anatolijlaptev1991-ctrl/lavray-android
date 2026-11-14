// SPDX-License-Identifier: GPL-3.0-only

// query.go — параллельный опрос источников одним именем файла. Не ждёт
// зависший источник: после первого ОТВЕТА (тело или «нет такого файла») остальным
// даётся короткая отсрочка Grace, всей фазе — предел Timeout.
package updates

import (
	"context"
	"errors"
	"time"
)

// QueryPolicy — пределы параллельного опроса (0 → значения по умолчанию).
type QueryPolicy struct {
	Timeout time.Duration
	Grace   time.Duration
}

func (p QueryPolicy) withDefaults() QueryPolicy {
	if p.Timeout <= 0 {
		p.Timeout = DefaultPhaseTimeout
	}
	if p.Grace <= 0 {
		p.Grace = DefaultGrace
	}
	return p
}

// answer — ответ одного источника (тело либо ошибка).
type answer struct {
	src  Source
	body []byte
	err  error
}

// answered: источник ответил по существу (дал тело или сказал «нет файла»).
func (a answer) answered() bool { return a.err == nil || errors.Is(a.err, ErrNotFound) }

// queryAll опрашивает все источники параллельно. Возвращает ответы в порядке
// источников; не успевшие до отсрочки/предела отсутствуют в результате.
func queryAll(ctx context.Context, srcs []Source, name string, p QueryPolicy) []answer {
	p = p.withDefaults()
	ctx, cancel := context.WithTimeout(ctx, p.Timeout)
	defer cancel()
	type indexed struct {
		i int
		a answer
	}
	ch := make(chan indexed, len(srcs))
	for i, s := range srcs {
		go func(i int, s Source) {
			b, err := s.Fetch(ctx, name)
			ch <- indexed{i, answer{src: s, body: b, err: err}}
		}(i, s)
	}
	got := make([]*answer, len(srcs))
	var graceC <-chan time.Time
loop:
	for n := 0; n < len(srcs); n++ {
		select {
		case r := <-ch:
			a := r.a
			got[r.i] = &a
			if a.answered() && graceC == nil {
				graceC = time.After(p.Grace)
			}
		case <-graceC:
			break loop
		case <-ctx.Done():
			break loop
		}
	}
	out := make([]answer, 0, len(srcs))
	for _, a := range got {
		if a != nil {
			out = append(out, *a)
		}
	}
	return out
}
