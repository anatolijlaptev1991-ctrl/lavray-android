// SPDX-License-Identifier: GPL-3.0-only

// Package inst — менеджер инстанса sing-box (W6, core-port-plan §6.1):
// ОДИН активный инстанс под мьютексом (fix AUDIT §7.1.1), FSM состояний
// (FR-UI-261), таймауты Connect 15 с / Disconnect 10 с (LNB-06), Start поверх
// бегущего → FAILED_PRECONDITION.
//
// Сам sing-box НЕ запускается здесь: Runner — интерфейс. Реальный раннер
// (box.New форка sing-box) — в singbox.go ЗА BUILD-ТЕГОМ //go:build
// singbox_run (локальная сборка без сети не тянет модуль форка); базовая
// сборка использует NopRunner (внятная ошибка), тесты — FakeRunner.
package inst

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// Таймауты FSM (LNB-06).
const (
	StartTimeout = 15 * time.Second
	StopTimeout  = 10 * time.Second
)

// State — FSM инстанса (FR-UI-261; тексты для UI даёт CoreState контракта).
type State int

const (
	StateIdle     State = iota // инстанс не запущен
	StateStarting              // сборка конфига + старт
	StateReady                 // работает
	StateStopping              // graceful-остановка в процессе
	StateCrashed               // старт/остановка не удалась (детали в LastError)
)

func (s State) String() string {
	switch s {
	case StateIdle:
		return "idle"
	case StateStarting:
		return "starting"
	case StateReady:
		return "ready"
	case StateStopping:
		return "stopping"
	case StateCrashed:
		return "crashed"
	}
	return fmt.Sprintf("state(%d)", int(s))
}

// Ошибки менеджера (API-слой маппит их в Connect-коды).
var (
	// ErrAlreadyRunning — Start поверх бегущего (FAILED_PRECONDITION).
	ErrAlreadyRunning = errors.New("inst: соединение уже активно (нужен Disconnect/RestartProfile)")
	// ErrNotRunning — Restart/Disconnect без активного соединения.
	ErrNotRunning = errors.New("inst: активного соединения нет")
)

// RunInfo — параметры запуска (что и как запущено; конфиг собирает слой API).
type RunInfo struct {
	ProfileID   int64
	GroupID     int64
	ProfileName string
	Mode        string // "system_proxy" | "tun" | "gateway_tproxy" | "gateway_tun"
	ConfigJSON  []byte // конфиг sing-box текущего запуска
	StartedAtMs int64
}

// Status — снимок менеджера (безопасно копируется).
type Status struct {
	State      State
	Run        *RunInfo // nil = idle
	LastError  string
	LastConfig []byte // конфиг текущего/ПОСЛЕДНЕГО запуска (redact — слой API)
}

// CoreStateIDLE-совместимость: версия ядра отдаётся отдельно (version.Version).
func (s Status) ActiveProfileID() int64 {
	if s.Run == nil {
		return 0
	}
	return s.Run.ProfileID
}

// Runner — интерфейс запуска инстанса sing-box. Реализации: SingBoxRunner
// (тег singbox_run), FakeRunner (тесты), NopRunner (сборка без тега).
//
// Контракт: Start возвращается после успешного старта (или с ошибкой);
// повторный Start на том же Runner без Stop между — ошибка. Stop — graceful:
// закрыть TUN/маршруты/инбаунды и дождаться остановки. Реализации должны
// быть готовы к Stop после НЕУДАВШЕГОСЯ Start (no-op).
type Runner interface {
	Start(ctx context.Context, configJSON []byte) error
	Stop(ctx context.Context) error
}

// ProxyDialer — раннер, умеющий открыть соединение через исходящее "proxy"
// запущенного инстанса (проверка связи после подключения, 0.2.1). Не входит в
// Runner: раннеры без этой возможности (NopRunner, FakeRunner без подставной
// функции) проверку связи просто пропускают.
type ProxyDialer interface {
	DialProxy(ctx context.Context, network, address string) (net.Conn, error)
}

// ErrDialUnsupported — раннер не умеет открывать соединения через исходящее.
var ErrDialUnsupported = errors.New("inst: раннер не умеет открывать соединения через исходящее")

// NopRunner — раннер по умолчанию в сборке без тега singbox_run: внятная
// ошибка вместо падения (десктоп-оболочка собирается с тегом; CI собирает
// оба варианта).
type NopRunner struct{}

// Start всегда объясняет, почему инстанс не стартует.
func (NopRunner) Start(ctx context.Context, configJSON []byte) error {
	return errors.New("inst: sing-box не встроен в эту сборку (пересоберите с тегом singbox_run)")
}

// Stop — no-op.
func (NopRunner) Stop(ctx context.Context) error { return nil }

// Manager — единственный активный инстанс. Все методы можно звать из любых
// горутин; события состояний рассылаются подписчикам (SubscribeEvents W6+).
type Manager struct {
	mu     sync.Mutex
	runner Runner

	state      State
	run        *RunInfo
	lastError  string
	lastConfig []byte

	startCancel context.CancelFunc // отмена текущего Start (L13)
	startDone   chan struct{}      // завершение текущего Start (I1); закрывается ПОСЛЕ записи итогового состояния
	// stopRequested — Stop пришёл, пока шёл Start (T-14, A1-06): Start,
	// вернувшись из раннера, не публикует Ready, а гасит инстанс сам.
	stopRequested bool
	// stop — идущая остановка (T-14, A1-05): параллельные Stop ждут её
	// результата, а не зовут runner.Stop повторно.
	stop *stopOp

	subMu sync.Mutex
	subs  map[chan Event]struct{}
	// closed — Shutdown отработал. Читают Start (под mu) и Subscribe/
	// notifySnapshot/Shutdown (под subMu), пишет только Shutdown; общего
	// мьютекса у этих путей нет, поэтому флаг атомарный (D-03). Запись всё
	// равно идёт под subMu, чтобы «closed + обмен subs» оставались атомарными.
	closed atomic.Bool
}

// Event — уведомление об изменении состояния (полный снимок — headless-режим
// контракта: событие несёт состояние, а не дельту).
type Event struct {
	Status Status
}

// stopOp — одна остановка инстанса. err читать только после <-done.
type stopOp struct {
	done chan struct{}
	err  error
}

// wait ждёт завершения остановки либо ctx.
func (o *stopOp) wait(ctx context.Context) error {
	select {
	case <-o.done:
		return o.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// NewManager создаёт менеджера поверх runner (nil → NopRunner).
func NewManager(runner Runner) *Manager {
	if runner == nil {
		runner = NopRunner{}
	}
	return &Manager{runner: runner, subs: map[chan Event]struct{}{}}
}

// Start запускает инстанс с готовым конфигом: Idle/Crashed → Starting →
// Ready; ошибка старта → Crashed + LastError. Start поверх Ready/Starting/
// Stopping → ErrAlreadyRunning. ctx ограничивает время старта (вызывающий
// даёт ≤ StartTimeout; сверх него — DeadlineExceeded, состояние Crashed).
func (m *Manager) Start(ctx context.Context, info RunInfo) error {
	m.mu.Lock()
	switch m.state {
	case StateReady, StateStarting, StateStopping:
		m.mu.Unlock()
		return ErrAlreadyRunning
	}
	if info.StartedAtMs == 0 {
		info.StartedAtMs = time.Now().UnixMilli()
	}
	if m.closed.Load() {
		m.mu.Unlock()
		return ErrNotRunning // L14: после Shutdown инстанс не поднимается
	}
	startCtx, cancel := context.WithCancel(ctx)
	m.startCancel = cancel
	startDone := make(chan struct{})
	m.startDone = startDone
	m.state = StateStarting
	m.run = &info
	m.lastError = ""
	m.mu.Unlock()
	m.notifySnapshot()

	err := m.runner.Start(startCtx, info.ConfigJSON)
	cancel()
	return m.finishStart(ctx, info, startDone, err)
}

// finishStart записывает итог Start и только потом закрывает startDone: Stop,
// ждущий конца старта, должен видеть окончательное состояние (T-14, A1-06;
// раньше startDone закрывался до записи, и Stop возвращал успех, а инстанс
// затем становился Ready).
//
// Если Stop пришёл во время старта, пользователь хотел отключиться, и Start
// не оставляет за собой работающий инстанс: отмена раннером → Idle (не
// Crashed), раннер отмену проигнорировал и поднялся → гасим его здесь же, без
// публикации Ready. В обоих случаях Start возвращает ошибку с context.Canceled
// (API отдаст CANCELED, и UI не включит системный прокси).
func (m *Manager) finishStart(ctx context.Context, info RunInfo, startDone chan struct{}, err error) error {
	m.mu.Lock()
	m.startCancel = nil
	m.startDone = nil
	cancelled := m.stopRequested
	m.stopRequested = false

	switch {
	case cancelled && err == nil:
		op := &stopOp{done: make(chan struct{})}
		m.stop = op
		m.state = StateStopping
		m.mu.Unlock()
		m.notifySnapshot()
		stopCtx, stopCancel := context.WithTimeout(context.WithoutCancel(ctx), StopTimeout)
		m.runStop(stopCtx, op)
		stopCancel()
		close(startDone)
		if op.err != nil {
			return errors.Join(fmt.Errorf("inst: старт отменён: %w", context.Canceled), op.err)
		}
		return fmt.Errorf("inst: старт отменён: %w", context.Canceled)

	case cancelled:
		m.state = StateIdle
		m.lastError = ""
		m.run = nil
		m.lastConfig = nil
		m.mu.Unlock()
		close(startDone)
		m.notifySnapshot()
		return fmt.Errorf("inst: старт отменён: %w", context.Canceled)

	case err != nil:
		// I2 (аудит round 2): при падении старта run не должен оставаться —
		// иначе Status показывает активный профиль в состоянии Crashed.
		m.state = StateCrashed
		m.lastError = err.Error()
		m.run = nil
		m.lastConfig = nil
		m.mu.Unlock()
		close(startDone)
		m.notifySnapshot()
		return fmt.Errorf("inst: старт инстанса: %w", err)
	}

	m.state = StateReady
	m.lastConfig = append([]byte(nil), info.ConfigJSON...)
	m.mu.Unlock()
	close(startDone)
	m.notifySnapshot()
	return nil
}

// Stop останавливает инстанс: Ready → Stopping → Idle. Неактивный
// (Idle/Crashed) Stop — no-op (идемпотентность Disconnect). ctx ограничивает
// graceful-ожидание (≤ StopTimeout); таймаут → Crashed + ошибка (раннер может
// ещё дорывать TUN в фоне — перезапуск в этом состоянии небезопасен, о чём и
// говорит состояние).
//
// T-14: повторный и параллельный Stop во время остановки не зовёт
// runner.Stop второй раз, а ждёт результата первой остановки (A1-05);
// Stop во время Start отменяет старт и ждёт, пока Start запишет итог, —
// после чего инстанс гарантированно не работает (A1-06).
func (m *Manager) Stop(ctx context.Context) error {
	for {
		m.mu.Lock()
		switch m.state {
		case StateIdle, StateCrashed:
			m.mu.Unlock()
			return nil
		case StateStopping:
			op := m.stop
			m.mu.Unlock()
			return op.wait(ctx)
		case StateStarting:
			// L13/I1: отменяем старт и ЖДЁМ, пока Start запишет итоговое
			// состояние (finishStart) — раннер, игнорирующий ctx, успевал
			// стать Ready уже после «успешного» Disconnect.
			m.stopRequested = true
			cancel := m.startCancel
			done := m.startDone
			m.mu.Unlock()
			if cancel != nil {
				cancel()
			}
			if done != nil {
				select {
				case <-done:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			continue // состояние окончательное: Idle/Crashed (или Ready, если Stop опоздал)
		}
		op := &stopOp{done: make(chan struct{})}
		m.stop = op
		m.state = StateStopping
		m.mu.Unlock()
		m.notifySnapshot()
		m.runStop(ctx, op)
		return op.err
	}
}

// runStop вызывает runner.Stop, записывает итог (Idle либо Crashed), будит
// всех, кто ждёт op, и рассылает снимок. Состояние перед вызовом — Stopping,
// m.stop == op.
func (m *Manager) runStop(ctx context.Context, op *stopOp) {
	err := m.runner.Stop(ctx)
	m.mu.Lock()
	if err != nil {
		m.state = StateCrashed
		m.lastError = err.Error()
		op.err = fmt.Errorf("inst: остановка инстанса: %w", err)
	} else {
		m.state = StateIdle
		m.run = nil
	}
	m.stop = nil
	m.mu.Unlock()
	close(op.done)
	m.notifySnapshot()
}

// Status возвращает снимок менеджера.
func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := Status{State: m.state, LastError: m.lastError}
	if m.run != nil {
		r := *m.run
		st.Run = &r
	}
	if m.lastConfig != nil {
		st.LastConfig = append([]byte(nil), m.lastConfig...)
	}
	return st
}

// DialProxy открывает соединение через исходящее "proxy" работающего инстанса
// (имя хоста уходит серверу профиля, трафик в обход исходящего невозможен).
// Нет работающего инстанса — ErrNotRunning; раннер не умеет — ErrDialUnsupported.
func (m *Manager) DialProxy(ctx context.Context, network, address string) (net.Conn, error) {
	m.mu.Lock()
	state, runner := m.state, m.runner
	m.mu.Unlock()
	if state != StateReady {
		return nil, ErrNotRunning
	}
	d, ok := runner.(ProxyDialer)
	if !ok {
		return nil, ErrDialUnsupported
	}
	return d.DialProxy(ctx, network, address)
}

// Subscribe — поток событий состояний. Первый кадр подписчик получает сам
// (снимок Status() до подписки — контракт SubscribeEvents), далее — каждый
// переход. Возврат: канал, отписка.
func (m *Manager) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, 8)
	m.subMu.Lock()
	if m.closed.Load() {
		m.subMu.Unlock()
		close(ch)
		return ch, func() {}
	}
	m.subs[ch] = struct{}{}
	m.subMu.Unlock()
	return ch, func() {
		m.subMu.Lock()
		delete(m.subs, ch)
		m.subMu.Unlock()
	}
}

// notifySnapshot рассылает снимок подписчикам (неблокирующе).
func (m *Manager) notifySnapshot() {
	st := m.Status()
	m.subMu.Lock()
	defer m.subMu.Unlock()
	if m.closed.Load() {
		return
	}
	for ch := range m.subs {
		select {
		case ch <- Event{Status: st}:
		default: // переполнение: подписчик отстанет — пропускаем кадр
		}
	}
}

// Shutdown — финальное закрытие (CoreService.Shutdown): остановка инстанса
// (graceful, AUDIT §7.1.2 — Close до выхода, никакого зависшего TUN) и
// закрытие подписок. Повторный вызов — no-op.
func (m *Manager) Shutdown(ctx context.Context) error {
	m.subMu.Lock()
	if m.closed.Load() {
		m.subMu.Unlock()
		return nil
	}
	m.closed.Store(true)
	subs := m.subs
	m.subs = map[chan Event]struct{}{}
	m.subMu.Unlock()
	for ch := range subs {
		close(ch)
	}
	return m.Stop(ctx)
}
