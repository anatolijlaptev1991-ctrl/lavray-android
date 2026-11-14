// SPDX-License-Identifier: GPL-3.0-only

package api

// agent_table_test.go — таблица решений для агентов (agent_table.go) покрывает
// все процедуры proto и не меняет белые списки, выпущенные раньше.

import (
	"maps"
	"slices"
	"sort"
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1connect"
)

// allProtoProcedures — "/<пакет>.<Сервис>/<Метод>" для каждого метода каждого
// сервиса lavray.proto. Берётся из дескрипторов proto, а не из констант connect,
// чтобы тест видел процедуру, даже если код connect не пересобран.
func allProtoProcedures() []string {
	var out []string
	services := lavrayv1.File_lavray_proto.Services()
	for i := 0; i < services.Len(); i++ {
		svc := services.Get(i)
		methods := svc.Methods()
		for j := 0; j < methods.Len(); j++ {
			out = append(out, procedureName(svc, methods.Get(j)))
		}
	}
	sort.Strings(out)
	return out
}

// procedureName — путь процедуры в Connect: "/<полное имя сервиса>/<метод>".
func procedureName(svc protoreflect.ServiceDescriptor, m protoreflect.MethodDescriptor) string {
	return "/" + string(svc.FullName()) + "/" + string(m.Name())
}

func TestEveryProcedureHasAgentDecision(t *testing.T) {
	procedures := allProtoProcedures()
	if len(procedures) == 0 {
		t.Fatal("в lavray.proto не нашлось ни одной процедуры")
	}
	known := make(map[string]bool, len(procedures))
	for _, p := range procedures {
		known[p] = true
		if _, ok := agentTable[p]; !ok {
			t.Errorf("у процедуры %s нет решения в agentTable: добавьте agentDeny, agentRead или agentControl", p)
		}
	}
	for p := range agentTable {
		if !known[p] {
			t.Errorf("agentTable содержит %q, но такой процедуры нет в lavray.proto", p)
		}
	}
	if len(agentTable) != len(procedures) {
		t.Errorf("в agentTable %d записей, в proto %d процедур", len(agentTable), len(procedures))
	}
}

func TestAgentTableKeepsPreviousAllowlists(t *testing.T) {
	// Снимок белых списков, выпущенных до таблицы (agent.go, 0.2.1). Расширять
	// доступ агентов можно только осознанно: правка этого снимка в ревью видна.
	wantRead := map[string]bool{
		lavrayv1connect.CoreServiceGetStatusProcedure:                 true,
		lavrayv1connect.ProfileServiceListProfilesProcedure:           true,
		lavrayv1connect.ProfileServiceListGroupsProcedure:             true,
		lavrayv1connect.SubscriptionServiceListSubscriptionsProcedure: true,
		lavrayv1connect.StatsServiceSubscribeProcedure:                true,
		lavrayv1connect.StatsServiceGetTrafficTotalsProcedure:         true,
		lavrayv1connect.StatsServiceGetTrafficSummaryProcedure:        true, // T-79: сводка трафика группы — read
		lavrayv1connect.DiagnosticsServiceGetPublicIpProcedure:        true,
		lavrayv1connect.LogServiceGetTailProcedure:                    true,
		lavrayv1connect.UpdateServiceGetUpdateStateProcedure:          true, // T-75: состояние обновлений — read
	}
	wantControl := map[string]bool{
		lavrayv1connect.CoreServiceConnectProcedure:                        true,
		lavrayv1connect.CoreServiceDisconnectProcedure:                     true,
		lavrayv1connect.ProfileServiceTestLatencyProcedure:                 true,
		lavrayv1connect.ProfileServiceStopTestsProcedure:                   true,
		lavrayv1connect.SubscriptionServiceUpdateSubscriptionProcedure:     true,
		lavrayv1connect.SubscriptionServiceUpdateAllSubscriptionsProcedure: true,
	}
	if !maps.Equal(agentReadProcedures, wantRead) {
		t.Errorf("agentReadProcedures изменился:\n got %v\nwant %v", slices.Sorted(maps.Keys(agentReadProcedures)), slices.Sorted(maps.Keys(wantRead)))
	}
	if !maps.Equal(agentControlProcedures, wantControl) {
		t.Errorf("agentControlProcedures изменился:\n got %v\nwant %v", slices.Sorted(maps.Keys(agentControlProcedures)), slices.Sorted(maps.Keys(wantControl)))
	}
	for p := range agentReadProcedures {
		if agentControlProcedures[p] {
			t.Errorf("%s одновременно в read и control", p)
		}
	}
}
