// SPDX-License-Identifier: GPL-3.0-only

package store

// SetWarnLogger задаёт приёмник предупреждений хранилища: то, что не мешает
// операции, но о чём администратор должен знать (временный каталог копии не
// удалён, копия не отключена после COMMIT). Хранилище само журнала не имеет,
// его даёт слой API.
func (s *Store) SetWarnLogger(f func(format string, args ...any)) { s.warn = f }

func (s *Store) warnf(format string, args ...any) {
	if s.warn != nil {
		s.warn(format, args...)
	}
}
