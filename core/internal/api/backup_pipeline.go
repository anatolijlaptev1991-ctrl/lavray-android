// SPDX-License-Identifier: GPL-3.0-only

package api

// backup_pipeline.go — сборка и разбор файла копии: снимок БД ↔ контейнер ↔
// шифр. Файлы живут во временном каталоге внутри каталога данных и удаляются
// сразу (backup_tempdir.go); снимок содержит секреты открытым текстом, как и
// сама lavray.db. Снимок читается и пишется потоком, в памяти он целиком не
// держится.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"connectrpc.com/connect"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/backup"
)

// snapshotFile — имя файла снимка во временном каталоге.
const snapshotFile = "snapshot.db"

// snapshotLimit — предел размера снимка БД перед упаковкой.
func (b *backupService) snapshotLimit() int64 {
	if b.maxSnapshot > 0 {
		return b.maxSnapshot
	}
	return backup.MaxSnapshotBytes
}

// tooLarge — отказ экспорта: копия не поместилась бы в предел восстановления.
func tooLarge(format string, args ...any) error {
	return backupError(connect.CodeResourceExhausted, reasonTooLarge, format, args...)
}

// buildBackup снимает БД, проверяет снимок и возвращает зашифрованный файл.
func (b *backupService) buildBackup(ctx context.Context, password string) ([]byte, summaryDoc, error) {
	dir, cleanup, err := b.tempDir()
	if err != nil {
		return nil, summaryDoc{}, err
	}
	defer cleanup()
	path := filepath.Join(dir, snapshotFile)
	st := b.srv.st
	if err := st.Snapshot(ctx, path); err != nil {
		return nil, summaryDoc{}, err
	}
	// База больше предела: копия создалась бы, но не восстановилась (Unpack
	// режет на том же пределе). Отказываем сразу, с понятной причиной.
	fi, err := os.Stat(path)
	if err != nil {
		return nil, summaryDoc{}, err
	}
	if fi.Size() > b.snapshotLimit() {
		return nil, summaryDoc{}, tooLarge("база данных больше %d МиБ: такую копию нельзя было бы восстановить", b.snapshotLimit()>>20)
	}
	info, err := st.InspectCopy(ctx, path) // снимок открывается сам: проверка до шифрования
	if err != nil {
		return nil, summaryDoc{}, fmt.Errorf("проверка снимка: %w", err)
	}
	doc := newSummaryDoc(b.srv.upd.appVersion(), b.now().UnixMilli(), info)
	summary, err := json.Marshal(doc)
	if err != nil {
		return nil, summaryDoc{}, err
	}
	body, err := packFile(path, summary)
	if err != nil {
		return nil, summaryDoc{}, err
	}
	file, err := backup.SealPassword(password, body, b.sealOpts)
	if errors.Is(err, backup.ErrTooLarge) {
		return nil, summaryDoc{}, tooLarge("копия получилась больше %d МиБ", backup.MaxFileBytes>>20)
	}
	if err != nil {
		return nil, summaryDoc{}, err
	}
	return file, doc, nil
}

// packFile упаковывает снимок из файла потоком.
func packFile(path string, summary []byte) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	body, err := backup.Pack(summary, f)
	if errors.Is(err, backup.ErrTooLarge) {
		return nil, tooLarge("копия получилась больше предела (%d МиБ)", backup.MaxFileBytes>>20)
	}
	return body, err
}

// openedBackup — расшифрованная и проверенная копия.
type openedBackup struct {
	doc  summaryDoc // содержимое снимка + версия приложения и время из сводки
	path string     // файл снимка во временном каталоге
}

// openBackup расшифровывает data паролем, распаковывает снимок потоком во
// временный файл и проверяет его (целостность, версия схемы). Данные
// хранилища не меняются.
func (b *backupService) openBackup(ctx context.Context, data []byte, password string) (*openedBackup, func(), error) {
	body, err := backup.OpenPassword(password, data)
	if err != nil {
		return nil, nil, openBackupError(err)
	}
	dir, cleanup, err := b.tempDir()
	if err != nil {
		return nil, nil, err
	}
	path := filepath.Join(dir, snapshotFile)
	summary, err := unpackToFile(body, path)
	if err != nil {
		cleanup()
		return nil, nil, contentBackupError(err)
	}
	doc, err := parseSummaryDoc(summary)
	if err != nil {
		cleanup()
		return nil, nil, contentBackupError(err)
	}
	info, err := b.srv.st.InspectCopy(ctx, path)
	if err != nil {
		cleanup()
		return nil, nil, contentBackupError(err)
	}
	return &openedBackup{doc: doc.withContent(info), path: path}, cleanup, nil
}

// unpackToFile распаковывает снимок из тела в новый файл path (0600).
func unpackToFile(body []byte, path string) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, fmt.Errorf("запись снимка: %w", err)
	}
	summary, err := backup.Unpack(body, f)
	if cerr := f.Close(); err == nil && cerr != nil {
		err = fmt.Errorf("запись снимка: %w", cerr)
	}
	return summary, err
}
