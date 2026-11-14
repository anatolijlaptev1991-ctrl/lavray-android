// SPDX-License-Identifier: GPL-3.0-only

// settings_update.go — служебные настройки подсистемы обновлений (T-75).
package model

// Служебные ключи подсистемы обновлений (T-75): хранят принятый корень доверия и
// монотонные счётчики защиты от отката метаданных. SetSettings их не принимает,
// GetSettings не отдаёт, резервная копия исключает.
const (
	SettingUpdateRootJSON         = "update.root_json"
	SettingUpdateRootVersion      = "update.root_version"
	SettingUpdateTimestampVersion = "update.timestamp_version"
	SettingUpdateSerialStable     = "update.serial_stable"
	SettingUpdateSerialBeta       = "update.serial_beta"
	SettingUpdateLastChecked      = "update.last_checked_at"
	SettingUpdateLastURL          = "update.last_url"
)

// updateInternalKeys — служебные ключи обновлений (все Internal + BackupExclude).
var updateInternalKeys = []string{
	SettingUpdateRootJSON, SettingUpdateRootVersion, SettingUpdateTimestampVersion,
	SettingUpdateSerialStable, SettingUpdateSerialBeta, SettingUpdateLastChecked, SettingUpdateLastURL,
}

// BackupExcluded — ключ настроек не входит в резервную копию.
func BackupExcluded(key string) bool {
	for _, d := range SettingDescriptors() {
		if d.Key == key {
			return d.BackupExclude
		}
	}
	return false
}

// updateInternalDescriptors — служебные ключи обновлений (T-75).
func updateInternalDescriptors() []SettingDescriptor {
	out := make([]SettingDescriptor, 0, len(updateInternalKeys))
	for _, k := range updateInternalKeys {
		t := SettingInt
		if k == SettingUpdateRootJSON || k == SettingUpdateLastURL {
			t = SettingString
		}
		out = append(out, SettingDescriptor{Key: k, Type: t, GroupID: "update", Internal: true, BackupExclude: true})
	}
	return out
}
