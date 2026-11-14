// SPDX-License-Identifier: GPL-3.0-only

package gateway

// policies_repo.go — хранение политик устройств в настройках ядра (ключ
// gateway.device_policies, JSON-список). В список SettingsService ключ не входит:
// он без дескриптора и наружу идёт только через GatewayService.

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

// SettingPolicies — ключ таблицы settings со списком политик.
const SettingPolicies = "gateway.device_policies"

// MaxPolicies — наибольшее число политик (карта ограничена по размеру).
const MaxPolicies = 512

// ErrTooManyPolicies — достигнут MaxPolicies.
var ErrTooManyPolicies = errors.New("gateway: слишком много политик устройств")

// ErrBadPolicy — политика не проходит проверку.
var ErrBadPolicy = errors.New("gateway: недопустимая политика устройства")

// KV — хранилище настроек (его реализует store.Store).
type KV interface {
	GetSetting(key string) (string, bool, error)
	SetSetting(key, value string) error
}

// PolicyRepo хранит политики. Сериализацию вызовов обеспечивает Service.
type PolicyRepo struct{ kv KV }

// NewPolicyRepo создаёт хранилище поверх kv.
func NewPolicyRepo(kv KV) *PolicyRepo { return &PolicyRepo{kv: kv} }

type storedPolicy struct {
	MAC      string `json:"mac"`
	Policy   string `json:"policy"`
	PresetID string `json:"preset_id,omitempty"`
}

// List возвращает политики, отсортированные по MAC. Испорченные записи не
// скрываются: ошибка разбора возвращается, чтобы правила не строились из
// частично прочитанного списка.
func (r *PolicyRepo) List() ([]DevicePolicy, error) {
	raw, ok, err := r.kv.GetSetting(SettingPolicies)
	if err != nil || !ok || raw == "" {
		return nil, err
	}
	var stored []storedPolicy
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		return nil, fmt.Errorf("gateway: список политик повреждён: %w", err)
	}
	out := make([]DevicePolicy, 0, len(stored))
	for _, s := range stored {
		mac, err := NormalizeMAC(s.MAC)
		if err != nil || !validAction(s.Policy) {
			return nil, fmt.Errorf("gateway: запись политики %q повреждена", shorten(s.MAC))
		}
		out = append(out, DevicePolicy{MAC: mac, Policy: s.Policy, PresetID: s.PresetID})
	}
	return out, nil
}

// Save записывает список целиком (после проверки).
func (r *PolicyRepo) Save(list []DevicePolicy) error {
	if len(list) > MaxPolicies {
		return ErrTooManyPolicies
	}
	sorted := append([]DevicePolicy(nil), list...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].MAC < sorted[j].MAC })
	stored := make([]storedPolicy, len(sorted))
	for i, p := range sorted {
		stored[i] = storedPolicy{MAC: p.MAC, Policy: p.Policy, PresetID: p.PresetID}
	}
	raw, err := json.Marshal(stored)
	if err != nil {
		return err
	}
	return r.kv.SetSetting(SettingPolicies, string(raw))
}

func validAction(a string) bool {
	return a == PolicyProxy || a == PolicyDirect || a == PolicyPreset
}

// Upserted возвращает список с политикой p (заменяет запись того же MAC) либо
// без неё, если remove. Исходный список не меняется.
func Upserted(list []DevicePolicy, p DevicePolicy, remove bool) []DevicePolicy {
	out := make([]DevicePolicy, 0, len(list)+1)
	for _, e := range list {
		if e.MAC != p.MAC {
			out = append(out, e)
		}
	}
	if !remove {
		out = append(out, p)
	}
	return out
}
