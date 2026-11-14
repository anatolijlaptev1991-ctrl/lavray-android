// SPDX-License-Identifier: GPL-3.0-only

// subscription_hwid.go — постоянный HWID установки для заголовка x-hwid
// (D-02, FR-UI-265).
//
// Поведение по умолчанию не меняется: sub.hwid_enabled=false, и тогда запрос
// подписки не содержит x-hwid, x-device-os, x-ver-os, x-device-model, а сам
// идентификатор не создаётся. При включении ID генерируется ОДИН раз и хранится
// в служебной настройке sub.hwid_id; раньше subs.NewSubscriber создавал новый
// случайный ID на каждое обновление, и провайдер видел каждое обновление как
// новое устройство, быстро выбивая лимит устройств.
package api

import (
	"encoding/json"
	"strings"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/subs"
)

// deviceHWID отдаёт сохранённый HWID установки, создавая и сохраняя его при
// первом обращении. false — ID не удалось ни прочитать, ни сохранить: тогда
// заголовки HWID не отправляются (случайный несохранённый ID выдал бы каждый
// запрос за новое устройство, это хуже, чем не отправлять ничего).
func (s *Server) deviceHWID() (subs.HWID, bool) {
	s.hwidMu.Lock()
	defer s.hwidMu.Unlock()

	raw, found, err := s.st.GetSetting(model.SettingHWIDID)
	if err != nil {
		s.logs.Logf("warn", "subs", "HWID: не удалось прочитать %s: %v; заголовки HWID не отправлены", model.SettingHWIDID, err)
		return subs.HWID{}, false
	}
	if found {
		var id string
		if json.Unmarshal([]byte(raw), &id) == nil && strings.TrimSpace(id) != "" {
			return subs.HWIDWithID(id), true
		}
		// Запись есть, но повреждена: создаём новый ID и перезаписываем её.
	}
	h := subs.NewHWID()
	encoded, err := json.Marshal(h.ID)
	if err == nil {
		err = s.st.SetSetting(model.SettingHWIDID, string(encoded))
	}
	if err != nil {
		s.logs.Logf("warn", "subs", "HWID: не удалось сохранить %s: %v; заголовки HWID не отправлены", model.SettingHWIDID, err)
		return subs.HWID{}, false
	}
	return h, true
}
