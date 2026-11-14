// SPDX-License-Identifier: GPL-3.0-only

package api

// settings_subscribe.go — SubscribeSettings: поток изменений карты настроек,
// чтобы окно приложения и веб-панель видели одно и то же (хвосты A).

import (
	"context"
	"errors"

	"connectrpc.com/connect"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
)

// SubscribeSettings шлёт SettingsChanged при каждом изменении настроек: SetSettings,
// ResetSettings, восстановление копии. Начального снимка нет — его даёт
// GetSettings. Клиент, не успевающий читать (буфер на 16 событий переполнен),
// получает ABORTED и должен переподписаться, перечитав настройки.
func (s *Server) SubscribeSettings(ctx context.Context, req *connect.Request[lavrayv1.SubscribeSettingsRequest], stream *connect.ServerStream[lavrayv1.SettingsChanged]) error {
	return s.serveSettings(ctx, stream.Send)
}

// serveSettings отдаёт события через send, пока клиент не уйдёт (ctx) или не
// переполнит буфер. Выход по отмене — тихий, подписка снимается всегда.
func (s *Server) serveSettings(ctx context.Context, send func(*lavrayv1.SettingsChanged) error) error {
	events, unsubscribe := s.subscribeSettings()
	defer unsubscribe()
	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, open := <-events:
			if !open {
				return connect.NewError(connect.CodeAborted,
					errors.New("подписка на настройки закрыта: клиент не успевает читать события, подпишитесь заново"))
			}
			if err := send(ev); err != nil {
				return err
			}
		}
	}
}
