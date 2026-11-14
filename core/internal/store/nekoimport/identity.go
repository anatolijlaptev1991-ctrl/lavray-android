// SPDX-License-Identifier: GPL-3.0-only

package nekoimport

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
)

// identity.go — ключ идемпотентности переноса: по нему import_map узнаёт, что
// запись NekoBox уже перенесена.

// SourceNekoBox — значение колонки import_map.source.
const SourceNekoBox = "nekobox"

// Виды записей import_map (колонка kind).
const (
	KindGroup   = "group"
	KindProfile = "profile"
	KindPreset  = "preset"
)

// SourceKey — hex(sha256(neko_id|type|address)). Тот же id с другим типом или
// адресом — другая запись (NekoBox мог выдать освободившийся id заново), поэтому
// она переносится как новая. Для группы type = "group", address = ссылка
// подписки; для маршрута id = 0, type = "preset", address = имя и хэш содержимого.
func SourceKey(nekoID int64, typ, address string) string {
	sum := sha256.Sum256([]byte(strconv.FormatInt(nekoID, 10) + "|" + typ + "|" + address))
	return hex.EncodeToString(sum[:])
}
