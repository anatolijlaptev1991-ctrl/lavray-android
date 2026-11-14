// SPDX-License-Identifier: GPL-3.0-only

package api

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"

	"connectrpc.com/connect"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/logs"
)

// Interceptor — обвязка ВСЕХ хендлеров сервера:
//
//  1. recover: паника хендлера (unary и стримингового) не убивает процесс
//     ядра — клиенту уходит INTERNAL, трейс — в лог-мультиплексор
//     (THREAT-MODEL «паника ядра», LNB-13/32; stack redact-нут по общим
//     правилам мультиплексора);
//  2. маппинг ошибок mapErr: хендлеры возвращают доменные ошибки как есть;
//  3. агентский доступ (agent.go): запросы с агентским токеном пропускаются
//     только к процедурам белого списка режима `agent.mode`, их параметры
//     ограничиваются, а ответы очищаются от адресов и секретов.
type interceptor struct {
	logs *logs.Mux
	mode func() string // текущий agent.mode; nil = off
}

func newInterceptor(lg *logs.Mux, mode func() string) *interceptor {
	return &interceptor{logs: lg, mode: mode}
}

func (i *interceptor) recoverLog(rcv any) {
	msg := fmt.Sprintf("ПАНИКА хендлера: %v", rcv)
	if i.logs != nil {
		// стек идёт в лог БЕЗ сырого вывода в stderr; redact применит Mux
		i.logs.Logf(logs.LevelError, logs.SourceAPI, "%s\n%s", msg, debug.Stack())
	}
}

// WrapUnary — unary-вызовы.
func (i *interceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (resp connect.AnyResponse, err error) {
		defer func() {
			if r := recover(); r != nil {
				i.recoverLog(r)
				resp = nil
				err = connect.NewError(connect.CodeInternal,
					fmt.Errorf("внутренняя ошибка ядра (паника обработана); подробности в журнале"))
			}
		}()
		agent := IsAgent(ctx)
		if err = i.agentBefore(ctx, req.Spec().Procedure, req.Header()); err != nil {
			return nil, err
		}
		if agent {
			if err = i.agentMessage(req.Spec().Procedure, req.Any()); err != nil {
				return nil, err
			}
		}
		resp, err = next(ctx, req)
		err = mapErr(err)
		if agent {
			if err != nil {
				return nil, agentSanitizeError(err)
			}
			if resp != nil {
				agentRedactResponse(resp.Any())
			}
		}
		return resp, err
	}
}

// WrapStreamingHandler — серверные стримы.
func (i *interceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) (err error) {
		defer func() {
			if r := recover(); r != nil {
				i.recoverLog(r)
				err = connect.NewError(connect.CodeInternal,
					fmt.Errorf("внутренняя ошибка ядра (паника обработана); подробности в журнале"))
			}
		}()
		agent := IsAgent(ctx)
		if agent {
			if err = i.agentBefore(ctx, conn.Spec().Procedure, conn.RequestHeader()); err != nil {
				return err
			}
			procedure := conn.Spec().Procedure
			conn = agentStream{StreamingHandlerConn: conn, check: func(msg any) error {
				return i.agentMessage(procedure, msg)
			}}
		}
		err = mapErr(next(ctx, conn))
		if agent && err != nil {
			// отказ политики уже понятен и чист; ошибки хендлера чистятся от адресов
			var ce *connect.Error
			if !errors.As(err, &ce) || ce.Code() != connect.CodePermissionDenied {
				err = agentSanitizeError(err)
			}
		}
		return err
	}
}

// WrapStreamingClient — клиентские стримы (сервером не используются;
// нужен для интерфейса connect.Interceptor).
func (i *interceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return func(ctx context.Context, spec connect.Spec) connect.StreamingClientConn {
		return next(ctx, spec)
	}
}

var _ connect.Interceptor = (*interceptor)(nil)
