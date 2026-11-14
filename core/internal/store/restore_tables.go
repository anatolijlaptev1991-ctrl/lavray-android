// SPDX-License-Identifier: GPL-3.0-only

package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// restoreTables — таблицы данных живой базы (main) в порядке имён; служебные
// sqlite_* и backupSkipTables не входят.
func restoreTables(ctx context.Context, conn *sql.Conn) ([]string, error) {
	rows, err := conn.QueryContext(ctx,
		`SELECT name FROM main.sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite\_%' ESCAPE '\' ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("store: список таблиц: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		if !backupSkipTables[name] {
			out = append(out, name)
		}
	}
	return out, rows.Err()
}

// tableColumns — колонки таблицы в схеме schema ("main" или "bk"); пусто —
// таблицы нет. Генерируемые колонки в table_info не попадают.
func tableColumns(ctx context.Context, conn *sql.Conn, schema, table string) ([]string, error) {
	rows, err := conn.QueryContext(ctx, `PRAGMA `+schema+`.table_info(`+quoteIdent(table)+`)`)
	if err != nil {
		return nil, fmt.Errorf("store: колонки %s.%s: %w", schema, table, err)
	}
	defer func() { _ = rows.Close() }()
	var cols []string
	for rows.Next() {
		var (
			cid, notnull, pk int
			name, typ        string
			dflt             sql.NullString
		)
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			return nil, err
		}
		cols = append(cols, name)
	}
	return cols, rows.Err()
}

// commonColumns — колонки обеих баз в порядке живой таблицы.
func commonColumns(live, backup []string) []string {
	inBackup := make(map[string]bool, len(backup))
	for _, c := range backup {
		inBackup[c] = true
	}
	var out []string
	for _, c := range live {
		if inBackup[c] {
			out = append(out, quoteIdent(c))
		}
	}
	return out
}

// clearTable удаляет строки таблицы, кроме исключённых политикой.
func clearTable(ctx context.Context, conn *sql.Conn, table string) error {
	if _, err := conn.ExecContext(ctx, `DELETE FROM main.`+quoteIdent(table)+excludedWhere(table)); err != nil {
		return fmt.Errorf("store: очистка %s: %w", table, err)
	}
	return nil
}

// fillTable вставляет в таблицу строки копии по общим колонкам, кроме
// исключённых политикой.
func fillTable(ctx context.Context, conn *sql.Conn, table string) error {
	live, err := tableColumns(ctx, conn, "main", table)
	if err != nil {
		return err
	}
	backup, err := tableColumns(ctx, conn, "bk", table)
	if err != nil {
		return err
	}
	cols := commonColumns(live, backup)
	if len(cols) == 0 {
		return fmt.Errorf("store: в копии нет таблицы %s", table)
	}
	name, list := quoteIdent(table), strings.Join(cols, ", ")
	if _, err := conn.ExecContext(ctx,
		`INSERT INTO main.`+name+` (`+list+`) SELECT `+list+` FROM bk.`+name+excludedWhere(table)); err != nil {
		return fmt.Errorf("store: перенос %s: %w", table, err)
	}
	return nil
}

// copySequences переносит счётчики AUTOINCREMENT (sqlite_sequence), если они
// есть: следующий id продолжается от значения копии, а не от прежнего.
func copySequences(ctx context.Context, conn *sql.Conn) error {
	has := func(schema string) (bool, error) {
		var n int
		err := conn.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM `+schema+`.sqlite_master WHERE name = 'sqlite_sequence'`).Scan(&n)
		return n > 0, err
	}
	live, err := has("main")
	if err != nil || !live {
		return err
	}
	if _, err := conn.ExecContext(ctx, `DELETE FROM main.sqlite_sequence`); err != nil {
		return fmt.Errorf("store: очистка sqlite_sequence: %w", err)
	}
	backup, err := has("bk")
	if err != nil || !backup {
		return err
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO main.sqlite_sequence (name, seq)
		SELECT name, seq FROM bk.sqlite_sequence
		WHERE name IN (SELECT name FROM main.sqlite_master WHERE type = 'table')`); err != nil {
		return fmt.Errorf("store: перенос sqlite_sequence: %w", err)
	}
	return nil
}

// checkForeignKeys: после переноса не должно остаться висячих ссылок.
func checkForeignKeys(ctx context.Context, conn *sql.Conn) error {
	rows, err := conn.QueryContext(ctx, `PRAGMA main.foreign_key_check`)
	if err != nil {
		return fmt.Errorf("store: foreign_key_check: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if rows.Next() {
		var (
			table, parent string
			rowid, fkid   sql.NullInt64
		)
		if err := rows.Scan(&table, &rowid, &parent, &fkid); err != nil {
			return err
		}
		return fmt.Errorf("%w: после восстановления в %s есть ссылка на отсутствующую запись %s", ErrConstraint, table, parent)
	}
	return rows.Err()
}
