// SPDX-License-Identifier: GPL-3.0-only

package cfgbuild

import "testing"

// T-49: log.level из настроек доходит до конфига sing-box; неизвестное
// значение не должно ронять старт («unknown log level»), а заменяется на info.
func TestLogLevelFromSettings(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", "info"},
		{"info", "info"},
		{"trace", "trace"},
		{"debug", "debug"},
		{"warn", "warn"},
		{"error", "error"},
		{"fatal", "fatal"},
		{"panic", "panic"},
		{"WARN", "warn"},
		{"  Error ", "error"},
		{"verbose", "info"},
		{"warning", "info"},
		{`debug","output":"x`, "info"},
	}
	for _, c := range cases {
		st := DefaultSettings()
		st.LogLevel = c.in
		res, err := Build(BuildRequest{Profile: socksProfile(1, "1.2.3.4", 1080), Settings: st})
		if err != nil {
			t.Fatalf("Build(log.level=%q): %v", c.in, err)
		}
		logSection, _ := res.Config["log"].(map[string]any)
		if got := logSection["level"]; got != c.want {
			t.Errorf("log.level=%q → в конфиге %v, ожидался %q", c.in, got, c.want)
		}
		if len(logSection) != 1 {
			t.Errorf("секция log должна содержать только level: %v", logSection)
		}
	}
}
