// SPDX-License-Identifier: GPL-3.0-only

// rootchain.go — ротация корня доверия N → N+1 → … до тех пор, пока источник
// не ответит 404 (но не больше MaxRootSteps шагов). Каждый шаг проверяется
// порогом ключей ПРЕДЫДУЩЕГО корня и ключами нового (Trust.VerifyRootUpdate);
// промежуточные корни могут быть просрочены, срок проверяется у итогового.
package updates

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// MaxRootSteps — предел цепочки ротации за один запуск проверки.
const MaxRootSteps = 64

// RootName — имя файла корня версии v на источнике: «2.root.json».
func RootName(v int64) string { return fmt.Sprintf("%d.root.json", v) }

// AdvanceRoot тянет корни N+1, N+2, … с источников. Возвращает итоговое
// доверие и число принятых шагов. Источник отдал кандидата, но ни один не
// прошёл проверку, — ErrRootRotation (отзыв ключа нельзя обойти «пропуском»
// версии или подписью только старых ключей). Итоговый корень просрочен —
// ErrExpired (просроченный встроенный корень, у которого есть валидный
// преемник, не мешает).
func AdvanceRoot(ctx context.Context, srcs []Source, start *Trust, now time.Time) (*Trust, int, error) {
	return AdvanceRootWith(ctx, srcs, start, now, QueryPolicy{})
}

// AdvanceRootWith — AdvanceRoot с явными пределами опроса источников.
func AdvanceRootWith(ctx context.Context, srcs []Source, start *Trust, now time.Time, p QueryPolicy) (*Trust, int, error) {
	cur, steps := start, 0
	for steps < MaxRootSteps {
		next, err := nextRoot(ctx, srcs, cur, p)
		if err != nil {
			return nil, steps, err
		}
		if next == nil {
			break
		}
		cur = next
		steps++
	}
	if exp := cur.Root().Expires; !now.Before(exp) {
		return nil, steps, fmt.Errorf("%w: root.json версии %d истёк (%s)", ErrExpired, cur.Root().Version, exp.UTC().Format(time.RFC3339))
	}
	return cur, steps, nil
}

// nextRoot — следующий корень или nil, если цепочка закончилась. Цепочка
// заканчивается ТОЛЬКО когда каждый источник ответил «нет файла»: источник,
// который молчит, отвечает ошибкой или не успел, мог бы скрывать новый корень
// (отзыв ключа), поэтому такая проверка заканчивается ErrNetwork. Побеждает
// первый (по порядку списка) кандидат, прошедший проверку.
func nextRoot(ctx context.Context, srcs []Source, cur *Trust, p QueryPolicy) (*Trust, error) {
	p = p.withDefaults()
	p.Grace = p.Timeout // ждём всех: конец цепочки нельзя объявлять по части ответов
	answers := queryAll(ctx, srcs, RootName(cur.Root().Version+1), p)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var rejected error
	notFound := 0
	for _, a := range answers {
		switch {
		case errors.Is(a.err, ErrNotFound):
			notFound++
		case a.err != nil || len(a.body) == 0:
			// недоступен или пустой ответ: не 404, конец цепочки не подтверждён
		default:
			next, verr := cur.VerifyRootUpdate(a.body)
			if verr == nil {
				return next, nil
			}
			rejected = firstErr(rejected, verr)
		}
	}
	if rejected != nil {
		return nil, fmt.Errorf("%w: %v", ErrRootRotation, rejected)
	}
	if len(srcs) == 0 || notFound != len(srcs) {
		return nil, &MirrorError{Hosts: silentMirrors(srcs, answers), Root: cur.Root().Version + 1}
	}
	return nil, nil
}

// silentMirrors — источники, не ответившие «нет файла» (недоступны, молчат, отдали
// ошибку); их адреса попадают в сообщение для пользователя.
func silentMirrors(srcs []Source, answers []answer) []string {
	confirmed := map[Source]bool{}
	for _, a := range answers {
		if errors.Is(a.err, ErrNotFound) {
			confirmed[a.src] = true
		}
	}
	var hosts []string
	for i, s := range srcs {
		if !confirmed[s] {
			hosts = append(hosts, sourceLabel(s, i))
		}
	}
	if len(hosts) == 0 {
		hosts = []string{"нет источников"}
	}
	return hosts
}
