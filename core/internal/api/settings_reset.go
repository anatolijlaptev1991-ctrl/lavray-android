// SPDX-License-Identifier: GPL-3.0-only

package api

// settings_reset.go — ResetSettings: сброс настроек к умолчанию (хвосты A).
// Сброс — это удаление записи: значение тогда берётся из дескриптора.

import (
	"context"
	"sort"
	"strings"

	"connectrpc.com/connect"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

// isInternalSetting: ключ сбросу не подлежит — служебный ключ ядра или состояние
// обновлений (update.*, им управляет только подсистема обновлений).
func isInternalSetting(key string) bool {
	return descriptorMap[key].Internal || isUpdateKey(key)
}

// resetKeys — ключи к сбросу: перечисленные в запросе либо все допустимые
// (пустой список). Неизвестный или служебный ключ — INVALID_ARGUMENT, запрос
// отвергается целиком.
func resetKeys(requested []string) ([]string, error) {
	if len(requested) == 0 {
		var all []string
		for _, d := range model.SettingDescriptors() {
			if !isInternalSetting(d.Key) {
				all = append(all, d.Key)
			}
		}
		sort.Strings(all)
		return all, nil
	}
	seen := map[string]bool{}
	var keys, unknown, internal []string
	for _, k := range requested {
		if seen[k] {
			continue
		}
		seen[k] = true
		switch _, known := descriptorMap[k]; {
		case !known:
			unknown = append(unknown, k)
		case isInternalSetting(k):
			internal = append(internal, k)
		default:
			keys = append(keys, k)
		}
	}
	if len(unknown) > 0 {
		return nil, invalidArg("неизвестные ключи настроек: %s", strings.Join(unknown, ", "))
	}
	if len(internal) > 0 {
		return nil, invalidArg("служебные ключи сбросить нельзя: %s", strings.Join(internal, ", "))
	}
	sort.Strings(keys)
	return keys, nil
}

// ResetSettings возвращает настройки к умолчанию: перечисленные ключи или все
// (пустой список), кроме служебных и update.*. Ответ — как у SetSettings:
// accepted_keys и флаги перезапуска (только для ключей, чьё значение реально
// изменилось). Подписчики SubscribeSettings получают сброшенные ключи в removed.
func (s *Server) ResetSettings(ctx context.Context, req *connect.Request[lavrayv1.ResetSettingsRequest]) (*connect.Response[lavrayv1.SetSettingsResponse], error) {
	keys, err := resetKeys(req.Msg.Keys)
	if err != nil {
		return nil, err
	}
	stored, err := s.st.Settings()
	if err != nil {
		return nil, mapErr(err)
	}
	deleted, err := s.st.DeleteSettings(keys)
	if err != nil {
		return nil, mapErr(err)
	}
	resp := &lavrayv1.SetSettingsResponse{AcceptedKeys: keys}
	for _, key := range deleted {
		d := descriptorMap[key]
		if stored[key] == d.DefaultValue {
			continue // значение осталось тем же: перезапуск не нужен
		}
		if d.RequiresProfileRestart && s.inst.Status().State != instStateIdle {
			resp.NeedsProfileRestart = true
		}
		resp.NeedsAppRestart = resp.NeedsAppRestart || d.RequiresAppRestart
	}
	s.publishSettingsChanged(&lavrayv1.SettingsChanged{Removed: deleted})
	return connect.NewResponse(resp), nil
}
