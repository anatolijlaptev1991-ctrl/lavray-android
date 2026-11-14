// SPDX-License-Identifier: GPL-3.0-only

package web

import "context"

// RequestInfo — что middleware узнал о запросе; сервис WebSessionService
// читает это из контекста (Login нужен адрес клиента и признак TLS, остальным
// методам — сессия).
type RequestInfo struct {
	IP      string   // адрес клиента без порта (RemoteAddr; заголовкам прокси не верим)
	Secure  bool     // соединение по TLS: cookie получает Secure
	Bearer  bool     // запрос прошёл по Bearer-токену ядра (headless), сессии нет
	Session *Session // сессия по cookie; nil у Bearer и до входа
	Token   string   // токен cookie этой сессии (для выхода); не логировать
}

type infoKey struct{}

// WithInfo кладёт сведения о запросе в контекст.
func WithInfo(ctx context.Context, info RequestInfo) context.Context {
	return context.WithValue(ctx, infoKey{}, info)
}

// InfoFrom возвращает сведения о запросе; ok=false — запрос пришёл мимо
// веб-слушателя (основной сокет ядра с Bearer-токеном).
func InfoFrom(ctx context.Context) (RequestInfo, bool) {
	info, ok := ctx.Value(infoKey{}).(RequestInfo)
	return info, ok
}
