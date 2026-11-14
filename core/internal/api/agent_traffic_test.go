// SPDX-License-Identifier: GPL-3.0-only

package api

// agent_traffic_test.go — доступ агентов к соединениям и статистике (T-79):
// адреса назначения — приватные данные, поэтому соединения закрыты в любом
// режиме; числа сводки трафика и поток скоростей читать можно.

import (
	"context"
	"testing"

	"connectrpc.com/connect"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1connect"
)

func TestAgentConnectionsDeniedSummaryRead(t *testing.T) {
	// Решения в таблице.
	for procedure, want := range map[string]agentDecision{
		lavrayv1connect.ConnectionServiceListConnectionsProcedure:  agentDeny,
		lavrayv1connect.ConnectionServiceWatchConnectionsProcedure: agentDeny,
		lavrayv1connect.ConnectionServiceCloseConnectionProcedure:  agentDeny,
		lavrayv1connect.StatsServiceGetTrafficSummaryProcedure:     agentRead,
		lavrayv1connect.StatsServiceSubscribeProcedure:             agentRead,
	} {
		if got := agentTable[procedure]; got != want {
			t.Errorf("%s: решение %v, ожидалось %v", procedure, got, want)
		}
	}

	f := newAgentFixture(t)
	summary := func() error {
		return unary(f.agent.stats.GetTrafficSummary, &lavrayv1.GetTrafficSummaryRequest{})
	}
	connCalls := func() map[string]error {
		ctx, cancel := context.WithTimeout(context.Background(), 3e8)
		defer cancel()
		return map[string]error{
			"ListConnections":  unary(f.agent.conns.ListConnections, &lavrayv1.ListConnectionsRequest{}),
			"WatchConnections": drain(f.agent.conns.WatchConnections(ctx, connect.NewRequest(&lavrayv1.WatchConnectionsRequest{}))),
			"CloseConnection":  unary(f.agent.conns.CloseConnection, &lavrayv1.CloseConnectionRequest{Id: "00000000-0000-0000-0000-000000000000"}),
		}
	}

	// off: закрыто всё, включая сводку.
	if !denied(summary()) {
		t.Error("off: GetTrafficSummary не отклонён")
	}
	for name, err := range connCalls() {
		if !denied(err) {
			t.Errorf("off: %s не отклонён: %v", name, err)
		}
	}
	// read и control: сводка открыта, соединения закрыты.
	for _, mode := range []string{AgentModeRead, AgentModeControl} {
		f.setMode(t, mode)
		if err := summary(); err != nil {
			t.Errorf("%s: GetTrafficSummary: %v", mode, err)
		}
		for name, err := range connCalls() {
			if !denied(err) {
				t.Errorf("%s: %s не отклонён: %v", mode, name, err)
			}
		}
	}
}
