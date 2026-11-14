// SPDX-License-Identifier: GPL-3.0-only

package store

// importMapSchemaVersion — номер миграции таблицы import_map (T-78). Номера
// выдаёт порядок слияния веток: если до этой ветки в main попала другая
// миграция, меняются две вещи — эта константа и номер в строке
// testdata/migrations.sha256 (хэш текста не меняется).
const importMapSchemaVersion = 6

// importMapSchema — миграция import_map: что уже перенесено из внешнего
// источника (NekoBox) и под каким локальным id. Повторный перенос по этой
// таблице не создаёт дублей; удалённая локальная копия переносится заново.
var importMapSchema = []string{
	`CREATE TABLE import_map (source TEXT NOT NULL, kind TEXT NOT NULL CHECK(kind IN ('group','profile','preset')),
  source_key TEXT NOT NULL, local_id TEXT NOT NULL, imported_at_ms INTEGER NOT NULL, PRIMARY KEY(source, kind, source_key))`,
}
