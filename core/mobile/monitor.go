// SPDX-License-Identifier: GPL-3.0-only

package mobile

// monitor.go — монитор сети по умолчанию. Порт experimental/libbox/monitor.go
// (sing-box, GPL-3.0-or-later) с одним отличием: Kotlin регистрирует ОДИН
// слушатель сети на процесс (netHub), а мониторы основного и временных
// инстансов sing-box подписываются на хаб. Иначе каждая проверка задержки
// регистрировала бы и снимала NetworkCallback.

import (
	"sync"

	tun "github.com/sagernet/sing-tun"
	"github.com/sagernet/sing/common/control"
	E "github.com/sagernet/sing/common/exceptions"
	"github.com/sagernet/sing/common/logger"
	"github.com/sagernet/sing/common/x/list"
)

// netState — последнее сообщение Kotlin о сети по умолчанию.
type netState struct {
	name    string
	index   int32
	metered bool
}

// netHub раздаёт обновления сети подписчикам и владеет регистрацией у Kotlin.
type netHub struct {
	plat Platform

	ctl     sync.Mutex // сериализует Start/CloseNetworkMonitor
	started bool

	mu      sync.Mutex
	subs    []*defaultMonitor
	last    netState
	hasLast bool
}

func newNetHub(p Platform) *netHub { return &netHub{plat: p} }

// Update реализует NetworkListener; вызывается потоком Kotlin. Обход выполняется
// в отдельной горутине с ожиданием: JNI-поток Android имеет малый стек
// (golang/go#68760), а обработчик ходит в Platform.Interfaces().
func (h *netHub) Update(name string, index int32, metered bool) {
	h.mu.Lock()
	h.last, h.hasLast = netState{name, index, metered}, true
	subs := append([]*defaultMonitor(nil), h.subs...)
	h.mu.Unlock()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for _, s := range subs {
			s.update(name, index, metered)
		}
	}()
	<-done
}

func (h *netHub) subscribe(m *defaultMonitor) error {
	h.ctl.Lock()
	defer h.ctl.Unlock()
	h.mu.Lock()
	h.subs = append(h.subs, m)
	last, hasLast := h.last, h.hasLast
	h.mu.Unlock()
	if h.started {
		if hasLast {
			m.update(last.name, last.index, last.metered)
		}
		return nil
	}
	if err := h.plat.StartNetworkMonitor(h); err != nil {
		h.remove(m)
		return err
	}
	h.started = true
	return nil
}

func (h *netHub) unsubscribe(m *defaultMonitor) error {
	h.ctl.Lock()
	defer h.ctl.Unlock()
	if empty := h.remove(m); empty && h.started {
		h.started = false
		h.mu.Lock()
		h.hasLast = false
		h.mu.Unlock()
		return h.plat.CloseNetworkMonitor()
	}
	return nil
}

// remove убирает подписчика и сообщает, остался ли хаб без подписчиков.
func (h *netHub) remove(m *defaultMonitor) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i, s := range h.subs {
		if s == m {
			h.subs = append(h.subs[:i:i], h.subs[i+1:]...)
			break
		}
	}
	return len(h.subs) == 0
}

var _ tun.DefaultInterfaceMonitor = (*defaultMonitor)(nil)

// defaultMonitor — tun.DefaultInterfaceMonitor одного экземпляра sing-box.
type defaultMonitor struct {
	bp     *boxPlatform
	logger logger.Logger

	access       sync.Mutex
	callbacks    list.List[tun.DefaultInterfaceUpdateCallback]
	myInterfaces []string
	current      *control.Interface
	initialized  bool
}

func (m *defaultMonitor) Start() error { return m.bp.h.hub.subscribe(m) }

func (m *defaultMonitor) Close() error { return m.bp.h.hub.unsubscribe(m) }

func (m *defaultMonitor) DefaultInterface() *control.Interface {
	m.access.Lock()
	defer m.access.Unlock()
	return m.current
}

// OverrideAndroidVPN и AndroidVPNEnabled относятся к режиму без платформы.
func (m *defaultMonitor) OverrideAndroidVPN() bool { return false }
func (m *defaultMonitor) AndroidVPNEnabled() bool  { return false }

func (m *defaultMonitor) RegisterCallback(cb tun.DefaultInterfaceUpdateCallback) *list.Element[tun.DefaultInterfaceUpdateCallback] {
	m.access.Lock()
	defer m.access.Unlock()
	return m.callbacks.PushBack(cb)
}

func (m *defaultMonitor) UnregisterCallback(el *list.Element[tun.DefaultInterfaceUpdateCallback]) {
	m.access.Lock()
	defer m.access.Unlock()
	m.callbacks.Remove(el)
}

func (m *defaultMonitor) RegisterMyInterface(name string) {
	m.access.Lock()
	defer m.access.Unlock()
	m.myInterfaces = append(m.myInterfaces, name)
}

func (m *defaultMonitor) MyInterfaces() []string {
	m.access.Lock()
	defer m.access.Unlock()
	return m.myInterfaces
}

// update — обновление сети: освежает список интерфейсов и вызывает обратные
// вызовы sing-box (порт updateDefaultInterface из libbox).
func (m *defaultMonitor) update(name string, index int32, metered bool) {
	m.bp.setExpensive(metered)
	nm := m.bp.networkManager()
	if nm == nil {
		return
	}
	if err := nm.UpdateInterfaces(); err != nil {
		m.logger.Error(E.Cause(err, "update interfaces"))
	}
	m.access.Lock()
	if index == -1 {
		m.current = nil
		m.bp.setDefault(nil)
		m.initialized = true
		callbacks := m.callbacks.Array()
		m.access.Unlock()
		for _, cb := range callbacks {
			cb(nil, 0)
		}
		return
	}
	old := m.current
	found, err := nm.InterfaceFinder().ByIndex(int(index))
	if err != nil {
		m.access.Unlock()
		m.logger.Error(E.Cause(err, "find updated interface: ", name))
		return
	}
	m.current = found
	m.bp.setDefault(found)
	if m.initialized && old != nil && old.Name == found.Name && old.Index == found.Index {
		m.access.Unlock()
		return
	}
	m.initialized = true
	callbacks := m.callbacks.Array()
	m.access.Unlock()
	for _, cb := range callbacks {
		cb(found, 0)
	}
}
