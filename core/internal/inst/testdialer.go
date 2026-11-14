// SPDX-License-Identifier: GPL-3.0-only

package inst

// testdialer.go — контракт временного инстанса для URL- и полного теста
// (T-84). Без тега singbox_run: api зависит только от интерфейса, а реальную
// реализацию (NewTestInstance) даёт testinst.go; без тега — заглушка.

import (
	"context"
	"errors"
	"net"
)

// TestOutboundTag — тег исходящего проверяемого профиля в тестовом конфиге:
// cfgbuild всегда называет «последнюю милю» цепочки "proxy".
const TestOutboundTag = "proxy"

// TestDialer — временный инстанс sing-box с единственным исходящим профиля,
// без входящих и без портов. Любой трафик через него идёт только через это
// исходящее — никогда напрямую. Горутинно-безопасен; Close обязателен сразу
// после проверки.
type TestDialer interface {
	// DialContext открывает соединение (network — "tcp"/"udp", address —
	// "host:port") через исходящее профиля; имя хоста уходит серверу профиля.
	DialContext(ctx context.Context, network, address string) (net.Conn, error)
	// ListenPacket открывает UDP через исходящее профиля. destination
	// ("host:port") — первая цель обмена: SOCKS5 UDP ASSOCIATE и XUDP
	// сообщают её серверу при установке, поэтому пустой адрес не годится.
	// В WriteTo можно передавать любой net.Addr, чья String() — "host:port".
	ListenPacket(ctx context.Context, destination string) (net.PacketConn, error)
	// Close останавливает инстанс и освобождает ресурсы; повторный вызов
	// безопасен и возвращает результат первого.
	Close() error
}

// TestInstanceFactory создаёт TestDialer из конфига sing-box (JSON из
// cfgbuild.Build с ForTest). api подменяет её в тестах.
type TestInstanceFactory func(ctx context.Context, configJSON []byte) (TestDialer, error)

// ErrTestInstanceUnsupported — сборка ядра без sing-box (нет тега
// singbox_run): временный инстанс поднять нечем.
var ErrTestInstanceUnsupported = errors.New("inst: тестовый инстанс требует сборки с тегом singbox_run")
