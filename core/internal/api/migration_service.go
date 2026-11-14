// SPDX-License-Identifier: GPL-3.0-only

package api

// migration_service.go — MigrationService (T-78): перенос данных из NekoBox.
// Источник только читается; NekoBox не запускается и не останавливается.
// Агентам весь сервис закрыт (agent_table.go). Конвейер — migration_run.go.

import (
	"context"

	"connectrpc.com/connect"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/store/nekoimport"
)

// nekoDefaultEnv — окружение поиска по умолчанию. Тесты пакета подменяют его на
// пустое (migration_hermetic_test.go): настоящее смотрит процессы и профиль
// пользователя.
var nekoDefaultEnv = nekoimport.DefaultEnv

// migrationService — реализация lavrayv1connect.MigrationServiceHandler.
type migrationService struct {
	*migrator
	// env — окружение поиска установок; тесты подставляют своё.
	env nekoimport.Env
}

func newMigrationService(s *Server) *migrationService {
	return &migrationService{migrator: newMigrator(s.st, s.presetRepo), env: nekoDefaultEnv()}
}

// DetectNekoBox ищет установки: переданные пути, config рядом с запущенным
// NekoBox, %APPDATA% и %LOCALAPPDATA%. Пустой список — не ошибка.
func (m *migrationService) DetectNekoBox(ctx context.Context, req *connect.Request[lavrayv1.DetectNekoBoxRequest]) (*connect.Response[lavrayv1.DetectNekoBoxResponse], error) {
	out := &lavrayv1.DetectNekoBoxResponse{}
	for _, c := range nekoimport.Detect(m.env, req.Msg.ExtraPaths) {
		out.Candidates = append(out.Candidates, candidateProto(c))
	}
	return connect.NewResponse(out), nil
}

// PreviewNekoBoxImport — сухой прогон: те же числа, что даст ImportNekoBox.
func (m *migrationService) PreviewNekoBoxImport(ctx context.Context, req *connect.Request[lavrayv1.NekoBoxImportRequest]) (*connect.Response[lavrayv1.NekoBoxReport], error) {
	return m.handle(req.Msg, true)
}

// ImportNekoBox переносит данные одной транзакцией.
func (m *migrationService) ImportNekoBox(ctx context.Context, req *connect.Request[lavrayv1.NekoBoxImportRequest]) (*connect.Response[lavrayv1.NekoBoxReport], error) {
	return m.handle(req.Msg, false)
}

func (m *migrationService) handle(r *lavrayv1.NekoBoxImportRequest, dry bool) (*connect.Response[lavrayv1.NekoBoxReport], error) {
	rep, err := m.run(r.Path, NekoRunOptions{Routes: r.IncludeRoutes, Settings: r.IncludeSettings, DryRun: dry})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(reportProto(rep)), nil
}
