// SPDX-License-Identifier: GPL-3.0-only

package api

// backup_tempdir.go — временные каталоги операций с копией. В них лежит
// РАСШИФРОВАННЫЙ снимок БД (секреты открытым текстом), поэтому каталог
// удаляется сразу после операции, сбой удаления попадает в журнал, а то, что
// осталось после падения процесса, подметается при создании сервиса.

import (
	"fmt"
	"os"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/logs"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/store"
)

// tempDir создаёт временный каталог операции; cleanup удаляет его.
func (b *backupService) tempDir() (dir string, cleanup func(), err error) {
	dir, err = os.MkdirTemp(b.srv.st.Dir(), store.TempDirBackup)
	if err != nil {
		return "", nil, fmt.Errorf("временный каталог копии: %w", err)
	}
	return dir, func() { b.removeTempDir(dir) }, nil
}

// removeTempDir удаляет каталог; неудача — предупреждение в журнале (каталог
// подметёт следующий запуск), а не ошибка уже выполненной операции.
func (b *backupService) removeTempDir(dir string) {
	if err := b.removeDir(dir); err != nil {
		b.srv.logs.Logf(logs.LevelWarn, logs.SourceAPI,
			"резервная копия: %v; каталог подметёт следующий запуск ядра", err)
	}
}

// startupCleanup подключает журнал предупреждений хранилища и подметает
// каталоги backup-* и restore-*, оставшиеся от упавшего процесса. Вызывается
// при создании сервиса, до первой операции с копией.
func (b *backupService) startupCleanup() {
	st := b.srv.st
	st.SetWarnLogger(func(format string, args ...any) {
		b.srv.logs.Logf(logs.LevelWarn, logs.SourceAPI, format, args...)
	})
	res := store.SweepStaleTempDirs(st.Dir())
	if n := len(res.Removed); n > 0 {
		b.srv.logs.Logf(logs.LevelWarn, logs.SourceAPI,
			"удалено оставшихся после сбоя временных каталогов резервной копии: %d", n)
	}
	for _, err := range res.Failed {
		b.srv.logs.Logf(logs.LevelWarn, logs.SourceAPI, "резервная копия: %v", err)
	}
}
