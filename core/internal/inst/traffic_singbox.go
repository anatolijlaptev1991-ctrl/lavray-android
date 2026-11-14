// SPDX-License-Identifier: GPL-3.0-only

//go:build singbox_run

package inst

// traffic_singbox.go — адаптер trafficcontrol.Manager форка → traffic.Source (T-79).
// Manager создаётся в box.New только при непустом box.Options.PlatformLogWriter
// (box.go форка), clash_api для этого не нужен и запрещён (buildtags).

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/sagernet/sing-box/common/trafficcontrol"
	"github.com/sagernet/sing/common/observable"
	"github.com/sagernet/sing/service"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/traffic"
)

// eventBuffer — размер буфера подписчика: переполнение теряет события, а не
// тормозит sing-box.
const eventBuffer = 64

// errManagerNotStarted — Manager ещё не инициализирован (box.Start не вызывался).
var errManagerNotStarted = errors.New("inst: менеджер соединений не запущен")

// subscription — подписка на события Manager.
type subscription = observable.Subscription[trafficcontrol.ConnectionEvent]

// TrafficSource — источник соединений и счётчиков запущенного инстанса; nil,
// если инстанс не запущен или остановлен.
func (r *SingBoxRunner) TrafficSource() traffic.Source {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closing != nil {
		return nil // идёт закрытие: показывать и закрывать уже нечего
	}
	return r.traffic
}

// managerSource — traffic.Source над живым Manager инстанса.
type managerSource struct{ m *trafficcontrol.Manager }

// trafficSourceOf достаёт Manager из контекста инстанса (после box.New). nil —
// Manager не создан.
func trafficSourceOf(ctx context.Context) traffic.Source {
	m := service.PtrFromContext[trafficcontrol.Manager](ctx)
	if m == nil {
		return nil
	}
	return managerSource{m: m}
}

func (s managerSource) Active() []traffic.ConnInfo {
	metas := s.m.Connections()
	out := make([]traffic.ConnInfo, 0, len(metas))
	for _, m := range metas {
		out = append(out, connInfoFromMeta(m, false))
	}
	return out
}

func (s managerSource) Closed() []traffic.ConnInfo {
	metas := s.m.ClosedConnections()
	out := make([]traffic.ConnInfo, 0, len(metas))
	for _, m := range metas {
		out = append(out, connInfoFromMeta(m, true))
	}
	return out
}

func (s managerSource) Total() (up, down int64) { return s.m.Total() }

// Close закрывает активное соединение по строковому id. uuid-тип форка не
// импортируем (модуль у нас косвенный): тип id берём у самих метаданных.
func (s managerSource) Close(id string) error {
	for _, meta := range s.m.Connections() {
		if !strings.EqualFold(meta.ID.String(), id) { // UUID без учёта регистра
			continue
		}
		tracker := s.m.Connection(meta.ID)
		if tracker == nil {
			break // закрылось между обходом и поиском
		}
		_ = tracker.Close()
		return nil
	}
	return traffic.ErrNotFound
}

// Subscribe пересылает события Manager в канал нейтрального типа. Горутина
// живёт до cancel или закрытия Manager.
func (s managerSource) Subscribe() (events <-chan traffic.Event, cancel func()) {
	out := make(chan traffic.Event, eventBuffer)
	sub, done, err := s.subscribe()
	if err != nil {
		close(out)
		return out, func() {}
	}
	stop := make(chan struct{})
	go func() {
		defer close(out)
		for {
			select {
			case ev := <-sub:
				if e, ok := eventFromManager(ev); ok {
					select {
					case out <- e:
					default: // подписчик отстал — событие теряется
					}
				}
			case <-done:
				return
			case <-stop:
				return
			}
		}
	}()
	var once sync.Once
	return out, func() {
		once.Do(func() {
			close(stop)
			s.m.UnSubscribeEvents(sub)
		})
	}
}

// subscribe вызывает SubscribeEvents; Manager без инициализации (до старта
// инстанса) паникует на пустом наблюдателе — превращаем это в ошибку.
func (s managerSource) subscribe() (sub subscription, done <-chan struct{}, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = errManagerNotStarted
		}
	}()
	return s.m.SubscribeEvents()
}

func eventFromManager(ev trafficcontrol.ConnectionEvent) (traffic.Event, bool) {
	switch ev.Type {
	case trafficcontrol.ConnectionEventNew:
		return traffic.Event{Kind: traffic.EventOpened, ID: ev.ID.String()}, true
	case trafficcontrol.ConnectionEventClosed:
		return traffic.Event{Kind: traffic.EventClosed, ID: ev.ID.String()}, true
	}
	return traffic.Event{}, false
}

var (
	_ traffic.Source  = managerSource{}
	_ TrafficProvider = (*SingBoxRunner)(nil)
)
