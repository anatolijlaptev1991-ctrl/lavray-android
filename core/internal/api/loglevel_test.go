// SPDX-License-Identifier: GPL-3.0-only

package api

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/inst"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/store"
)

// T-49: настройка log.level доходит до конфига sing-box (раньше уровень был
// всегда info, а ключ не принимался SetSettings). Порт 2080 только пишется в
// конфиг: раннер-заглушка ничего не слушает.
func TestLogLevelSettingReachesRunnerConfig(t *testing.T) {
	dir, err := os.MkdirTemp("", "lavray-t49-*")
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
	p := &model.Profile{GroupID: 1, Type: model.TypeSOCKS, Name: "t49", Address: "1.2.3.4", Port: 1080,
		Detail: &model.SocksHTTPDetail{Proto: "socks5"}}
	if err := st.CreateProfile(p); err != nil {
		t.Fatal(err)
	}
	fr := &inst.FakeRunner{}
	s := NewServer(Deps{Store: st, Runner: fr})
	ctx := context.Background()

	connectAndLevel := func() string {
		t.Helper()
		if _, err := s.Connect(ctx, connect.NewRequest(&lavrayv1.ConnectRequest{ProfileId: int64p(p.ID)})); err != nil {
			t.Fatalf("Connect: %v", err)
		}
		var cfg struct {
			Log struct {
				Level string `json:"level"`
			} `json:"log"`
		}
		if err := json.Unmarshal(fr.LastConfig(), &cfg); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Disconnect(ctx, connect.NewRequest(&lavrayv1.DisconnectRequest{})); err != nil {
			t.Fatalf("Disconnect: %v", err)
		}
		return cfg.Log.Level
	}

	if got := connectAndLevel(); got != "info" {
		t.Fatalf("уровень по умолчанию = %q, ожидался info", got)
	}
	resp, err := s.SetSettings(ctx, connect.NewRequest(&lavrayv1.SetSettingsRequest{
		Values: map[string]*lavrayv1.SettingValue{
			"log.level": {Value: &lavrayv1.SettingValue_StringValue{StringValue: "debug"}},
		},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Msg.AcceptedKeys) != 1 || resp.Msg.AcceptedKeys[0] != "log.level" {
		t.Fatalf("log.level не принят SetSettings (нет в model.SettingDescriptors): accepted=%v", resp.Msg.AcceptedKeys)
	}
	if got := connectAndLevel(); got != "debug" {
		t.Fatalf("после SetSettings(log.level=debug) в конфиге %q", got)
	}
}
