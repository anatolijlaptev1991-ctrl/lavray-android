// SPDX-License-Identifier: GPL-3.0-only

package inst

// traffic.go — доступ API к источнику статистики запущенного инстанса (T-79).
// Источник принадлежит раннеру: настоящий (SingBoxRunner) отдаёт адаптер над
// trafficcontrol.Manager форка, FakeRunner — подставной.

import "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/traffic"

// TrafficProvider — раннер, отдающий источник статистики своего инстанса.
// Источник появляется после успешного Start и пропадает после Stop; между
// запусками раннера это разные объекты с пустой историей.
type TrafficProvider interface {
	TrafficSource() traffic.Source
}

// Traffic возвращает источник соединений и статистики. nil — источника нет:
// инстанс не запущен, запускается или останавливается (трафика, который стоило
// бы показывать или закрывать, в эти моменты нет), либо раннер статистики не
// ведёт (NopRunner). Отдавать источник только в состоянии Ready нужно ещё и
// затем, чтобы Disconnect забрал его ДО остановки и прочитал итог после.
func (m *Manager) Traffic() traffic.Source {
	m.mu.Lock()
	state, runner := m.state, m.runner
	m.mu.Unlock()
	if state != StateReady {
		return nil
	}
	p, ok := runner.(TrafficProvider)
	if !ok {
		return nil
	}
	return p.TrafficSource()
}
