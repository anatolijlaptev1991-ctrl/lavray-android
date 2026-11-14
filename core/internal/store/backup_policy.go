// SPDX-License-Identifier: GPL-3.0-only

package store

// backup_policy.go — что НЕ входит в резервную копию и что восстановление
// оставляет как есть (T-83). Всё остальное в таблицах sqlite_master копируется
// без правок кода: таблица, добавленная новой миграцией, попадает в копию и в
// восстановление сама (TestRestoreCoversEveryTable).

import (
	"net/url"
	"path/filepath"
	"strings"
)

// MetaCustomPresets — ключ meta с пользовательскими наборами маршрутов (JSON
// массив). Должен совпадать с api.metaCustomPresets: это проверяет тест API.
const MetaCustomPresets = "routing.custom_presets"

// MetaCachePrefix — префикс ключей meta, которые являются кэшами (их можно
// получить заново из сети или диска): в копию не входят, при восстановлении не
// трогаются.
const MetaCachePrefix = "cache."

// backupExcluded — по таблицам: условие SQL над строками, которых нет в копии.
// При восстановлении такие строки получателя остаются, а такие же строки из
// чужого файла игнорируются. Состояние обновлений (update.*) обязано
// пережить восстановление: иначе копия, снятая до обновления, откатила бы
// serial и сняла защиту от downgrade (T-75, T-83).
var backupExcluded = map[string]string{
	"settings": `key LIKE 'update.%'`,
	"meta":     `key LIKE '` + MetaCachePrefix + `%'`,
}

// backupSkipTables — таблицы, которые восстановление не переносит.
// schema_version описывает физическую схему файла получателя, а не данные
// пользователя: копия до этого приводится к той же версии миграциями.
var backupSkipTables = map[string]bool{"schema_version": true}

// excludedWhere — «WHERE NOT (…)» для таблицы либо пустая строка.
func excludedWhere(table string) string {
	if cond, ok := backupExcluded[table]; ok {
		return ` WHERE NOT (` + cond + `)`
	}
	return ""
}

// plainDSN — URL-имя файла для sql.Open без прагм Open (экранирование пути
// как в Open: «#» и «?» в каталоге данных не ломают DSN).
func plainDSN(path string) string {
	p := filepath.ToSlash(path)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p // Windows: file:///C:/...
	}
	u := url.URL{Scheme: "file", Path: p}
	return u.String()
}

// quoteIdent — идентификатор SQLite в двойных кавычках.
func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
