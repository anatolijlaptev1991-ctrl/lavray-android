// SPDX-License-Identifier: GPL-3.0-only

// update_store.go — состояние обновлений в таблице settings. Все ключи
// служебные (model.SettingDescriptor.Internal): клиент не может ни прочитать, ни
// записать их через SettingsService, и они не попадают в резервную копию.
package api

import (
	"encoding/json"
	"strconv"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/updates"
)

// Ключи — строками (файл читателя настроек, TestEverySettingHasReader); их
// совпадение с model.SettingUpdate* проверяет TestUpdateSettingKeysMatchModel.
const (
	setUpdRoot     = "update.root_version"
	setUpdSerialSt = "update.serial_stable"
	setUpdSerialBe = "update.serial_beta"
	setUpdChecked  = "update.last_checked_at"
	setUpdLastURL  = "update.last_url"
	setUpdTSVer    = "update.timestamp_version"
)

func (s *Server) loadUpdateState() updates.State {
	st := updates.State{}
	get := func(k string, dst *int64) {
		if v, ok, _ := s.st.GetSetting(k); ok {
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				*dst = n
			}
		}
	}
	get(setUpdRoot, &st.RootVersion)
	get(setUpdSerialSt, &st.SerialStable)
	get(setUpdSerialBe, &st.SerialBeta)
	get(setUpdTSVer, &st.TimestampVersion)
	get(setUpdChecked, &st.LastCheckedAt)
	if v, ok, _ := s.st.GetSetting(setUpdLastURL); ok {
		var url string
		if json.Unmarshal([]byte(v), &url) == nil {
			st.LastWorkingURL = url
		}
	}
	return st
}

// saveUpdateState пишет состояние. Монотонные счётчики не уменьшаются даже при
// гонке двух проверок: записывается максимум из сохранённого и нового. Исключение —
// сброс после ротации ключей роли (State.ResetTimestamp/ResetSerials): тогда пишется
// новое значение как есть, иначе утёкший ключ, подписавший MaxInt64, заперл бы
// обновления навсегда.
func (s *Server) saveUpdateState(st *updates.State) {
	cur := s.loadUpdateState()
	setNum := func(k string, v int64) { _ = s.st.SetSetting(k, strconv.FormatInt(v, 10)) }
	pick := func(reset bool, saved, now int64) int64 {
		if reset {
			return now
		}
		return max(saved, now)
	}
	setNum(setUpdRoot, max(cur.RootVersion, st.RootVersion))
	setNum(setUpdSerialSt, pick(st.ResetSerials, cur.SerialStable, st.SerialStable))
	setNum(setUpdSerialBe, pick(st.ResetSerials, cur.SerialBeta, st.SerialBeta))
	setNum(setUpdTSVer, pick(st.ResetTimestamp, cur.TimestampVersion, st.TimestampVersion))
	setNum(setUpdChecked, st.LastCheckedAt)
	b, _ := json.Marshal(st.LastWorkingURL)
	_ = s.st.SetSetting(setUpdLastURL, string(b))
	st.ResetTimestamp, st.ResetSerials = false, false
}

// rootStore — цепочка принятых корней на диске (<данные>/updates/roots). Каталог
// не обязан быть доверенным: цепочка перепроверяется от вшитого корня.
func (s *Server) rootStore() updates.DirRootStore {
	return updates.DirRootStore{Dir: s.rootsDir()}
}
