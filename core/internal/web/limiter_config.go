// SPDX-License-Identifier: GPL-3.0-only

package web

import "time"

// LimiterConfig — параметры лимитера; нулевые поля заменяются значениями по умолчанию.
type LimiterConfig struct {
	GlobalPerMinute int           // 120
	IPFailures      int           // 20
	IPWindow        time.Duration // 10 минут
	IPBlock         time.Duration // 5 минут
	MaxEntries      int           // 4096 на каждую из двух карт
}

const (
	backoffFrom  = 5 // неудач до первой паузы
	lockAt       = 10
	baseDelay    = time.Second
	accountLock  = 15 * time.Minute
	maxAccountKB = 128
)

func (c LimiterConfig) withDefaults() LimiterConfig {
	if c.GlobalPerMinute <= 0 {
		c.GlobalPerMinute = 120
	}
	if c.IPFailures <= 0 {
		c.IPFailures = 20
	}
	if c.IPWindow <= 0 {
		c.IPWindow = 10 * time.Minute
	}
	if c.IPBlock <= 0 {
		c.IPBlock = 5 * time.Minute
	}
	if c.MaxEntries <= 0 {
		c.MaxEntries = 4096
	}
	return c
}

// evict удаляет одну запись: сначала с истёкшим блоком (самую давнюю), и лишь
// если все блокированы — самую давнюю из блокированных.
func evict(m map[string]*entry, now time.Time) {
	var victim string
	var best *entry
	for k, e := range m {
		blocked := now.Before(e.blockedUntil)
		if best == nil || (blocked == now.Before(best.blockedUntil) && e.touched.Before(best.touched)) ||
			(!blocked && now.Before(best.blockedUntil)) {
			victim, best = k, e
		}
	}
	delete(m, victim)
}
