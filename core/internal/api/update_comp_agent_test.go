// SPDX-License-Identifier: GPL-3.0-only

package api

import (
	"context"
	"net/http"
	"testing"

	"connectrpc.com/connect"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1connect"
)

// TestAgentDeniedDataUpdates (S): агентский токен не скачивает, не применяет и не
// откатывает наборы и гео-данные ни в режиме read, ни в control; основной токен
// проходит дальше решения для агентов (здесь — до отказа по состоянию).
func TestAgentDeniedDataUpdates(t *testing.T) {
	f := newAgentFixture(t)
	hc := &http.Client{Transport: bearerRT{testAgentToken}}
	agent := lavrayv1connect.NewUpdateServiceClient(hc, f.url)
	main := lavrayv1connect.NewUpdateServiceClient(&http.Client{Transport: bearerRT{testMainToken}}, f.url)
	comps := []lavrayv1.UpdateComponent{compPresets, compGeo}
	for _, mode := range []string{"read", "control"} {
		f.setMode(t, mode)
		for _, c := range comps {
			calls := map[string]error{
				"DownloadUpdates": drain(agent.DownloadUpdates(context.Background(), connect.NewRequest(&lavrayv1.DownloadUpdatesRequest{Components: []lavrayv1.UpdateComponent{c}}))),
				"ApplyUpdates":    drain(agent.ApplyUpdates(context.Background(), connect.NewRequest(&lavrayv1.ApplyUpdatesRequest{Components: []lavrayv1.UpdateComponent{c}}))),
				"Rollback":        unary(agent.Rollback, &lavrayv1.RollbackRequest{Component: c}),
			}
			for name, err := range calls {
				if !denied(err) {
					t.Errorf("режим %s, агент, %s(%v): %v — ожидался PERMISSION_DENIED", mode, name, c, err)
				}
			}
		}
	}
	// Основной токен не отвергнут решением для агентов: без прошлой ревизии откат — состояние, а не запрет.
	if err := unary(main.Rollback, &lavrayv1.RollbackRequest{Component: compPresets}); denied(err) || connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Errorf("основной токен, Rollback(PRESETS): %v", err)
	}
}
