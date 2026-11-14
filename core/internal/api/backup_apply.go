// SPDX-License-Identifier: GPL-3.0-only

package api

// backup_apply.go — замена данных из проверенной копии и всё, что нужно ядру
// после неё: остановка и перезапуск планировщика подписок, сброс снимков
// отката, рассылка SettingsChanged.

import (
	"context"
	"fmt"
	"strings"

	"connectrpc.com/connect"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/inst"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/subs"
)

// connectionActive: туннель поднимается, работает или останавливается.
func (b *backupService) connectionActive() bool {
	switch b.srv.inst.Status().State {
	case inst.StateStarting, inst.StateReady, inst.StateStopping:
		return true
	}
	return false
}

// subscriptionUpdateRunning: идёт ручное или автоматическое обновление подписки.
func (b *backupService) subscriptionUpdateRunning() bool {
	b.srv.subsMu.Lock()
	defer b.srv.subsMu.Unlock()
	return len(b.srv.subsRunning) > 0
}

// checkReplaceAllowed: замену данных нельзя начинать при активном подключении
// (профиль под работающим инстансом исчез бы) и при идущем обновлении подписки
// (его результат лёг бы поверх восстановленных данных).
func (b *backupService) checkReplaceAllowed() error {
	if b.connectionActive() {
		return backupError(connect.CodeFailedPrecondition, reasonConnectionActive,
			"есть активное подключение: отключитесь перед восстановлением копии")
	}
	if b.subscriptionUpdateRunning() {
		return backupError(connect.CodeAborted, reasonBusy, "идёт обновление подписки: повторите позже")
	}
	return nil
}

// replaceAll атомарно заменяет данные. Замена держит guard на запись: пока она
// идёт, запуск подключения (connect_guard.go) отказывает, а уже начавшийся
// дожидается конца, и проверка «подключения нет» остаётся верной до самого
// Restore. Планировщик подписок останавливается на время замены и запускается
// заново. Ошибки возвращаются как есть: наружу их превращает b.failed.
func (b *backupService) replaceAll(ctx context.Context, opened *openedBackup) (*lavrayv1.ImportBackupResponse, error) {
	st := b.srv.st
	if b.beforeLock != nil {
		b.beforeLock()
	}
	b.guard.Lock()
	defer b.guard.Unlock()
	before, err := st.Settings()
	if err != nil {
		return nil, err
	}
	resume := b.srv.sched.pause()
	defer resume()
	if err := b.checkReplaceAllowed(); err != nil {
		return nil, err
	}
	if b.onReplacing != nil {
		if err := b.onReplacing(); err != nil {
			return nil, err
		}
	}
	if err := st.Restore(ctx, opened.path); err != nil {
		if cerr := contentBackupError(err); cerr != err {
			return nil, cerr
		}
		return nil, fmt.Errorf("восстановление копии не удалось, данные не изменены: %w", err)
	}
	b.resetAfterRestore()
	after, err := st.Settings()
	if err != nil {
		return nil, err
	}
	ev, needsRestart := settingsDiff(before, after)
	b.srv.publishSettingsChanged(ev)

	stats, err := st.Stats(ctx)
	if err != nil {
		return nil, err
	}
	schema, err := st.SchemaVersion()
	if err != nil {
		return nil, err
	}
	return &lavrayv1.ImportBackupResponse{
		Restored:        liveSummary(opened.doc, stats, schema),
		NeedsAppRestart: needsRestart,
	}, nil
}

// resetAfterRestore сбрасывает состояние в памяти, привязанное к прежним id.
func (b *backupService) resetAfterRestore() {
	b.srv.subsMu.Lock()
	b.srv.subsSnap = map[int64]*subs.Snapshot{}
	b.srv.subsMu.Unlock()
}

// isUpdateKey — состояние обновлений: при восстановлении не меняется.
func isUpdateKey(key string) bool { return strings.HasPrefix(key, "update.") }
