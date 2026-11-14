// SPDX-License-Identifier: GPL-3.0-only

// Package store — SQLite-хранилище LAVRay (modernc.org/sqlite, CGO-free;
// core-port-plan §5). Режим WAL; одна операция UI — одна транзакция.
//
// Права (Р2-33): каталог данных 0700, lavray.db (и -wal/-shm) 0600 —
// устанавливаются при создании; на Windows файловые права не применяются.
package store

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	_ "modernc.org/sqlite" // регистрация драйвера (CGO_ENABLED=0)
)

const dbFileName = "lavray.db"

// DefaultGroupID — резервный id встроенной группы «Default»: маленькие
// номера (0,1,2…) принадлежат импорту NekoBox, поэтому встроенная группа
// живёт в верхнем диапазоне и не конфликтует ни с одним реальным каталогом.
const DefaultGroupID int64 = 2147483000

// Ошибки хранилища (сопоставляются ошибкам ConnectRPC на слое API, W6).
var (
	ErrNotFound   = fmt.Errorf("store: not found")
	ErrLastGroup  = fmt.Errorf("store: cannot delete the last group") // FR-UI-107
	ErrConstraint = fmt.Errorf("store: constraint violation")
)

// Store — обёртка над SQLite-базой в каталоге dir.
type Store struct {
	db  *sql.DB
	dir string
	// tx != nil только у транзакционного представления внутри InTx (intx.go):
	// withTx тогда не открывает свою транзакцию, а работает в этой.
	tx *sql.Tx
	// warn — приёмник предупреждений (SetWarnLogger); nil — молча.
	warn func(format string, args ...any)
}

// Open открывает (или создаёт) хранилище в dir, применяет права и миграции.
func Open(dir string) (*Store, error) {
	if dir == "" {
		return nil, fmt.Errorf("store: empty dir")
	}
	// Относительный путь — от текущего каталога: ниже путь становится
	// file:///C:/…, и без Abs он уходил бы в корень диска.
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("store: abs %q: %w", dir, err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("store: mkdir: %w", err)
	}
	s := &Store{dir: dir}

	// foreign_keys/journal_mode должны действовать на КАЖДОМ соединении
	// пула — задаём их в DSN (modernc.org/sqlite: _pragma=...).
	// L17 аудита №1: путь экранируется как URL. Раньше конкатенация строк
	// ломалась на «#»/«?» в пути (%APPDATA%\LAVRay#1): «#» обрезал DSN, и БД
	// создавалась в другом каталоге (split-brain между запусками).
	dbPath := filepath.ToSlash(filepath.Join(dir, dbFileName))
	if !strings.HasPrefix(dbPath, "/") {
		dbPath = "/" + dbPath // Windows: file:///C:/...
	}
	u := url.URL{Scheme: "file", Path: dbPath}
	dsn := u.String() +
		"?_pragma=foreign_keys(1)" +
		"&_pragma=journal_mode(WAL)" +
		"&_pragma=synchronous(NORMAL)" +
		"&_pragma=busy_timeout(10000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open: %w", err)
	}
	// Ядро — однопользовательский процесс: одно соединение исключает
	// SQLITE_BUSY между собственными горутинами (запись всё равно одна).
	db.SetMaxOpenConns(1)
	s.db = db

	// T-50: целостность — ДО миграций (на повреждённой базе миграция ничего
	// не пишет); файл не переименовывается и не удаляется (integrity.go).
	if err := checkIntegrity(db, DBPath(dir)); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := migrate(db); err != nil {
		_ = db.Close()
		return nil, openError(DBPath(dir), err)
	}
	if err := s.applyFileModes(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := s.settleDefaultPreset(); err != nil {
		_ = db.Close()
		return nil, err
	}
	// T-79: процесс убили в середине сессии — закрываем её по последнему сбросу.
	if err := s.closeDanglingOnOpen(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Close закрывает базу. Перед закрытием — чекпойнт и выход из WAL-режима:
// SQLite удаляет -wal/-shm синхронно на соединении, и на Windows каталог
// данных можно сразу удалить/переместить (без гонки с отложенным удалением
// файлов ядром). Следующий Open снова включает WAL (DSN).
func (s *Store) Close() error {
	_, _ = s.db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
	_, _ = s.db.Exec(`PRAGMA journal_mode = DELETE`)
	return s.db.Close()
}

// Dir возвращает каталог данных.
func (s *Store) Dir() string { return s.dir }

// DB открывает доступ к *sql.DB (бэкапы VACUUM INTO и служебные операции).
func (s *Store) DB() *sql.DB { return s.db }

func nowMs() int64 { return time.Now().UnixMilli() }

// applyFileModes выставляет права 0700/0600 на unix (Р2-33).
// На Windows права не применяются (файловая система без POSIX-режима).
func (s *Store) applyFileModes() error {
	if runtime.GOOS == "windows" {
		return nil
	}
	if err := os.Chmod(s.dir, 0o700); err != nil {
		return fmt.Errorf("store: chmod dir: %w", err)
	}
	dbPath := filepath.Join(s.dir, dbFileName)
	if err := os.Chmod(dbPath, 0o600); err != nil {
		return fmt.Errorf("store: chmod db: %w", err)
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(dbPath + suffix); err == nil {
			if err := os.Chmod(dbPath+suffix, 0o600); err != nil {
				return fmt.Errorf("store: chmod %s: %w", suffix, err)
			}
		}
	}
	return nil
}

// withTx выполняет fn в одной транзакции (rollback при ошибке).
func (s *Store) withTx(fn func(tx *sql.Tx) error) error {
	if s.tx != nil { // внутри InTx: commit/rollback — у владельца транзакции
		return fn(s.tx)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// ---- meta ----

// GetMeta возвращает значение служебного ключа (false — ключа нет).
func (s *Store) GetMeta(key string) (string, bool, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM meta WHERE key = ?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

// SetMeta записывает служебный ключ. Идёт через withTx: внутри InTx запись
// входит в транзакцию серии (T-78: перенос из NekoBox пишет наборы маршрутов
// вместе с профилями), вне её — отдельная транзакция.
func (s *Store) SetMeta(key, value string) error {
	return s.withTx(func(tx *sql.Tx) error {
		_, err := tx.Exec(
			`INSERT INTO meta (key, value) VALUES (?, ?)
			 ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
		return err
	})
}
