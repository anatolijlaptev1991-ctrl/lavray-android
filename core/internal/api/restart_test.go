// SPDX-License-Identifier: GPL-3.0-only

package api

import (
	"bytes"
	"context"
	"testing"

	"connectrpc.com/connect"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/inst"
)

func setSetting(t *testing.T, s *Server, key string, v *lavrayv1.SettingValue) error {
	t.Helper()
	_, err := s.SetSettings(context.Background(), connect.NewRequest(&lavrayv1.SetSettingsRequest{
		Values: map[string]*lavrayv1.SettingValue{key: v}}))
	return err
}

func strSetting(v string) *lavrayv1.SettingValue {
	return &lavrayv1.SettingValue{Value: &lavrayv1.SettingValue_StringValue{StringValue: v}}
}

func intSetting(v int64) *lavrayv1.SettingValue {
	return &lavrayv1.SettingValue{Value: &lavrayv1.SettingValue_IntValue{IntValue: v}}
}

// T-28 (B2-02): RestartProfile сначала собирает и проверяет новый конфиг и
// лишь потом останавливает инстанс. Ошибка сборки не роняет подключение.
func TestRestartProfileKeepsConnectionWhenConfigInvalid(t *testing.T) {
	f := newSvcFixture(t, nil)
	s, ctx := f.srv, context.Background()
	p := f.trojan(t)
	if _, err := s.Connect(ctx, connect.NewRequest(&lavrayv1.ConnectRequest{ProfileId: &p.ID})); err != nil {
		t.Fatal(err)
	}
	// Адрес «все интерфейсы» без пароля: SetSettings принимает, сборка конфига — нет.
	if err := setSetting(t, s, "inbound.address", strSetting("0.0.0.0")); err != nil {
		t.Fatal(err)
	}

	_, err := s.RestartProfile(ctx, connect.NewRequest(&lavrayv1.RestartProfileRequest{}))
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("RestartProfile с невалидным конфигом: ожидался INVALID_ARGUMENT, получено %v", err)
	}
	if st := s.inst.Status().State; st != inst.StateReady {
		t.Fatalf("после отказа соединение должно остаться активным, состояние = %v", st)
	}
	if f.runner.StopCalls != 0 || !f.runner.Running() {
		t.Fatalf("работающий инстанс остановлен: StopCalls=%d running=%v", f.runner.StopCalls, f.runner.Running())
	}
}

// Корректный перезапуск применяет НОВЫЙ конфиг (порт изменён после подключения).
func TestRestartProfileAppliesNewConfig(t *testing.T) {
	f := newSvcFixture(t, nil)
	s, ctx := f.srv, context.Background()
	p := f.trojan(t)
	if _, err := s.Connect(ctx, connect.NewRequest(&lavrayv1.ConnectRequest{ProfileId: &p.ID})); err != nil {
		t.Fatal(err)
	}
	if err := setSetting(t, s, "inbound.port", intSetting(2111)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RestartProfile(ctx, connect.NewRequest(&lavrayv1.RestartProfileRequest{})); err != nil {
		t.Fatalf("RestartProfile: %v", err)
	}
	if s.inst.Status().State != inst.StateReady || f.runner.StartedCounter != 2 || f.runner.StopCalls != 1 {
		t.Fatalf("ожидался один останов и второй старт: state=%v starts=%d stops=%d",
			s.inst.Status().State, f.runner.StartedCounter, f.runner.StopCalls)
	}
	if !bytes.Contains(f.runner.LastConfig(), []byte(`"listen_port": 2111`)) {
		t.Fatalf("новый конфиг не применён:\n%s", f.runner.LastConfig())
	}
}
