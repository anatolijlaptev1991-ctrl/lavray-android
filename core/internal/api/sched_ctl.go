// SPDX-License-Identifier: GPL-3.0-only

package api

import (
	"context"
	"sync"
)

// schedCtl — жизненный цикл планировщика автообновления подписок. Нужен
// затем, чтобы замена всех данных (восстановление копии) могла остановить
// планировщик на время замены и запустить его заново: его состояние попыток
// привязано к прежним id групп, а обновление, начатое до замены, не должно
// записать результат поверх восстановленных данных. Нулевое значение готово
// к работе (планировщик не запущен).
type schedCtl struct {
	mu     sync.Mutex
	parent context.Context
	run    func(ctx context.Context) (stop func())
	stop   func() // остановка текущего запуска; nil — сейчас не запущен
	closed bool   // остановлен навсегда (shutdown): оживать нельзя
	n      int    // сколько раз запускался
}

// start запускает планировщик (run) и возвращает функцию окончательной
// остановки.
func (c *schedCtl) start(ctx context.Context, run func(ctx context.Context) (stop func())) (shutdown func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.parent, c.run, c.closed = ctx, run, false
	c.stop = run(ctx)
	c.n++
	return c.shutdown
}

func (c *schedCtl) shutdown() {
	c.mu.Lock()
	stop := c.stop
	c.stop, c.closed = nil, true
	c.mu.Unlock()
	if stop != nil {
		stop()
	}
}

// pause останавливает планировщик и ждёт завершения текущего обновления (до
// 5 с, это делает stop). Возвращённый resume запускает его заново, если за
// это время ядро не остановлено. Если планировщик не запущен, оба шага пусты.
func (c *schedCtl) pause() (resume func()) {
	c.mu.Lock()
	stop := c.stop
	c.stop = nil
	c.mu.Unlock()
	if stop == nil {
		return func() {}
	}
	stop()
	return func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.closed || c.stop != nil {
			return
		}
		c.stop = c.run(c.parent)
		c.n++
	}
}

// starts — число запусков (для тестов).
func (c *schedCtl) starts() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}
