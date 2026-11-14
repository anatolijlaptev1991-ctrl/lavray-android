// SPDX-License-Identifier: GPL-3.0-only

package store

// Целостность и версия схемы БД (T-50, A1-15, FR-UI-260 «битый файл — понятная
// ошибка, не тихий сброс»). Open НИЧЕГО не переименовывает, не удаляет и не
// пересоздаёт: повреждённый файл остаётся как есть, а вызывающая сторона
// (cmd/lavray-core) печатает причину и инструкцию. Автоматический «старт с
// чистой базы» выглядел бы для пользователя как тихая потеря всех профилей.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	sqlite "modernc.org/sqlite"
)

// ErrCorrupt — файл базы данных повреждён (не SQLite либо нарушена
// целостность страниц). Проверяется через errors.Is; детали — *CorruptError.
var ErrCorrupt = errors.New("store: database file is corrupt")

// ErrSchemaTooNew — схема БД новее известной этой сборке; детали —
// *SchemaTooNewError.
var ErrSchemaTooNew = errors.New("store: database schema is newer than this build supports")

// CorruptError — Open не смог доверять файлу БД.
type CorruptError struct {
	Path  string // полный путь lavray.db
	Cause error  // исходная ошибка SQLite или результат quick_check
}

func (e *CorruptError) Error() string {
	return fmt.Sprintf("store: база данных повреждена (%s): %v", e.Path, e.Cause)
}
func (e *CorruptError) Unwrap() error        { return e.Cause }
func (e *CorruptError) Is(target error) bool { return target == ErrCorrupt }

// SchemaTooNewError — в БД записана версия схемы выше последней миграции этой
// сборки: файл создан более новой версией LAVRay. Старая версия не должна
// писать в такую схему.
type SchemaTooNewError struct {
	Found     int64 // версия в файле
	Supported int64 // последняя миграция этой сборки
}

func (e *SchemaTooNewError) Error() string {
	return fmt.Sprintf("store: версия схемы базы данных v%d новее поддерживаемой v%d — файл создан более новой версией LAVRay", e.Found, e.Supported)
}
func (e *SchemaTooNewError) Is(target error) bool { return target == ErrSchemaTooNew }

// DBPath — путь к файлу базы данных в каталоге данных dir.
func DBPath(dir string) string { return filepath.Join(dir, dbFileName) }

// checkIntegrity выполняет PRAGMA quick_check до миграций. «file is not a
// database» и «malformed» приходят ошибкой запроса, повреждённые страницы при
// целом заголовке — строкой результата.
func checkIntegrity(db *sql.DB, path string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	var res string
	if err := db.QueryRowContext(ctx, `PRAGMA quick_check(1)`).Scan(&res); err != nil {
		if isCorruptErr(err) {
			return &CorruptError{Path: path, Cause: err}
		}
		return fmt.Errorf("store: quick_check: %w", err)
	}
	if res != "ok" {
		first, _, _ := strings.Cut(strings.ReplaceAll(res, "*** in database main ***\n", ""), "\n")
		return &CorruptError{Path: path, Cause: fmt.Errorf("quick_check: %s", first)}
	}
	return nil
}

// openError превращает ошибку миграции в типизированную ошибку Open:
// версия «из будущего» и повреждение различимы через errors.As/Is.
func openError(path string, err error) error {
	var tooNew *SchemaTooNewError
	switch {
	case errors.As(err, &tooNew):
		return tooNew
	case isCorruptErr(err):
		return &CorruptError{Path: path, Cause: err}
	}
	return fmt.Errorf("store: migrate: %w", err)
}

// isCorruptErr — SQLITE_CORRUPT (11) и SQLITE_NOTADB (26), в том числе
// расширенные коды; запасной путь — текст ошибки.
func isCorruptErr(err error) bool {
	var se *sqlite.Error
	if errors.As(err, &se) {
		switch se.Code() & 0xff {
		case 11, 26:
			return true
		}
	}
	m := strings.ToLower(err.Error())
	return strings.Contains(m, "file is not a database") || strings.Contains(m, "database disk image is malformed")
}
