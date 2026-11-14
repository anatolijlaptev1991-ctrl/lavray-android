// SPDX-License-Identifier: GPL-3.0-only

package store

// temp_dirs.go — временные каталоги резервной копии. В них лежат РАСШИФРОВАННЫЕ
// снимки БД (секреты открытым текстом, как и в lavray.db), поэтому каталог
// удаляется сразу после операции, а оставшиеся после падения процесса
// подметаются при старте.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Префиксы временных каталогов внутри каталога данных: backup- создаёт слой
// API (сборка и разбор копии), restore- — Store.Restore.
const (
	TempDirBackup  = "backup-"
	TempDirRestore = "restore-"
)

// Повторы удаления: на Windows хэндлы SQLite закрываются с небольшой задержкой.
// Переменные — чтобы тест не ждал секунды.
var (
	removeAll    = os.RemoveAll
	removeTries  = 40
	removeDelay  = 50 * time.Millisecond
	staleDirPref = []string{TempDirBackup, TempDirRestore}
)

// RemoveTempDir удаляет каталог с повторами. Ошибка после всех попыток
// возвращается вызывающему: он обязан её записать в журнал, а каталог
// подметёт следующий запуск (SweepStaleTempDirs).
func RemoveTempDir(dir string) error {
	var err error
	for i := 0; i < removeTries; i++ {
		if err = removeAll(dir); err == nil {
			return nil
		}
		time.Sleep(removeDelay)
	}
	return fmt.Errorf("store: не удалось удалить временный каталог %s: %w", filepath.Base(dir), err)
}

// SweepResult — итог подметания.
type SweepResult struct {
	Removed []string // имена удалённых каталогов
	Failed  []error  // каталоги, которые удалить не удалось
}

// SweepStaleTempDirs удаляет в dir каталоги backup-* и restore-*, оставшиеся от
// упавшего процесса (расшифрованные снимки). Вызывать при старте, до начала
// операций с копиями: работающую операцию он бы лишил каталога. Файлы и
// символические ссылки не трогаются.
func SweepStaleTempDirs(dir string) SweepResult {
	var res SweepResult
	entries, err := os.ReadDir(dir)
	if err != nil {
		res.Failed = append(res.Failed, fmt.Errorf("store: чтение каталога данных: %w", err))
		return res
	}
	for _, e := range entries {
		if !e.IsDir() || !hasAnyPrefix(e.Name(), staleDirPref) { // IsDir ложно для ссылок
			continue
		}
		if err := RemoveTempDir(filepath.Join(dir, e.Name())); err != nil {
			res.Failed = append(res.Failed, err)
			continue
		}
		res.Removed = append(res.Removed, e.Name())
	}
	return res
}

func hasAnyPrefix(name string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}
