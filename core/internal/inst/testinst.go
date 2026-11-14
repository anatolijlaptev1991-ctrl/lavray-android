// SPDX-License-Identifier: GPL-3.0-only

//go:build singbox_run

// testinst.go — временный инстанс sing-box для URL- и полного теста (T-84).
// Отдельный box.Box на каждую проверку, никак не связанный с Manager и его
// активным инстансом: другой набор реестров в контексте, свой журнал, нет
// общих файлов и портов.
package inst

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/include"
	"github.com/sagernet/sing-box/option"
	sbjson "github.com/sagernet/sing/common/json"
	M "github.com/sagernet/sing/common/metadata"
)

// testCloseTimeout — сколько Close ждёт box.Close. Остальное инстанс дорывает
// в фоне: проверка не должна зависать на закрытии чужого соединения.
const testCloseTimeout = 10 * time.Second

// testInstance — запущенный box.Box без входящих.
type testInstance struct {
	box    *box.Box
	cancel context.CancelFunc

	closeOnce sync.Once
	closeErr  error
}

// NewTestInstance поднимает временный инстанс из конфига cfgbuild.Build с
// ForTest. Инстанс не слушает порты: конфиг с входящими, конечными точками или
// сервисами отвергается, а experimental (cache_file, clash_api, v2ray_api)
// вырезается — кэш открывал бы общий файл, API занял бы порт. Журнал выключен,
// route.final закреплён за исходящим "proxy", чтобы любой трафик шёл только
// через него. Вызывающий обязан закрыть результат (Close).
func NewTestInstance(ctx context.Context, configJSON []byte) (TestDialer, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("singbox: тестовый инстанс: %w", err)
	}
	// Отмена ctx не доходит до инстанса (как в SingBoxRunner.Start): он живёт до
	// Close и снимается собственным cancel. Реестры протоколов — на каждый
	// инстанс свои: общий контекст разделил бы реестр сервисов между боксами.
	baseCtx, cancel := context.WithCancel(include.Context(context.WithoutCancel(ctx)))
	var options option.Options
	if err := sbjson.UnmarshalContext(baseCtx, configJSON, &options); err != nil {
		cancel()
		return nil, fmt.Errorf("singbox: некорректный тестовый конфиг: %w", err)
	}
	if err := sanitizeTestOptions(&options); err != nil {
		cancel()
		return nil, err
	}
	instance, err := box.New(box.Options{Context: baseCtx, Options: options})
	if err != nil {
		cancel()
		return nil, fmt.Errorf("singbox: box.New: %w", err)
	}
	if err := startBounded(ctx, instance); err != nil {
		cancel()
		return nil, err
	}
	// Страховка: трафик без маршрута уходит в исходящее по умолчанию. Если оно
	// не «proxy» (в конфиге нет такого тега), закрываем: тест пошёл бы мимо профиля.
	if def := instance.Outbound().Default(); def == nil || def.Tag() != TestOutboundTag {
		_ = instance.Close()
		cancel()
		return nil, fmt.Errorf("singbox: в тестовом конфиге нет исходящего %q", TestOutboundTag)
	}
	return &testInstance{box: instance, cancel: cancel}, nil
}

// sanitizeTestOptions приводит разобранные опции к виду «только исходящие».
func sanitizeTestOptions(o *option.Options) error {
	switch {
	case len(o.Inbounds) > 0:
		return fmt.Errorf("singbox: тестовый конфиг не должен содержать входящих")
	case len(o.Endpoints) > 0:
		return fmt.Errorf("singbox: тестовый конфиг не должен содержать endpoints")
	case len(o.Services) > 0:
		return fmt.Errorf("singbox: тестовый конфиг не должен содержать services")
	}
	o.Experimental = nil
	o.Log = &option.LogOptions{Disabled: true}
	if o.Route == nil {
		o.Route = &option.RouteOptions{}
	}
	o.Route.Final = TestOutboundTag
	return nil
}

// startBounded запускает инстанс, но не ждёт его дольше ctx: отмена теста
// (StopTests) не должна висеть на старте. Недозапущенный инстанс закрывается в фоне.
func startBounded(ctx context.Context, instance *box.Box) error {
	done := make(chan error, 1)
	go func() { done <- instance.Start() }()
	select {
	case err := <-done:
		if err != nil {
			// Start при ошибке закрывает инстанс сам.
			return fmt.Errorf("singbox: старт: %w", err)
		}
		return nil
	case <-ctx.Done():
		go func() {
			if <-done == nil {
				_ = instance.Close()
			}
		}()
		return fmt.Errorf("singbox: старт отменён: %w", ctx.Err())
	}
}

// DialContext — через исходящее по умолчанию (метод форка NekoDialContext).
func (t *testInstance) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return t.box.NekoDialContext(ctx, network, M.ParseSocksaddr(address))
}

// ListenPacket — то же исходящее. NekoDialUDP форка передаёт пустую цель, а
// SOCKS5 UDP ASSOCIATE и XUDP сообщают её серверу при установке, поэтому
// вызываем ListenPacket исходящего с настоящей целью.
func (t *testInstance) ListenPacket(ctx context.Context, destination string) (net.PacketConn, error) {
	return t.box.Outbound().Default().ListenPacket(ctx, M.ParseSocksaddr(destination))
}

// Close останавливает инстанс; ждёт не дольше testCloseTimeout.
func (t *testInstance) Close() error {
	t.closeOnce.Do(func() {
		done := make(chan error, 1)
		go func() {
			err := t.box.Close()
			t.cancel()
			done <- err
		}()
		select {
		case t.closeErr = <-done:
		case <-time.After(testCloseTimeout):
			t.closeErr = fmt.Errorf("singbox: закрытие тестового инстанса не уложилось в %s", testCloseTimeout)
		}
	})
	return t.closeErr
}

var _ TestDialer = (*testInstance)(nil)
