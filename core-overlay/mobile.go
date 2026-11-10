// SPDX-License-Identifier: GPL-3.0-only

// Package mobile — мост Go-ядра LAVRay и Android (gomobile bind, Java-пакет
// lavray.core). Контракт зафиксирован в docs/ARCHITECTURE.md; здесь только
// экспортируемая поверхность, всё остальное в файлах пакета неэкспортируемо,
// чтобы gomobile не генерировал лишних классов.
//
// Пакет копируется сборкой в core/mobile (scripts/build-core.sh): каталоги
// core/internal/... нельзя импортировать из другого модуля.
package mobile

import (
	"errors"
	"sync"
	"sync/atomic"

	"github.com/anatolijlaptev1991-ctrl/lavray/core/internal/version"
)

// Platform реализует Kotlin (процесс :vpn).
type Platform interface {
	// OpenTun вызывает VpnService.Builder.establish и возвращает дескриптор.
	// optionsJSON: {mtu, inet4:[cidr], inet6:[cidr], dns:[ip], includePackages:[],
	// excludePackages:[], route4:[cidr], route6:[cidr]}. Возвращается НЕЗАВИСИМЫЙ
	// дескриптор (Kotlin делает dup и отдаёт копию): им владеет Go и закрывает его вместе
	// с туннелем. Исходный дескриптор держит Kotlin, пока интерфейс должен существовать.
	OpenTun(optionsJSON string) (int32, error)
	// Protect вызывает VpnService.protect(fd).
	Protect(fd int32) error
	// FindOwnerUid вызывает ConnectivityManager.getConnectionOwnerUid (API 29+), -1 если нет.
	FindOwnerUid(proto int32, srcIP string, srcPort int32, dstIP string, dstPort int32) (int32, error)
	PackagesForUid(uid int32) string // пакеты через запятую
	// Монитор сети по умолчанию; listener вызывается при каждой смене.
	StartNetworkMonitor(l NetworkListener) error
	CloseNetworkMonitor() error
	// Interfaces возвращает JSON-массив {name,index,mtu,addrs:[cidr],flags,metered,type}
	// (flags — битовая маска IFF_* Linux, type — "wifi"|"cellular"|"ethernet"|"other";
	// необязательные поля dns и gateway — массивы адресов).
	Interfaces() (string, error)
}

// NetworkListener принимает смену сети по умолчанию.
type NetworkListener interface {
	// name "" и index -1 означают отсутствие сети.
	Update(name string, index int32, metered bool)
}

// Options — параметры запуска ядра.
type Options struct {
	DataDir, TempDir, SocketPath, AuthToken string
	MemoryLimitMB                           int32 // 0 — без ограничения
	// TimeZone — идентификатор IANA (java.util.TimeZone.getDefault().id): у Go
	// на Android нет /etc/localtime, без него время в журнале было бы UTC.
	TimeZone string
}

var (
	lifeMu  sync.Mutex
	current *instance
)

// errAlreadyStarted — повторный Start без Stop.
var errAlreadyStarted = errors.New("mobile: ядро уже запущено")

// Start поднимает API ядра и возвращается, когда сокет слушает. p обязателен.
func Start(o *Options, p Platform) error {
	lifeMu.Lock()
	defer lifeMu.Unlock()
	if current != nil {
		return errAlreadyStarted
	}
	inst, err := startInstance(o, p)
	if err != nil {
		return err
	}
	current = inst
	return nil
}

// Stop корректно завершает ядро (sing-box, HTTP, планировщик, хранилище).
// Безопасен при повторном вызове и без Start.
func Stop() {
	lifeMu.Lock()
	defer lifeMu.Unlock()
	if current == nil {
		return
	}
	current.stop()
	current = nil
}

// Version — версия ядра LAVRay.
func Version() string { return version.Version }

// SetTun задаёт MTU туннеля из настроек приложения; значение применяется при
// следующем подключении, mtu <= 0 оставляет MTU из настроек ядра. stack
// принимается ради совместимости контракта и не действует: на Android стек
// всегда gvisor (patch.go, androidStack).
func SetTun(stack string, mtu int32) {
	tunSettings.Store(&tunParams{Stack: stack, MTU: int(mtu)})
}

// tunSettings — значения SetTun; nil, пока приложение их не задало.
var tunSettings atomic.Pointer[tunParams]
