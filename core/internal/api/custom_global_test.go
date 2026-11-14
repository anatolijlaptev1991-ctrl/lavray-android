// SPDX-License-Identifier: GPL-3.0-only

package api

import (
	"encoding/json"
	"strings"
	"testing"

	"connectrpc.com/connect"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/logs"
)

// Устаревшая настройка routing.custom_global с негодным значением (оставшимся
// от старой установки) не мешает подключению: Connect успешен, причина — в
// предупреждениях ответа и в журнале ядра.
func TestConnectIgnoresInvalidLegacyCustomGlobal(t *testing.T) {
	for _, raw := range []string{`{"foo":1}`, `{"rules":[1]}`, `{"rules":[{"domain":["` + strings.Repeat("a", 300<<10) + `"]}]}`} {
		f := newSvcFixture(t, nil)
		encoded, _ := json.Marshal(raw)
		if err := f.srv.st.SetSetting("routing.custom_global", string(encoded)); err != nil {
			t.Fatal(err)
		}
		p := f.trojan(t)
		res, err := f.srv.Connect(bg, connect.NewRequest(&lavrayv1.ConnectRequest{ProfileId: &p.ID}))
		if err != nil {
			t.Fatalf("Connect при негодной routing.custom_global (%.30q): %v", raw, err)
		}
		found := false
		for _, w := range res.Msg.Warnings {
			found = found || strings.Contains(w, "routing.custom_global")
		}
		if !found {
			t.Errorf("предупреждений ответа нет: %v", res.Msg.Warnings)
		}
		logged := false
		for _, e := range f.srv.logs.Tail(100) {
			logged = logged || (e.Level == logs.LevelWarn && strings.Contains(e.Message, "routing.custom_global"))
		}
		if !logged {
			t.Error("в журнале нет предупреждения")
		}
	}
}
