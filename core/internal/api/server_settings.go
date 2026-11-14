// SPDX-License-Identifier: GPL-3.0-only

package api

import (
	"encoding/json"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

// ============================================================================
// Хелперы настроек (таблица settings + дескрипторы model.SettingDescriptors)
// ============================================================================

// descriptorMap — каталог ключей (собирается один раз).
var descriptorMap = func() map[string]model.SettingDescriptor {
	m := map[string]model.SettingDescriptor{}
	for _, d := range model.SettingDescriptors() {
		m[d.Key] = d
	}
	return m
}()

// settingJSON — значение ключа из таблицы settings или дефолт дескриптора
// ("" если ключа нет вообще — такого не бывает для известных ключей).
func (s *Server) settingJSON(key string) string {
	if s.onSettingRead != nil {
		s.onSettingRead(key)
	}
	if v, ok, err := s.st.GetSetting(key); err == nil && ok {
		return v
	}
	if d, found := descriptorMap[key]; found {
		return d.DefaultValue
	}
	return ""
}

func (s *Server) settingStr(key string) string {
	var v string
	if json.Unmarshal([]byte(s.settingJSON(key)), &v) == nil {
		return v
	}
	return ""
}

func (s *Server) settingInt(key string) int {
	var v int
	if json.Unmarshal([]byte(s.settingJSON(key)), &v) == nil {
		return v
	}
	return 0
}

func (s *Server) settingBool(key string) bool {
	var v bool
	if json.Unmarshal([]byte(s.settingJSON(key)), &v) == nil {
		return v
	}
	return false
}

// settingStringList — список строк (remember.modes, ui.log_ignore).
func (s *Server) settingStringList(key string) []string {
	var v []string
	if json.Unmarshal([]byte(s.settingJSON(key)), &v) == nil {
		return v
	}
	return nil
}
