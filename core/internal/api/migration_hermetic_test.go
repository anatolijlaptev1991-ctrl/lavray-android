// SPDX-License-Identifier: GPL-3.0-only

package api

// migration_hermetic_test.go — тесты не смотрят в настоящую систему. Без этого
// DetectNekoBox{} из rpc_ratchet_test.go и TestAgentDeniedMigration
// перечисляли бы процессы и обращались к %APPDATA%\nekoray на машине
// разработчика, где может работать живой NekoBox со своими данными.

import (
	"testing"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/inst"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/store/nekoimport"
)

// Каждый Server, созданный в тестах пакета, получает пустое окружение поиска:
// ни процессов, ни каталогов профиля. Тест, которому нужны свои значения,
// ставит s.migration.env сам.
func init() {
	nekoDefaultEnv = func() nekoimport.Env {
		return nekoimport.Env{Processes: func() ([]nekoimport.Process, error) { return nil, nil }}
	}
}

func TestMigrationEnvIsHermeticInTests(t *testing.T) {
	servers := map[string]*Server{
		"svcFixture": newSvcFixture(t, nil).srv,
		"e2e":        NewServer(Deps{Store: newSvcFixture(t, nil).srv.st, Runner: &inst.FakeRunner{}}),
	}
	_, e2e := newE2EServer(t, Deps{Runner: &inst.FakeRunner{}}, func(s *Server) { servers["newE2EServer"] = s })
	e2e.Close()
	for name, s := range servers {
		env := s.migration.env
		if env.AppData != "" || env.LocalAppData != "" {
			t.Errorf("%s: окружение смотрит в профиль пользователя: %+v", name, env)
		}
		procs, err := env.Processes()
		if err != nil || len(procs) != 0 {
			t.Errorf("%s: окружение перечисляет процессы: %v %v", name, procs, err)
		}
	}
}
