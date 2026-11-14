// SPDX-License-Identifier: GPL-3.0-only

//go:build singbox_run

// singbox.go — реальный раннер sing-box (форк 628a9ed, см. NEKO_PATCHES.md).
// ЗА BUILD-ТЕГОМ //go:build singbox_run: локальная сборка без сети не может
// скачать модуль форка (go.mod: replace github.com/sagernet/sing-box → …);
// основной код пакета inst от этого файла НЕ зависит (Runner-интерфейс).
//
// Сборка с раннером: go build -tags singbox_run (плюс теги with_gvisor,
// with_quic, … — по целевой платформе, см. core-port-plan §2).
//
// LNB-18: никакие секреты не передаются через argv/env — конфиг целиком
// уходит в box.New через память (JSON собирает cfgbuild в этом же процессе).
package inst

import (
	"context"
	"fmt"
	"net"
	"sync"

	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/include"
	"github.com/sagernet/sing-box/option"
	sbjson "github.com/sagernet/sing/common/json"
	M "github.com/sagernet/sing/common/metadata"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/traffic"
)

// SingBoxRunner — запуск форка sing-box в текущем процессе (одновременно —
// один инстанс; менеджер гарантирует сериализацию, дубль — защита).
type SingBoxRunner struct {
	mu       sync.Mutex
	instance *box.Box
	// closing — идущее закрытие instance (I3, T-14): Close может занять время
	// (TUN/маршруты/QUIC-соединения). Повторные и параллельные Stop ждут ЭТОГО
	// закрытия, а не зовут Close ещё раз; новый Start запрещён, пока instance
	// не освободит ресурсы.
	closing *closeOp
	// logSink — приёмник журнала sing-box (SetLogSink, T-33); nil — журнал
	// остаётся в stderr процесса, где его никто не читает.
	logSink LogSink
	// traffic — источник соединений запущенного инстанса (T-79); nil вне работы.
	traffic traffic.Source
}

// closeOp — одно закрытие инстанса. err читать только после <-done.
type closeOp struct {
	done chan struct{}
	err  error
}

// NewSingBoxRunner создаёт раннер (инстанс создаётся в Start).
func NewSingBoxRunner() *SingBoxRunner { return &SingBoxRunner{} }

// SetLogSink подключает приёмник журнала sing-box: следующие Start
// перенаправляют вывод инстанса в sink (T-33). Вызывать до Start.
func (r *SingBoxRunner) SetLogSink(sink LogSink) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.logSink = sink
}

// Start поднимает box.Box из конфига и ждёт готовности (box.Box.Start
// возвращает после открытия inbounds/outbound'ов).
func (r *SingBoxRunner) Start(ctx context.Context, configJSON []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.instance != nil {
		return fmt.Errorf("singbox: инстанс уже запущен")
	}
	// ВАЖНО: option.Options в форке разбирается ТОЛЬКО своим декодером
	// (`sing/common/json`) с реестрами в контексте. Стандартный
	// encoding/json «успешно» декодировал, но оставлял поля пустыми —
	// box.New затем падал «parse rule[0]: missing conditions» на ЛЮБОМ
	// конфиге. Это ловилось только реальным прогоном sing-box.
	// T-01: инстанс живёт до Stop, а не до конца RPC Connect. Manager.Start
	// отменяет ctx сразу после успешного старта (inst.go: cancel()), и
	// раньше это обрывало каждое исходящее соединение
	// («operation was canceled»). WithoutCancel сохраняет значения
	// контекста, но не наследует отмену.
	baseCtx := include.Context(context.WithoutCancel(ctx))
	var options option.Options
	if err := sbjson.UnmarshalContext(baseCtx, configJSON, &options); err != nil {
		return fmt.Errorf("singbox: некорректный конфиг: %w", err)
	}
	// Рабочий каталог процесса — каталог данных (rule-set кэш, cache_file
	// с относительными путями); подставляет вызывающая сторона (serve).
	// sing-box 1.14 требует в контексте реестры протоколов (endpoint/
	// inbound/outbound/dns/service/certificate): без них box.New падает
	// «missing endpoint registry in context». include.Context(ctx) — ровно
	// тот набор, что использует штатный cmd/sing-box.
	// PlatformLogWriter: T-33 — вывод sing-box попадает в журнал ядра (раньше он
	// шёл в stderr, который никто не читает), T-79 — только при нём форк создаёт
	// менеджер соединений (platform_writer.go). Записи, накопленные до Start,
	// фабрика выдаёт уже в этот писатель.
	instance, err := box.New(box.Options{
		Context: baseCtx, Options: options,
		PlatformLogWriter: newPlatformWriter(options.Log, r.logSink),
	})
	if err != nil {
		return fmt.Errorf("singbox: box.New: %w", err)
	}
	if r.logSink != nil {
		silenceConsole(instance, r.logSink)
	}
	// T-14: ctx ограничивает время СТАРТА и отменяется Stop-ом (Disconnect во
	// время подключения) — но не доходит до инстанса (WithoutCancel выше), так
	// что проверяем его сами: до открытия портов и после старта.
	if err := ctx.Err(); err != nil {
		_ = instance.Close()
		return fmt.Errorf("singbox: старт отменён: %w", err)
	}
	if err := instance.Start(); err != nil {
		// старт не удался — ресурсы инстанса закрываем сразу (TUN мог
		// подняться частично; AUDIT §7.1.2)
		_ = instance.Close()
		return fmt.Errorf("singbox: старт: %w", err)
	}
	if err := ctx.Err(); err != nil {
		_ = instance.Close()
		return fmt.Errorf("singbox: старт отменён: %w", err)
	}
	r.instance = instance
	r.traffic = trafficSourceOf(baseCtx)
	return nil
}

// Stop — graceful-остановка: box.Close() (TUN/маршруты/инбаунды) с ожиданием
// в горутине; ctx дан менеджером (≤ StopTimeout). Дождаться Close ВАЖНО:
// выход из процесса до завершения Close оставляет висящий TUN (AUDIT §7.1.2).
//
// T-14 (A1-05): безопасен при повторных и параллельных вызовах — Close
// выполняется один раз, все вызывающие ждут его результата. Раньше второй Stop
// звал Close ещё раз (ошибка «file already closed») и закрывал уже закрытый
// канал (panic).
func (r *SingBoxRunner) Stop(ctx context.Context) error {
	r.mu.Lock()
	instance := r.instance
	if instance == nil {
		r.mu.Unlock()
		return nil // Stop после неудачного Start или повторный Stop — no-op
	}
	// I3 (аудит round 2): instance НЕ обнуляется до окончания Close. Раньше
	// при таймауте дескриптор терялся навсегда (утечка TUN), а следующий
	// Start поднимал второй инстанс поверх неубитого.
	op := r.closing
	if op == nil {
		op = &closeOp{done: make(chan struct{})}
		r.closing = op
		go func() {
			op.err = instance.Close()
			r.mu.Lock()
			r.instance = nil
			r.traffic = nil
			r.closing = nil
			r.mu.Unlock()
			close(op.done)
		}()
	}
	r.mu.Unlock()

	select {
	case <-op.done:
		if op.err != nil {
			return fmt.Errorf("singbox: закрытие: %w", op.err)
		}
		return nil
	case <-ctx.Done():
		// Close продолжает дорывать ресурсы в фоне; повторный Stop дождётся
		// его же, а не запустит второй Close.
		return fmt.Errorf("singbox: закрытие не уложилось в таймаут (%w); инстанс считается упавшим", ctx.Err())
	}
}

// DialProxy открывает соединение через исходящее "proxy" работающего инстанса
// (проверка связи после подключения). Если в конфиге нет исходящего с таким
// тегом (internal-full конфиг пользователя), берётся исходящее по умолчанию.
func (r *SingBoxRunner) DialProxy(ctx context.Context, network, address string) (net.Conn, error) {
	r.mu.Lock()
	instance := r.instance
	r.mu.Unlock()
	if instance == nil {
		return nil, ErrNotRunning
	}
	out, ok := instance.Outbound().Outbound(TestOutboundTag)
	if !ok {
		out = instance.Outbound().Default()
	}
	if out == nil {
		return nil, fmt.Errorf("singbox: в конфиге нет исходящего %q", TestOutboundTag)
	}
	return out.DialContext(ctx, network, M.ParseSocksaddr(address))
}

var (
	_ Runner      = (*SingBoxRunner)(nil)
	_ ProxyDialer = (*SingBoxRunner)(nil)
)
