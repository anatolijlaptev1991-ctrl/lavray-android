// SPDX-License-Identifier: GPL-3.0-only

package api

// migration_errors.go — проверка пути переноса из NekoBox и коды ошибок
// MigrationService: INVALID_ARGUMENT (путь пуст, не абсолютный, не каталог, в
// нём нет ни groups, ни profiles), NOT_FOUND (пути нет), RESOURCE_EXHAUSTED
// (пределы разбора), ABORTED (параллельный перенос или гонка с записью).

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"connectrpc.com/connect"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/presets"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/store/nekoimport"
)

// validateNekoPath проверяет путь и возвращает каталог config: сам путь либо
// вложенный config (выбрали каталог установки).
func validateNekoPath(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", invalidArg("путь к каталогу NekoBox не указан")
	}
	if !filepath.IsAbs(path) {
		return "", invalidArg("путь должен быть абсолютным: %q", path)
	}
	fi, err := os.Stat(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return "", notFound("каталог %q не найден", path)
	case err != nil:
		return "", invalidArg("каталог %q недоступен: %v", path, err)
	case !fi.IsDir():
		return "", invalidArg("%q не каталог", path)
	}
	dir, ok := nekoimport.ResolveConfigDir(path)
	if !ok {
		return "", invalidArg("в %q нет ни groups, ни profiles: это не каталог config NekoBox", path)
	}
	return dir, nil
}

// ValidateNekoBoxPath — проверка пути для CLI до открытия базы (ошибки — как у
// MigrationService).
func ValidateNekoBoxPath(path string) error {
	_, err := validateNekoPath(path)
	return err
}

// mapMigrationErr приводит ошибки переноса к кодам Connect; остальное — как mapErr.
func mapMigrationErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, nekoimport.ErrLimits), errors.Is(err, presets.ErrLimit):
		return connect.NewError(connect.CodeResourceExhausted, err)
	case errors.Is(err, nekoimport.ErrConflict):
		return connect.NewError(connect.CodeAborted, err)
	}
	return mapErr(err)
}

// abortedImport — параллельный перенос уже идёт.
func abortedImport() error {
	return connect.NewError(connect.CodeAborted, errors.New("уже идёт перенос из NekoBox"))
}
