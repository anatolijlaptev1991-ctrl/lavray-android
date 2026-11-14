// SPDX-License-Identifier: GPL-3.0-only

package inst

// MarkFailed переводит неактивный инстанс в Crashed с понятной причиной.
// Нужен, когда подключение сорвалось ещё до запуска раннера (нет прав на TUN,
// профиль удалён) и состояние иначе осталось бы «idle» без объяснений.
// Следующий Start из Crashed разрешён. Возвращает false, если инстанс не idle.
func (m *Manager) MarkFailed(reason string) bool {
	m.mu.Lock()
	if m.state != StateIdle || m.closed.Load() {
		m.mu.Unlock()
		return false
	}
	m.state = StateCrashed
	m.lastError = reason
	m.mu.Unlock()
	m.notifySnapshot()
	return true
}
