// SPDX-License-Identifier: GPL-3.0-only

package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"sort"
)

// Snapshot пишет в dst согласованную копию базы без исключённых данных
// (backup_policy.go). Файл dst не должен существовать.
//
// VACUUM INTO — одна транзакция чтения: копия видит либо всё, либо ничего из
// любой параллельной записи. Затем из файла удаляется исключённое и файл
// пересобирается VACUUM, чтобы удалённые строки не остались в свободных
// страницах. Живая база не меняется. При ошибке dst удаляется.
func (s *Store) Snapshot(ctx context.Context, dst string) (err error) {
	if s.tx != nil {
		return errors.New("store: Snapshot недоступен внутри InTx")
	}
	if _, statErr := os.Stat(dst); statErr == nil {
		return fmt.Errorf("store: файл копии уже существует: %s", dst)
	}
	defer func() {
		if err != nil {
			removeDBFiles(dst)
		}
	}()
	if _, err = s.db.ExecContext(ctx, `VACUUM INTO ?`, dst); err != nil {
		return fmt.Errorf("store: снимок БД: %w", err)
	}
	return stripExcluded(ctx, dst)
}

// stripExcluded удаляет из файла копии строки по backupExcluded и сжимает файл.
func stripExcluded(ctx context.Context, path string) error {
	db, err := sql.Open("sqlite", plainDSN(path))
	if err != nil {
		return fmt.Errorf("store: открытие копии: %w", err)
	}
	defer func() { _ = db.Close() }()
	db.SetMaxOpenConns(1)
	// режим без WAL: копия обязана быть одним самодостаточным файлом
	if _, err := db.ExecContext(ctx, `PRAGMA journal_mode = DELETE`); err != nil {
		return fmt.Errorf("store: режим журнала копии: %w", err)
	}
	tables := make([]string, 0, len(backupExcluded))
	for table := range backupExcluded {
		tables = append(tables, table)
	}
	sort.Strings(tables)
	for _, table := range tables {
		var n int
		err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&n)
		if err != nil {
			return fmt.Errorf("store: поиск таблицы %s: %w", table, err)
		}
		if n == 0 {
			continue
		}
		if _, err := db.ExecContext(ctx, `DELETE FROM `+quoteIdent(table)+` WHERE `+backupExcluded[table]); err != nil {
			return fmt.Errorf("store: очистка %s в копии: %w", table, err)
		}
	}
	if _, err := db.ExecContext(ctx, `VACUUM`); err != nil {
		return fmt.Errorf("store: VACUUM копии: %w", err)
	}
	return nil
}

// removeDBFiles убирает файл БД и возможные спутники (при ошибках — молча).
func removeDBFiles(path string) {
	for _, suffix := range []string{"", "-journal", "-wal", "-shm"} {
		_ = os.Remove(path + suffix)
	}
}
