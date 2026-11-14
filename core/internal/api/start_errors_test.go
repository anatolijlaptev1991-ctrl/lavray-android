// SPDX-License-Identifier: GPL-3.0-only

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/inst"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/store"
)

// t55Server — сервер на заданном раннере; в группе 1 один SOCKS-профиль.
func t55Server(t *testing.T, runner inst.Runner) (*Server, int64) {
	t.Helper()
	dir, err := os.MkdirTemp("", "lavray-t55-*")
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = st.Close()
		for i := 0; i < 50; i++ {
			if os.RemoveAll(dir) == nil {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	})
	if err := st.CreateGroup(&model.Group{ID: 1, Name: "Default", Kind: model.GroupBasic}); err != nil {
		t.Fatal(err)
	}
	p := &model.Profile{GroupID: 1, Type: model.TypeSOCKS, Name: "t55", Address: "1.2.3.4", Port: 1080,
		Detail: &model.SocksHTTPDetail{Proto: "socks5"}}
	if err := st.CreateProfile(p); err != nil {
		t.Fatal(err)
	}
	return NewServer(Deps{Store: st, Runner: runner}), p.ID
}

func t55SetInboundPort(t *testing.T, s *Server, port int64) {
	t.Helper()
	// Порт 0 (= «нет mixed-in») SetSettings отвергает по T-34 (диапазон 1–65535),
	// поэтому такое значение пишется в хранилище напрямую, как его читает cfgbuild.
	if port == 0 {
		if err := s.st.SetSetting("inbound.port", "0"); err != nil {
			t.Fatal(err)
		}
		return
	}
	if _, err := s.SetSettings(context.Background(), connect.NewRequest(&lavrayv1.SetSettingsRequest{
		Values: map[string]*lavrayv1.SettingValue{"inbound.port": {Value: &lavrayv1.SettingValue_IntValue{IntValue: port}}},
	})); err != nil {
		t.Fatal(err)
	}
}

// T-55 (B1-36): порт входа занят — понятная ошибка вместо сырого bind-текста.
// Свободный порт занимает сам тест (порт 2080 NekoBox не трогаем); ошибку
// sing-box имитирует настоящая ошибка ОС в его цепочке обёрток.
func TestConnectExplainsBusyInboundPort(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	port := busy.Addr().(*net.TCPAddr).Port
	_, bindErr := net.Listen("tcp", busy.Addr().String())
	if bindErr == nil {
		t.Fatal("второй Listen на занятый адрес удался")
	}

	s, pid := t55Server(t, &inst.FakeRunner{StartErr: fmt.Errorf("singbox: старт: start inbound/mixed[mixed-in]: %w", bindErr)})
	t55SetInboundPort(t, s, int64(port))
	_, err = s.Connect(context.Background(), connect.NewRequest(&lavrayv1.ConnectRequest{ProfileId: int64p(pid)}))
	if connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("код ошибки %v, ожидался FAILED_PRECONDITION (err=%v)", connect.CodeOf(err), err)
	}
	for _, want := range []string{strconv.Itoa(port), "занят", "NekoBox", "Настройки → Сеть и прокси"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("в сообщении нет %q: %v", want, err)
		}
	}
	for _, raw := range []string{"bind", "socket address", "singbox:", "[internal]"} {
		if strings.Contains(err.Error(), raw) {
			t.Errorf("в сообщении осталась сырая часть %q: %v", raw, err)
		}
	}

	// Прочие ошибки старта остаются как были.
	s2, pid2 := t55Server(t, &inst.FakeRunner{StartErr: fmt.Errorf("singbox: старт: unknown outbound type")})
	if _, err := s2.Connect(context.Background(), connect.NewRequest(&lavrayv1.ConnectRequest{ProfileId: int64p(pid2)})); err == nil ||
		!strings.Contains(err.Error(), "unknown outbound type") {
		t.Fatalf("ожидалась исходная ошибка, получено: %v", err)
	}
}

// t55Inbounds — теги inbound'ов конфига, который получил раннер.
func t55Inbounds(t *testing.T, fr *inst.FakeRunner) (hasMixed, hasTun bool) {
	t.Helper()
	var cfg struct {
		Inbounds []struct {
			Tag string `json:"tag"`
		} `json:"inbounds"`
	}
	if err := json.Unmarshal(fr.LastConfig(), &cfg); err != nil {
		t.Fatal(err)
	}
	for _, in := range cfg.Inbounds {
		hasMixed = hasMixed || in.Tag == "mixed-in"
		hasTun = hasTun || in.Tag == "tun-in"
	}
	return
}

func t55Warned(warnings []string, prefix string) bool {
	for _, w := range warnings {
		if strings.HasPrefix(w, prefix) {
			return true
		}
	}
	return false
}

// T-55 (B1-21, D1-06): предупреждение TUN соответствует фактическому конфигу.
// Раньше при режиме TUN всегда сообщалось «tun_mixed_disabled», хотя в конфиге
// были и mixed-in, и tun-in.
func TestTunWarningMatchesBuiltConfig(t *testing.T) {
	fr := &inst.FakeRunner{}
	s, pid := t55Server(t, fr)
	ctx := context.Background()
	tun := lavrayv1.ConnectMode_CONNECT_MODE_TUN

	resp, err := s.Connect(ctx, connect.NewRequest(&lavrayv1.ConnectRequest{ProfileId: int64p(pid), Mode: tun}))
	if err != nil {
		t.Fatalf("Connect(TUN): %v", err)
	}
	if hasMixed, hasTun := t55Inbounds(t, fr); !hasMixed || !hasTun {
		t.Fatalf("предпосылка: в конфиге TUN ожидались mixed-in и tun-in (mixed=%v tun=%v)", hasMixed, hasTun)
	}
	if t55Warned(resp.Msg.Warnings, "tun_mixed_disabled") ||
		!t55Warned(resp.Msg.Warnings, "tun_mixed_enabled") || !strings.Contains(strings.Join(resp.Msg.Warnings, " "), "127.0.0.1:2080") {
		t.Errorf("предупреждения не соответствуют конфигу с mixed-in на 127.0.0.1:2080: %v", resp.Msg.Warnings)
	}

	// validate_only: те же предупреждения по тому же конфигу.
	if _, err := s.Disconnect(ctx, connect.NewRequest(&lavrayv1.DisconnectRequest{})); err != nil {
		t.Fatal(err)
	}
	dry, err := s.Connect(ctx, connect.NewRequest(&lavrayv1.ConnectRequest{ProfileId: int64p(pid), Mode: tun, ValidateOnly: boolp(true)}))
	if err != nil {
		t.Fatal(err)
	}
	if t55Warned(dry.Msg.Warnings, "tun_mixed_disabled") || !t55Warned(dry.Msg.Warnings, "tun_mixed_enabled") {
		t.Errorf("validate_only(TUN): предупреждения не соответствуют конфигу: %v", dry.Msg.Warnings)
	}

	// Нет mixed-in (inbound.port = 0) — предупреждать не о чем.
	t55SetInboundPort(t, s, 0)
	resp, err = s.Connect(ctx, connect.NewRequest(&lavrayv1.ConnectRequest{ProfileId: int64p(pid), Mode: tun}))
	if err != nil {
		t.Fatal(err)
	}
	if hasMixed, _ := t55Inbounds(t, fr); hasMixed {
		t.Fatal("предпосылка: при inbound.port=0 mixed-in быть не должно")
	}
	if t55Warned(resp.Msg.Warnings, "tun_mixed") {
		t.Errorf("лишнее предупреждение без mixed-in: %v", resp.Msg.Warnings)
	}
}
