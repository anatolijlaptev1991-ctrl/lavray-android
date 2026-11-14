// SPDX-License-Identifier: GPL-3.0-only

package api

import (
	"context"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

// beginConnect пускает запуск подключения, если данные сейчас не заменяются
// восстановлением копии. Пока замена идёт (или ждёт своей очереди), запуск
// отклоняется; уже начавшийся запуск замена дожидается. release вызывается по
// окончании запуска.
func (b *backupService) beginConnect() (release func(), err error) {
	if !b.guard.TryRLock() {
		return nil, failedPrecondition("идёт восстановление резервной копии: повторите подключение через несколько секунд")
	}
	return b.guard.RUnlock, nil
}

// startRun — общая точка запуска Connect, RestartProfile и ConnectLast: сборка
// конфига и старт инстанса (startRunLocked) идут под защитой от замены данных
// (T-83): профиль и группа не исчезнут из-под запускаемого подключения, а
// восстановление не начнётся поверх запуска.
func (s *Server) startRun(ctx context.Context, profile *model.Profile, mode lavrayv1.ConnectMode) (*lavrayv1.CoreStatus, []string, error) {
	release, err := s.backup.beginConnect()
	if err != nil {
		return nil, nil, err
	}
	defer release()
	return s.startRunLocked(ctx, profile, mode)
}
