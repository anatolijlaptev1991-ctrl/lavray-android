// SPDX-License-Identifier: GPL-3.0-only

// idlebody.go — тело ответа с таймаутом простоя: источник, который прислал
// заголовки и замолчал, не держит проверку или загрузку вечно (ResponseHeaderTimeout
// защищает только до первого байта).
package updates

import (
	"context"
	"io"
	"time"
)

type idleBody struct {
	body   io.ReadCloser
	timer  *time.Timer
	stall  time.Duration
	cancel context.CancelFunc
}

// newIdleBody оборачивает тело: если за stall не прочитан ни один байт, запрос
// отменяется (cancel), и Read возвращает ошибку контекста. cancel вызывается и
// при Close, так что соединение освобождается.
func newIdleBody(body io.ReadCloser, stall time.Duration, cancel context.CancelFunc) io.ReadCloser {
	return &idleBody{body: body, stall: stall, cancel: cancel, timer: time.AfterFunc(stall, cancel)}
}

func (b *idleBody) Read(p []byte) (int, error) {
	n, err := b.body.Read(p)
	if n > 0 {
		b.timer.Reset(b.stall)
	}
	return n, err
}

func (b *idleBody) Close() error {
	b.timer.Stop()
	err := b.body.Close()
	b.cancel()
	return err
}
