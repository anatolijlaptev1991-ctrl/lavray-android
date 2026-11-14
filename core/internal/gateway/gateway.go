// SPDX-License-Identifier: GPL-3.0-only

// Package gateway — шлюз OpenWrt (T-76, OPENWRT-GATEWAY §3–5): nftables-
// include для fw4 (nft_render.go, детерминированный, golden), применение через
// `nft -f -` за интерфейсом Runner (nft_apply.go; в тестах — фейк), политики
// per-MAC (policies_repo.go), watchdog связи (gateway.go, watchdog_*.go) и
// Service, который всё это связывает (service.go).
package gateway

import (
	"sync"
	"time"
)

// Config — конфигурация шлюза (UCI /etc/config/lavray → флаги lavray-core).
type Config struct {
	TProxyPort  int      // порт tproxy-инбаунда ядра
	Enabled     bool     // глобально
	IPv6        bool     // отдельные ip6-правила, ВЫКЛ по умолчанию (§5)
	DNSHijack   bool     // порт 53 → ядро (в том числе к dnsmasq роутера)
	WatchdogMin int      // N минут без связи → снять правила (0 = 5)
	FwMark      int      // метка пакетов для policy routing tproxy (0 = 1)
	LANIfaces   []string // входящие интерфейсы LAN (пусто = br-lan); остальное не перехватывается
	SelfUser    string   // системный пользователь lavrayd (пусто = lavray)
}

// Действия политики устройства.
const (
	PolicyProxy  = "proxy"
	PolicyDirect = "direct"
	PolicyPreset = "preset"
)

// DevicePolicy — маршрутизация устройства LAN по MAC (GatewayService).
type DevicePolicy struct {
	MAC      string
	Policy   string // PolicyProxy | PolicyDirect | PolicyPreset
	PresetID string // для PolicyPreset
	Hostname string // метка из DHCP-аренды, только чтение
}

// Applier — применение/снятие include (реальный: fw4 reload/файл; тесты — мок).
type Applier interface {
	Apply(nftScript string) error
	Remove() error
}

// Watchdog — «нет связи N минут или ядро упало → снять правила → интернет
// напрямую; восстановилось → вернуть» (§5). Тесты — ручная прокрутка времени.
type Watchdog struct {
	Threshold time.Duration
	Applier   Applier
	Renderer  NftRenderer
	Config    Config
	Policies  func() []DevicePolicy

	mu         sync.Mutex
	lastBeat   time.Time
	armed      bool
	rulesUp    bool
	OnFailover func(active bool) // уведомление (событие/лог)
}

// Beat — связь есть (пинг через туннель/статус инстанса).
func (w *Watchdog) Beat(now time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.lastBeat = now
	w.armed = true
	if !w.rulesUp { // правила сняты failover-ом — восстановление
		// L41 (аудит round 2): rulesUp выставляется по РЕЗУЛЬТАТУ Apply.
		// Раньше флаг поднимался до вызова, и при ошибке fw4 правила
		// считались установленными — ретрая не было вовсе.
		if err := w.Applier.Apply(w.Renderer.Render(w.Config, w.Policies(), w.Config.SelfUser)); err == nil {
			w.rulesUp = true
			if w.OnFailover != nil {
				w.OnFailover(false)
			}
		}
	}
}

// Tick — проверка (вызывается раз в минуту).
func (w *Watchdog) Tick(now time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.armed || !w.rulesUp {
		return // не вооружён / уже снято (ждём Beat для возврата)
	}
	if now.Sub(w.lastBeat) >= w.Threshold {
		if err := w.Applier.Remove(); err == nil {
			w.rulesUp = false
			if w.OnFailover != nil {
				w.OnFailover(true) // интернет напрямую, уведомление
			}
		}
	}
}

// StartArmed — watchdog вооружён, правила применяются (вызов после старта
// шлюза). Если Apply не удался, watchdog остаётся вооружённым без правил: следующий
// Beat повторит применение.
func (w *Watchdog) StartArmed(now time.Time) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.lastBeat = now
	w.armed = true
	err := w.Applier.Apply(w.Renderer.Render(w.Config, w.Policies(), w.Config.SelfUser))
	w.rulesUp = err == nil
	return err
}
