// SPDX-License-Identifier: GPL-3.0-only

package api

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"connectrpc.com/connect"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1connect"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

const exportPassword = "unredacted-Pw-7f3a91"

func exportConfig(cs *clients, id int64, purpose lavrayv1.ExportConfigRequest_Purpose) (*lavrayv1.ExportConfigResponse, error) {
	res, err := cs.profile.ExportConfig(context.Background(),
		connect.NewRequest(&lavrayv1.ExportConfigRequest{ProfileId: id, Purpose: purpose}))
	if err != nil {
		return nil, err
	}
	return res.Msg, nil
}

func savedTrojan(t *testing.T, cs *clients, password string) int64 {
	t.Helper()
	p := &model.Profile{GroupID: 1, Type: model.TypeTrojan, Name: "exp", Address: "exp.example.org", Port: 443,
		Detail: &model.TrojanVlessDetail{Password: password},
		Stream: &model.StreamSettings{Security: "tls", SNI: "exp.example.org"}}
	if err := cs.st.CreateProfile(p); err != nil {
		t.Fatal(err)
	}
	return p.ID
}

func inboundsOf(t *testing.T, cfgJSON string) []any {
	t.Helper()
	var cfg map[string]any
	if err := json.Unmarshal([]byte(cfgJSON), &cfg); err != nil {
		t.Fatalf("конфиг не JSON: %v", err)
	}
	list, _ := cfg["inbounds"].([]any)
	return list
}

// Экспорт отдаёт пароль как есть: маскирование только у GetRuntimeConfig и журнала.
func TestExportConfigUnredacted(t *testing.T) {
	cs := newE2E(t)
	id := savedTrojan(t, cs, exportPassword)

	got, err := exportConfig(cs, id, lavrayv1.ExportConfigRequest_PURPOSE_RUN)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.SingBoxJson, exportPassword) {
		t.Fatalf("пароль в экспорте замаскирован:\n%s", got.SingBoxJson)
	}
	if got.IsRawConfig {
		t.Fatal("обычный профиль помечен как raw-конфиг")
	}
}

// RUN — рабочий конфиг с входящими, TEST — без входящих и без cache_file.
func TestExportConfigPurposeDiffers(t *testing.T) {
	cs := newE2E(t)
	id := savedTrojan(t, cs, exportPassword)

	run, err := exportConfig(cs, id, lavrayv1.ExportConfigRequest_PURPOSE_RUN)
	if err != nil {
		t.Fatal(err)
	}
	test, err := exportConfig(cs, id, lavrayv1.ExportConfigRequest_PURPOSE_TEST)
	if err != nil {
		t.Fatal(err)
	}
	if len(inboundsOf(t, run.SingBoxJson)) == 0 {
		t.Error("RUN: нет входящих")
	}
	if n := len(inboundsOf(t, test.SingBoxJson)); n != 0 {
		t.Errorf("TEST: %d входящих, want 0", n)
	}
	if run.SingBoxJson == test.SingBoxJson {
		t.Error("RUN и TEST совпадают")
	}
	if !strings.Contains(test.SingBoxJson, exportPassword) {
		t.Error("TEST: пароль потерян")
	}
}

// Профиля нет — NOT_FOUND.
func TestExportConfigNotFound(t *testing.T) {
	cs := newE2E(t)
	if _, err := exportConfig(cs, 4040, lavrayv1.ExportConfigRequest_PURPOSE_RUN); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("code = %v, err = %v", connect.CodeOf(err), err)
	}
}

// Внешнее ядро в конфиг sing-box не выражается — FAILED_PRECONDITION.
func TestExportConfigExternalCore(t *testing.T) {
	cs := newE2E(t)
	naive := &model.Profile{GroupID: 1, Type: model.TypeNaive, Name: "n", Address: "n.example.org", Port: 443,
		Detail: &model.NaiveDetail{Username: "u", Password: "p"}}
	if err := cs.st.CreateProfile(naive); err != nil {
		t.Fatal(err)
	}
	if _, err := exportConfig(cs, naive.ID, lavrayv1.ExportConfigRequest_PURPOSE_RUN); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("code = %v, err = %v", connect.CodeOf(err), err)
	}
}

// Профиль, из которого конфиг не собирается, — INVALID_ARGUMENT с причиной.
func TestExportConfigBuildErrorInvalidArgument(t *testing.T) {
	cs := newE2E(t)
	id := savedTrojan(t, cs, "")

	_, err := exportConfig(cs, id, lavrayv1.ExportConfigRequest_PURPOSE_RUN)
	if connect.CodeOf(err) != connect.CodeInvalidArgument || !strings.Contains(err.Error(), "password") {
		t.Fatalf("code = %v, err = %v", connect.CodeOf(err), err)
	}
}

// «Свой полный конфиг» отдаётся как есть и с признаком is_raw_config, но только
// после подтверждения.
func TestExportConfigInternalFullRaw(t *testing.T) {
	cs := newE2E(t)
	ctx := context.Background()
	created, err := cs.profile.CreateProfile(ctx, connect.NewRequest(&lavrayv1.CreateProfileRequest{
		Profile: internalFullProto(`{"outbounds":[{"type":"direct","tag":"direct"}]}`),
	}))
	if err != nil {
		t.Fatal(err)
	}
	id := created.Msg.GetId()
	if _, err := exportConfig(cs, id, lavrayv1.ExportConfigRequest_PURPOSE_RUN); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("неподтверждённый internal-full: %v", err)
	}
	confirmByHash(t, cs, id)

	got, err := exportConfig(cs, id, lavrayv1.ExportConfigRequest_PURPOSE_RUN)
	if err != nil {
		t.Fatal(err)
	}
	if !got.IsRawConfig || !strings.Contains(got.SingBoxJson, `"direct"`) {
		t.Fatalf("raw=%v json=%s", got.IsRawConfig, got.SingBoxJson)
	}
}

// Агенту ExportConfig закрыт в любом режиме: конфиг содержит секреты.
func TestExportConfigAgentDenied(t *testing.T) {
	f := newAgentFixture(t)
	for _, mode := range []string{AgentModeOff, AgentModeRead, AgentModeControl} {
		f.setMode(t, mode)
		err := unary(f.agent.profile.ExportConfig, &lavrayv1.ExportConfigRequest{ProfileId: f.profile.ID})
		if !denied(err) {
			t.Errorf("режим %s: ExportConfig не закрыт: %v", mode, err)
		}
	}
	if agentTable[lavrayv1connect.ProfileServiceExportConfigProcedure] != agentDeny {
		t.Error("в таблице агента ExportConfig не deny")
	}
}
