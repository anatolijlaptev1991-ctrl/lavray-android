// SPDX-License-Identifier: GPL-3.0-only

package mobile

// testfactory.go — фабрика временных инстансов проверки задержки.
//
// Временный инстанс (inst.NewTestInstance) создаёт контекст из контекста запроса.
// Платформу и системный DNS ему нужно передать так, чтобы параллельные проверки
// не делили реестр служб (см. host.go), поэтому фабрика оборачивает
// NewTestInstance и подставляет каждому вызову свой контекст host.boxContext.
// Установка идёт через api.SetTestInstanceFactory: его сторона в ядре —
// internal/api (tunnel_inproc.go, testfactory_inject.go).

import (
	"context"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/api"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/inst"
)

func (h *host) testFactory() inst.TestInstanceFactory {
	return func(ctx context.Context, cfg []byte) (inst.TestDialer, error) {
		return inst.NewTestInstance(h.boxContext(ctx), cfg)
	}
}

// installTestFactory подключает фабрику к серверу API до первого запроса.
func installTestFactory(s *api.Server, f inst.TestInstanceFactory) {
	api.SetTestInstanceFactory(s, f)
}
