// SPDX-License-Identifier: GPL-3.0-only

package store

// restore_copy.go — подготовка файла копии к восстановлению: копия кладётся во
// временный каталог внутри каталога данных (права те же, что у lavray.db),
// проверяется и открывается обычным Open, то есть проходит целостность и
// миграции вверх. Файл-источник не меняется.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// CopyInfo — что нашлось в файле копии.
type CopyInfo struct {
	Stats         Stats
	SchemaVersion int64 // версия схемы В ФАЙЛЕ, до миграции вверх
}

// InspectCopy проверяет файл копии (целостность, версия схемы) и возвращает
// его содержимое. База хранилища не меняется. Ошибки: *CorruptError
// (ErrCorrupt) — файл не база LAVRay или повреждён; *SchemaTooNewError
// (ErrSchemaTooNew) — копия из более новой версии.
func (s *Store) InspectCopy(ctx context.Context, src string) (CopyInfo, error) {
	var info CopyInfo
	err := s.withCopy(ctx, src, func(c *Store, _ string, ver int64) error {
		st, err := c.Stats(ctx)
		info = CopyInfo{Stats: st, SchemaVersion: ver}
		return err
	})
	return info, err
}

// withCopy раскладывает src во временный каталог, проверяет и открывает как
// хранилище c; path — файл копии. Каталог удаляется при выходе.
func (s *Store) withCopy(ctx context.Context, src string, fn func(c *Store, path string, ver int64) error) error {
	dir, err := os.MkdirTemp(s.dir, "restore-")
	if err != nil {
		return fmt.Errorf("store: временный каталог: %w", err)
	}
	defer func() {
		if err := RemoveTempDir(dir); err != nil {
			s.warnf("%v; каталог подметёт следующий запуск", err)
		}
	}()
	path := filepath.Join(dir, dbFileName)
	if err := copyFile(src, path); err != nil {
		return err
	}
	ver, err := verifyCopy(ctx, path)
	if err != nil {
		return err
	}
	c, err := Open(dir)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()
	return fn(c, path, ver)
}

// verifyCopy: файл — целая база LAVRay нужной версии. Выполняется ДО Open,
// потому что Open на «чужой» валидной SQLite создал бы пустую схему LAVRay, и
// восстановление молча стёрло бы все данные пустыми таблицами.
func verifyCopy(ctx context.Context, path string) (int64, error) {
	db, err := sql.Open("sqlite", plainDSN(path))
	if err != nil {
		return 0, fmt.Errorf("store: открытие копии: %w", err)
	}
	defer func() { _ = db.Close() }()
	db.SetMaxOpenConns(1)
	corrupt := func(cause error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return &CorruptError{Path: path, Cause: cause}
	}
	var res string
	if err := db.QueryRowContext(ctx, `PRAGMA integrity_check(1)`).Scan(&res); err != nil {
		return 0, corrupt(err)
	}
	if res != "ok" {
		return 0, corrupt(fmt.Errorf("integrity_check: %s", res))
	}
	var ver sql.NullInt64
	if err := db.QueryRowContext(ctx, `SELECT MAX(version) FROM schema_version`).Scan(&ver); err != nil {
		return 0, corrupt(err)
	}
	if !ver.Valid {
		return 0, corrupt(errors.New("в копии нет версий схемы"))
	}
	if latest := migrations[len(migrations)-1].version; ver.Int64 > latest {
		return 0, &SchemaTooNewError{Found: ver.Int64, Supported: latest}
	}
	return ver.Int64, nil
}

// copyFile копирует src в dst (0600).
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("store: чтение копии: %w", err)
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("store: запись копии: %w", err)
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return fmt.Errorf("store: запись копии: %w", err)
	}
	return out.Close()
}
