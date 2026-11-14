// SPDX-License-Identifier: GPL-3.0-only

package api

// connect_last.go — однократное автоподключение десктоп-оболочки
// (`lavray-core serve --connect-last`). В отличие от headless.Loop это одна
// серия попыток при старте, а не вечный цикл: ручное отключение пользователя
// остаётся отключением. Выбор профиля и режима — общий с headless
// (ConnectLast: remember.enable/remember.id/remember.modes).

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	"connectrpc.com/connect"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/inst"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/logs"
)

// connectLastBudget — общий предел серии попыток (паузы + сами попытки).
const connectLastBudget = 30 * time.Second

// connectLastDelays — паузы между попытками: растут, всего ≤ 3 попыток.
var connectLastDelays = []time.Duration{2 * time.Second, 6 * time.Second}

// autoConnectState — счётчик ручных действий пользователя над соединением
// (Connect/Disconnect/RestartProfile по RPC). Автоподключение сверяет его
// перед каждой попыткой и уступает, если пользователь уже вмешался.
type autoConnectState struct {
	userOps atomic.Int64
}

func (a *autoConnectState) noteUserOp() { a.userOps.Add(1) }

// onceEnv — зависимости серии попыток (подменяются в тестах).
type onceEnv struct {
	attempt func(ctx context.Context) error
	state   func() inst.State
	userOps func() int64
	sleep   func(ctx context.Context, d time.Duration) error
	fail    func(err error) // показать итоговый сбой пользователю
	logf    func(format string, args ...any)
	delays  []time.Duration
}

// onceEnvironment — окружение серии для этого сервера; sleep nil → реальный таймер.
func (s *Server) onceEnvironment(sleep func(context.Context, time.Duration) error) onceEnv {
	if sleep == nil {
		sleep = sleepCtx
	}
	return onceEnv{
		attempt: s.ConnectLast,
		state:   func() inst.State { return s.inst.Status().State },
		userOps: s.auto.userOps.Load,
		sleep:   sleep,
		fail:    s.reportAutoConnectFailure,
		logf: func(format string, args ...any) {
			s.logs.Logf(logs.LevelInfo, logs.SourceCore, "connect-last: "+format, args...)
		},
		delays: connectLastDelays,
	}
}

// ConnectLastOnce — серия попыток автоподключения при старте ядра. Тихо
// ничего не делает, если автоподключение выключено, профиля нет, ядро уже
// занято или пользователь успел подключиться/отключиться сам.
func (s *Server) ConnectLastOnce(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, connectLastBudget)
	defer cancel()
	return runConnectLastOnce(ctx, s.onceEnvironment(nil))
}

// runConnectLastOnce — сама серия: ≤ len(delays)+1 попыток, повторы только для
// временных сбоев. Ошибка наружу не возвращается: сбой показывается через
// env.fail и журнал, отмена ctx — штатный выход.
func runConnectLastOnce(ctx context.Context, env onceEnv) error {
	base := env.userOps()
	for i := 0; ; i++ {
		if ctx.Err() != nil || !env.mayAttempt(base, i > 0) {
			return nil
		}
		err := env.attempt(ctx)
		if err == nil || ctx.Err() != nil {
			return nil
		}
		if isPermanentConnectErr(err) || i >= len(env.delays) {
			env.finish(base, err)
			return nil
		}
		env.logf("временный сбой (%v), повтор через %s", err, env.delays[i])
		if env.sleep(ctx, env.delays[i]) != nil {
			return nil
		}
	}
}

// mayAttempt — ядро всё ещё «ничьё»: пользователь не вмешивался, инстанс не
// подключён и не подключается. Crashed допустим только перед повтором (это
// след нашей же неудачной попытки).
func (env onceEnv) mayAttempt(base int64, retry bool) bool {
	if env.userOps() != base {
		return false
	}
	st := env.state()
	return st == inst.StateIdle || (retry && st == inst.StateCrashed)
}

// finish — итог неудачной серии: тихие случаи молчат, остальное видно в статусе.
func (env onceEnv) finish(base int64, err error) {
	if errors.Is(err, errNoRememberedProfile) && !errors.Is(err, errRememberedGone) {
		return // выключено или не запомнено: штатный no-op
	}
	env.logf("автоподключение не выполнено: %v", err)
	if env.userOps() == base {
		env.fail(err)
	}
}

// isPermanentConnectErr — сбой, который повтор не исправит: нет профиля, нужны
// права, неверные настройки.
func isPermanentConnectErr(err error) bool {
	if errors.Is(err, errNoRememberedProfile) || errors.Is(err, inst.ErrNotRunning) {
		return true
	}
	switch connect.CodeOf(err) {
	case connect.CodeFailedPrecondition, connect.CodeInvalidArgument,
		connect.CodeNotFound, connect.CodePermissionDenied:
		return true
	}
	return false
}

// reportAutoConnectFailure — делает сбой видимым без изменений контракта:
// инстанс переходит в «ошибку» с текстом причины (CoreStatus.last_error), а
// следующее ручное подключение это состояние снимает.
func (s *Server) reportAutoConnectFailure(err error) {
	msg := err.Error()
	var ce *connect.Error
	if errors.As(err, &ce) {
		msg = ce.Message()
	}
	s.inst.MarkFailed(msg)
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
