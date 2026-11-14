// SPDX-License-Identifier: GPL-3.0-only

package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
)

// afterTableRestored вызывается после переноса каждой таблицы; тест подставляет
// сбой, чтобы проверить откат «посреди» восстановления.
var afterTableRestored = func(table string) error { return nil }

// detachCopy отключает копию от соединения; тест подставляет сбой.
var detachCopy = func(ctx context.Context, conn *sql.Conn) error {
	_, err := conn.ExecContext(ctx, `DETACH DATABASE bk`)
	return err
}

// Restore заменяет ВСЕ данные хранилища содержимым файла копии src (снятого
// Snapshot, возможно более старой версией схемы).
//
//  1. Копия проверяется и поднимается миграциями до текущей версии во
//     временной базе (withCopy): мусор, чужая SQLite и копия «из будущего»
//     отвергаются до любых изменений.
//  2. Копия подключается ATTACH к единственному соединению хранилища; в одной
//     транзакции BEGIN IMMEDIATE для каждой таблицы из sqlite_master
//     сначала очищаются ВСЕ таблицы (потомки раньше родителей), затем
//     вставляются строки по общим колонкам (родители раньше потомков).
//     Так каскад ON DELETE не может снести уже перенесённые строки, в каком
//     бы порядке ни стояли имена. Таблицы, добавленные будущими миграциями,
//     переносятся без правок этого кода.
//  3. Исключённые строки (backupExcluded, в первую очередь update.*) остаются
//     такими, какими были у получателя.
//  4. Внешние ключи проверяются до COMMIT; любая ошибка откатывает всё.
func (s *Store) Restore(ctx context.Context, src string) error {
	if s.tx != nil {
		return errors.New("store: Restore недоступен внутри InTx")
	}
	return s.withCopy(ctx, src, func(c *Store, path string, _ int64) error {
		// копия должна быть тихим файлом: Close сбрасывает WAL в основной файл
		if err := c.Close(); err != nil {
			return fmt.Errorf("store: закрытие копии: %w", err)
		}
		return s.replaceFrom(ctx, path)
	})
}

// replaceFrom — шаги 2–4 Restore над файлом поднятой копии.
func (s *Store) replaceFrom(ctx context.Context, copyPath string) error {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("store: соединение для восстановления: %w", err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.ExecContext(ctx, `ATTACH DATABASE ? AS bk`, copyPath); err != nil {
		return fmt.Errorf("store: подключение копии: %w", err)
	}
	// откат и отключение не зависят от отмены ctx: иначе копия осталась бы
	// подключённой к соединению, а транзакция — открытой
	cleanup := context.WithoutCancel(ctx)
	inTx := false
	defer func() {
		if inTx {
			_, _ = conn.ExecContext(cleanup, `ROLLBACK`)
		}
		s.detach(cleanup, conn)
	}()

	tables, err := orderedTables(ctx, conn)
	if err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return fmt.Errorf("store: начало восстановления: %w", err)
	}
	inTx = true
	if _, err := conn.ExecContext(ctx, `PRAGMA defer_foreign_keys = ON`); err != nil {
		return fmt.Errorf("store: defer_foreign_keys: %w", err)
	}
	if err := replaceTables(ctx, conn, tables); err != nil {
		return err
	}
	if err := copySequences(ctx, conn); err != nil {
		return err
	}
	if err := checkForeignKeys(ctx, conn); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return fmt.Errorf("store: фиксация восстановления: %w", err)
	}
	inTx = false
	return nil
}

// orderedTables — таблицы данных живой базы: родители раньше потомков.
func orderedTables(ctx context.Context, conn *sql.Conn) ([]string, error) {
	tables, err := restoreTables(ctx, conn)
	if err != nil {
		return nil, err
	}
	parents, err := tableParents(ctx, conn, tables)
	if err != nil {
		return nil, err
	}
	return sortTablesParentsFirst(tables, parents), nil
}

// replaceTables сначала очищает все таблицы (потомки раньше родителей), затем
// заполняет (родители раньше потомков). Пока идут вставки, ни одного DELETE уже
// нет, и каскад не может тронуть перенесённые строки.
func replaceTables(ctx context.Context, conn *sql.Conn, tables []string) error {
	for i := len(tables) - 1; i >= 0; i-- {
		if err := clearTable(ctx, conn, tables[i]); err != nil {
			return err
		}
	}
	for _, table := range tables {
		if err := fillTable(ctx, conn, table); err != nil {
			return err
		}
		if err := afterTableRestored(table); err != nil {
			return err
		}
	}
	return nil
}

// detach отключает копию. Сбой — предупреждение, а не ошибка операции: после
// COMMIT данные уже заменены, и сообщать «данные не изменены» было бы ложью.
// Соединение с прицепленной копией выбрасывается из пула, чтобы следующее
// восстановление не упёрлось в занятое имя bk.
func (s *Store) detach(ctx context.Context, conn *sql.Conn) {
	if err := detachCopy(ctx, conn); err != nil {
		s.warnf("store: копия не отключена от соединения, соединение закрыто: %v", err)
		_ = conn.Raw(func(any) error { return driver.ErrBadConn })
	}
}
