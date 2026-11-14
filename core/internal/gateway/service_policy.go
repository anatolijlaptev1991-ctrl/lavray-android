// SPDX-License-Identifier: GPL-3.0-only

package gateway

// service_policy.go — изменение политик устройств с откатом. Порядок: проверка →
// применение новых правил (если они сейчас установлены; nft-транзакция
// атомарна, при ошибке прежние правила остаются) → запись в настройки; сбой
// записи возвращает прежние правила.

import "fmt"

// SetPolicy добавляет или заменяет политику устройства. Возвращает
// нормализованную запись. Правила при установленном шлюзе обновляются сразу.
func (s *Service) SetPolicy(p DevicePolicy) (DevicePolicy, error) {
	norm, err := validatePolicy(p)
	if err != nil {
		return DevicePolicy{}, err
	}
	return norm, s.change(norm, false)
}

// ClearPolicy убирает политику устройства (оно наследует общую); отсутствие
// записи не ошибка.
func (s *Service) ClearPolicy(mac string) error {
	norm, err := NormalizeMAC(mac)
	if err != nil {
		return err
	}
	return s.change(DevicePolicy{MAC: norm}, true)
}

func validatePolicy(p DevicePolicy) (DevicePolicy, error) {
	mac, err := NormalizeMAC(p.MAC)
	if err != nil {
		return DevicePolicy{}, err
	}
	if !validAction(p.Policy) {
		return DevicePolicy{}, fmt.Errorf("%w: неизвестное действие %q", ErrBadPolicy, shorten(p.Policy))
	}
	out := DevicePolicy{MAC: mac, Policy: p.Policy}
	if p.Policy == PolicyPreset {
		id := safeToken(p.PresetID)
		if id == "" || id != p.PresetID {
			return DevicePolicy{}, fmt.Errorf("%w: для «по пресету» нужен preset_id из букв, цифр, «.», «_», «-»", ErrBadPolicy)
		}
		out.PresetID = id
	}
	return out, nil
}

func (s *Service) change(p DevicePolicy, remove bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	old := s.Policies()
	next := Upserted(old, p, remove)
	if len(next) > MaxPolicies {
		return ErrTooManyPolicies
	}
	// Кэш обновляется до применения: параллельный Beat watchdog'а строит правила
	// уже из нового списка, а не из устаревшего снимка.
	s.setPolicies(next)
	if _, err := s.wd.Reapply(); err != nil {
		s.rollback(old)
		return fmt.Errorf("gateway: правила не применены, политика не сохранена: %w", err)
	}
	if err := s.repo.Save(next); err != nil {
		s.rollback(old)
		s.setErr(err)
		return fmt.Errorf("gateway: политика не сохранена: %w", err)
	}
	return nil
}

func (s *Service) setPolicies(list []DevicePolicy) {
	s.polMu.Lock()
	s.policies = list
	s.polMu.Unlock()
}

// rollback возвращает прежний список политик и прежние правила (ошибка
// повторного применения уже записана в статус рекордером).
func (s *Service) rollback(old []DevicePolicy) {
	s.setPolicies(old)
	_, _ = s.wd.Reapply()
}
