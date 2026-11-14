// SPDX-License-Identifier: GPL-3.0-only

package store

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
)

// tableParents — для каждой таблицы из tables её родительские таблицы по
// внешним ключам (PRAGMA foreign_key_list). Ссылки на себя и на таблицы вне
// списка (sqlite_*, schema_version) не учитываются.
func tableParents(ctx context.Context, conn *sql.Conn, tables []string) (map[string][]string, error) {
	inSet := make(map[string]bool, len(tables))
	for _, name := range tables {
		inSet[name] = true
	}
	out := make(map[string][]string, len(tables))
	for _, table := range tables {
		rows, err := conn.QueryContext(ctx, `PRAGMA main.foreign_key_list(`+quoteIdent(table)+`)`)
		if err != nil {
			return nil, fmt.Errorf("store: внешние ключи %s: %w", table, err)
		}
		for rows.Next() {
			var (
				id, seq                         int
				parent, from                    string
				to, onUpdate, onDelete, matchBy sql.NullString
			)
			if err := rows.Scan(&id, &seq, &parent, &from, &to, &onUpdate, &onDelete, &matchBy); err != nil {
				_ = rows.Close()
				return nil, err
			}
			if parent != table && inSet[parent] {
				out[table] = append(out[table], parent)
			}
		}
		err = rows.Err()
		_ = rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// sortTablesParentsFirst упорядочивает таблицы так, чтобы родитель стоял раньше
// потомка; на одном уровне — по имени. В цикле ссылок (groups ↔ profiles)
// первой ставится таблица, чьё имя меньше, после чего зависящие от неё
// освобождаются. Родители вне списка и ссылки на себя игнорируются. Результат
// детерминирован. Корректность переноса от порядка не зависит (сначала
// очищаются все таблицы, потом заполняются), порядок делает его предсказуемым.
func sortTablesParentsFirst(tables []string, parents map[string][]string) []string {
	inSet := make(map[string]bool, len(tables))
	for _, name := range tables {
		inSet[name] = true
	}
	rest := append([]string(nil), tables...)
	sort.Strings(rest)
	placed := make(map[string]bool, len(rest))
	out := make([]string, 0, len(rest))
	for len(rest) > 0 {
		var next, blocked []string
		for _, table := range rest {
			ready := true
			for _, p := range parents[table] {
				if inSet[p] && p != table && !placed[p] {
					ready = false
					break
				}
			}
			if ready {
				next = append(next, table)
			} else {
				blocked = append(blocked, table)
			}
		}
		if len(next) == 0 { // цикл: разрываем его по имени
			next, blocked = blocked[:1], blocked[1:]
		}
		for _, table := range next {
			placed[table] = true
		}
		out = append(out, next...)
		rest = blocked
	}
	return out
}
