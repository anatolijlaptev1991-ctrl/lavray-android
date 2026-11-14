// SPDX-License-Identifier: GPL-3.0-only

package web

// limiter.go — защита входа от перебора. Три независимых уровня:
//
//   - по учётной записи (главный: за NAT лимит по адресу бесполезен): с 5-й
//     неудачи пауза 1, 2, 4, 8, 16 с, на 10-й неудаче — блок на 15 минут; после
//     блока счётчик возвращается к 5, поэтому следующая серия сразу идёт с пауз;
//   - по адресу клиента: IPFailures неудач за окно IPWindow — блок на IPBlock;
//   - общий: не более GlobalPerMinute допущенных попыток в минуту.
//
// Карты записей и адресов ограничены по размеру: при переполнении вытесняются
// давно не тронутые записи без блока. Блокированную запись вытесняют только
// когда другого выхода нет.

import (
	"strings"
	"sync"
	"time"
)

// entry — счётчик неудач одной записи или одного адреса.
type entry struct {
	fails        int
	windowStart  time.Time // только для адресов
	blockedUntil time.Time
	locked       bool // длинный блок записи (10 неудач)
	touched      time.Time
}

// Limiter — лимитер попыток входа; безопасен для параллельного использования.
type Limiter struct {
	now func() time.Time
	cfg LimiterConfig

	mu       sync.Mutex
	accounts map[string]*entry
	ips      map[string]*entry
	gStart   time.Time
	gCount   int
}

// NewLimiter создаёт лимитер; now == nil — time.Now.
func NewLimiter(now func() time.Time, cfg LimiterConfig) *Limiter {
	if now == nil {
		now = time.Now
	}
	return &Limiter{now: now, cfg: cfg.withDefaults(), accounts: map[string]*entry{}, ips: map[string]*entry{}}
}

// normalizeAccount приводит имя к ключу: без регистра и пробелов по краям, не
// длиннее 128 байт (длинное имя от клиента не должно раздувать память).
func normalizeAccount(name string) string {
	if len(name) > 2*maxAccountKB { // не копировать гигантское имя целиком
		name = name[:2*maxAccountKB]
	}
	k := strings.ToLower(strings.TrimSpace(name))
	if len(k) > maxAccountKB {
		k = k[:maxAccountKB]
	}
	return k
}

// Allow решает, можно ли проверять пароль: 0 — можно (попытка засчитана в общий
// лимит), иначе — через сколько повторить.
func (l *Limiter) Allow(account, ip string) time.Duration {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	var wait time.Duration
	if e := l.accounts[normalizeAccount(account)]; e != nil {
		l.expireLock(e, now)
		wait = maxDur(wait, e.blockedUntil.Sub(now))
	}
	if e := l.ips[ip]; e != nil {
		wait = maxDur(wait, e.blockedUntil.Sub(now))
	}
	if wait > 0 {
		return wait
	}
	if now.Sub(l.gStart) >= time.Minute {
		l.gStart, l.gCount = now, 0
	}
	if l.gCount >= l.cfg.GlobalPerMinute {
		return l.gStart.Add(time.Minute).Sub(now)
	}
	l.gCount++
	return 0
}

// expireLock после окончания длинного блока возвращает счётчик к порогу пауз.
func (l *Limiter) expireLock(e *entry, now time.Time) {
	if e.locked && !now.Before(e.blockedUntil) {
		e.locked, e.fails = false, backoffFrom
	}
}

// Failure записывает неудачную попытку.
func (l *Limiter) Failure(account, ip string) {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	a := l.touch(l.accounts, normalizeAccount(account), now)
	l.expireLock(a, now)
	a.fails++
	switch {
	case a.fails >= lockAt:
		a.locked, a.blockedUntil = true, now.Add(accountLock)
	case a.fails >= backoffFrom:
		a.blockedUntil = now.Add(baseDelay << (a.fails - backoffFrom))
	}
	if ip == "" {
		return
	}
	e := l.touch(l.ips, ip, now)
	if now.Sub(e.windowStart) >= l.cfg.IPWindow {
		e.windowStart, e.fails = now, 0
	}
	e.fails++
	if e.fails >= l.cfg.IPFailures {
		e.blockedUntil = now.Add(l.cfg.IPBlock)
	}
}

// Success сбрасывает счётчик записи (счётчик адреса истекает сам по окну).
func (l *Limiter) Success(account, _ string) {
	l.mu.Lock()
	delete(l.accounts, normalizeAccount(account))
	l.mu.Unlock()
}

// Sizes — текущие размеры карт записей и адресов (для тестов и диагностики).
func (l *Limiter) Sizes() (accounts, ips int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.accounts), len(l.ips)
}

// touch возвращает запись по ключу, создавая её; при переполнении карты сначала
// освобождает место.
func (l *Limiter) touch(m map[string]*entry, key string, now time.Time) *entry {
	e := m[key]
	if e == nil {
		for len(m) >= l.cfg.MaxEntries {
			evict(m, now)
		}
		e = &entry{windowStart: now}
		m[key] = e
	}
	e.touched = now
	return e
}

func maxDur(a, b time.Duration) time.Duration {
	if b > a {
		return b
	}
	return a
}
