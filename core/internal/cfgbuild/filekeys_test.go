// SPDX-License-Identifier: GPL-3.0-only

package cfgbuild

import (
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

// T-08 (A1-01): ключ, задающий путь к файлу, не должен попасть в итоговый
// конфиг ни через custom_config, ни через custom_outbound, ни через
// internal-full, ни через custom-outbound. Отказ — INVALID_ARGUMENT с путём
// ключа ("route.rule_set[0].path").

// custom_config → MergeCustomConfig: каждый случай отвергается, ключ в ошибке
// точный.
func TestCustomConfigRejectsFilePaths(t *testing.T) {
	cases := []struct {
		name    string
		custom  string
		wantKey string
	}{
		// --- главный вектор A1-01 ---
		{"cache_file.path (A1-01)", `{"experimental":{"cache_file":{"enabled":true,"path":"C:/Users/victim/Documents/diploma.docx"}}}`, "experimental.cache_file.path"},
		{"cache_file.Path другим регистром", `{"experimental":{"cache_file":{"Path":"C:/x"}}}`, "experimental.cache_file.Path"},
		{"cache_file.path+ с маркером", `{"experimental":{"cache_file":{"path+":["C:/x"]}}}`, "experimental.cache_file.path"},
		{"cache_file: неизвестное поле", `{"experimental":{"cache_file":{"foo":1}}}`, "experimental.cache_file.foo"},
		// --- route ---
		{"rule_set local path", `{"route":{"rule_set":[{"type":"local","tag":"y","format":"binary","path":"C:/Windows/win.ini"}]}}`, "route.rule_set[0].path"},
		{"rule_set +маркер", `{"route":{"+rule_set":[{"type":"local","tag":"y","path":"/etc/passwd"}]}}`, "route.rule_set[0].path"},
		{"rule_set remote initial_path", `{"route":{"rule_set":[{"type":"remote","tag":"y","url":"https://e.example/x.srs","initial_path":"C:/x"}]}}`, "route.rule_set[0].initial_path"},
		{"dhcp_lease_files", `{"route":{"dhcp_lease_files":["/var/lib/dhcp/dhcpd.leases"]}}`, "route.dhcp_lease_files"},
		{"geoip.path", `{"route":{"geoip":{"path":"C:/geoip.db"}}}`, "route.geoip.path"},
		{"dhcp_lease_fileſ (Unicode-эквивалент s)", "{\"route\":{\"dhcp_lease_file\u017f\":[\"/x\"]}}", "route.dhcp_lease_file\u017f"},
		// --- dns ---
		{"dns hosts path", `{"dns":{"servers":[{"tag":"h","type":"hosts","path":"C:/Windows/System32/drivers/etc/hosts"}]}}`, "dns.servers[0].path"},
		{"dns hosts path + двойной type", `{"dns":{"servers":[{"tag":"h","type":"hosts","TYPE":"https","path":"C:/x"}]}}`, "dns.servers[0].path"},
		{"dns tls.certificate_path", `{"dns":{"servers":[{"tag":"d","type":"tls","server":"1.1.1.1","tls":{"certificate_path":"C:/ca.pem"}}]}}`, "dns.servers[0].tls.certificate_path"},
		// --- outbounds своего тега ---
		{"outbound tls.certificate_path", `{"outbounds":[{"tag":"proxy","tls":{"certificate_path":"C:/ca.pem"}}]}`, "outbounds[0].tls.certificate_path"},
		{"outbound tls.KEY_PATH", `{"outbounds":[{"tag":"proxy","tls":{"KEY_PATH":"C:/k.pem"}}]}`, "outbounds[0].tls.KEY_PATH"},
		{"outbound tls.ech.key_path", `{"outbounds":[{"tag":"proxy","tls":{"ech":{"key_path":"C:/k"}}}]}`, "outbounds[0].tls.ech.key_path"},
		{"outbound знак Кельвина в key_path", "{\"outbounds\":[{\"tag\":\"proxy\",\"tls\":{\"\u212aey_path\":\"C:/k\"}}]}", "outbounds[0].tls.\u212aey_path"},
		{"outbound private_key_path (ssh)", `{"outbounds":[{"tag":"proxy","type":"ssh","private_key_path":"C:/id_rsa"}]}`, "outbounds[0].private_key_path"},
		{"outbound executable_path (tor)", `{"outbounds":[{"tag":"proxy","type":"tor","executable_path":"C:/Windows/System32/calc.exe"}]}`, "outbounds[0].executable_path"},
		{"outbound path вне transport", `{"outbounds":[{"tag":"proxy","path":"C:/x"}]}`, "outbounds[0].path"},
		{"outbound netns.path", `{"outbounds":[{"tag":"proxy","netns":{"path":"/var/run/netns/x"}}]}`, "outbounds[0].netns.path"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := baseConfig(t)
			err := MergeCustomConfig(cfg, tc.custom, []string{"proxy"})
			wantInvalidArgument(t, err, tc.wantKey)
		})
	}
}

// Допустимое остаётся допустимым: URL-пути транспортов и DNS-over-HTTPS,
// process_path как условие правила, пустые значения, булев tcp_multi_path,
// безопасные поля cache_file.
func TestCustomConfigAllowsNonFileKeys(t *testing.T) {
	cases := []struct{ name, custom string }{
		{"transport.path (ws)", `{"outbounds":[{"tag":"proxy","transport":{"type":"ws","path":"/ws"}}]}`},
		{"transport.path без type", `{"outbounds":[{"tag":"proxy","transport":{"path":"/ws"}}]}`},
		{"dns https path", `{"dns":{"servers":[{"tag":"d","type":"https","server":"dns.example","path":"/dns-query"}]}}`},
		{"dns h3 path", `{"dns":{"servers":[{"tag":"d","type":"h3","server":"dns.example","path":"/dns-query"}]}}`},
		{"process_path в правиле", `{"route":{"rules":[{"process_path":["C:/Games/game.exe"],"outbound":"direct"}]}}`},
		{"tcp_multi_path bool", `{"outbounds":[{"tag":"proxy","tcp_multi_path":true}]}`},
		{"пустой certificate_path", `{"outbounds":[{"tag":"proxy","tls":{"certificate_path":""}}]}`},
		{"cache_file безопасные поля", `{"experimental":{"cache_file":{"enabled":true,"cache_id":"lav","store_fakeip":true,"store_rdrc":true,"rdrc_timeout":"7d"}}}`},
		{"сертификат внутри конфига", `{"outbounds":[{"tag":"proxy","tls":{"certificate":["-----BEGIN CERTIFICATE-----","MIIB","-----END CERTIFICATE-----"]}}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := baseConfig(t)
			if err := MergeCustomConfig(cfg, tc.custom, []string{"proxy"}); err != nil {
				t.Fatalf("допустимый custom_config отклонён: %v", err)
			}
		})
	}
}

// После допустимого мержа cache_file путь остаётся тем, что задал LAVRay.
func TestCustomConfigKeepsGeneratedCacheFilePath(t *testing.T) {
	p := socksProfile(1, "1.2.3.4", 1080)
	p.CustomConfigJSON = `{"experimental":{"cache_file":{"enabled":true,"cache_id":"lav"}}}`
	p.CustomFieldsConfirmed = true
	st := DefaultSettings()
	st.CacheFilePath = "C:/Users/me/AppData/Roaming/LAVRay/cache.db"
	res, err := Build(BuildRequest{Profile: p, Settings: st})
	if err != nil {
		t.Fatal(err)
	}
	cf := res.Config["experimental"].(map[string]any)["cache_file"].(map[string]any)
	if cf["path"] != st.CacheFilePath || cf["cache_id"] != "lav" {
		t.Fatalf("cache_file = %v, путь должен остаться %q", cf, st.CacheFilePath)
	}
}

// Сценарий A1-01 целиком на уровне Build: подтверждённый профиль с чужим
// путём не собирается; неподтверждённый — конфиг строится без этого пути.
func TestBuildRefusesCacheFilePathFromCustomConfig(t *testing.T) {
	const evil = `{"experimental":{"cache_file":{"enabled":true,"path":"C:/Users/victim/Documents/diploma.docx"}}}`

	p := socksProfile(1, "1.2.3.4", 1080)
	p.CustomConfigJSON = evil
	p.CustomFieldsConfirmed = true
	res, err := Build(BuildRequest{Profile: p, Settings: DefaultSettings()})
	if res != nil {
		t.Fatalf("конфиг с путём к чужому файлу собран: %v", res.Config["experimental"])
	}
	wantInvalidArgument(t, err, "experimental.cache_file.path")
	if e, _ := AsError(err); e.Code != connect.CodeInvalidArgument {
		t.Fatalf("код = %v", e.Code)
	}

	p.CustomFieldsConfirmed = false
	res, err = Build(BuildRequest{Profile: p, Settings: DefaultSettings()})
	if err != nil {
		t.Fatalf("неподтверждённые поля не применяются и не должны давать ошибку: %v", err)
	}
	if strings.Contains(string(mustMarshal(t, res.Config)), "diploma.docx") {
		t.Fatal("путь из неподтверждённого custom_config попал в конфиг")
	}
}

// custom_outbound_json профиля мержится в outbound без allowlist: пути
// отвергаются тем же обходом.
func TestBuildRefusesFilePathInCustomOutbound(t *testing.T) {
	p := socksProfile(1, "1.2.3.4", 1080)
	p.CustomOutboundJSON = `{"tls":{"certificate_path":"C:/ca.pem"}}`
	p.CustomFieldsConfirmed = true
	_, err := Build(BuildRequest{Profile: p})
	wantInvalidArgument(t, err, "custom_outbound.tls.certificate_path")

	p.CustomOutboundJSON = `{"transport":{"type":"ws","path":"/ws"}}`
	if _, err := Build(BuildRequest{Profile: p}); err != nil {
		t.Fatalf("URL-путь транспорта отклонён: %v", err)
	}
}

// internal-full — конфиг «как есть», но без путей: log.output,
// experimental.cache_file.path, rule_set[].path.
func TestBuildRefusesFilePathInInternalFull(t *testing.T) {
	cases := []struct{ name, full, wantKey string }{
		{"log.output", `{"log":{"level":"warn","output":"C:/Users/me/Startup/x.bat"},"outbounds":[{"type":"direct","tag":"x"}]}`, "log.output"},
		{"cache_file.path", `{"experimental":{"cache_file":{"enabled":true,"path":"C:/x"}},"outbounds":[{"type":"direct","tag":"x"}]}`, "experimental.cache_file.path"},
		{"clash_api.external_ui", `{"experimental":{"clash_api":{"external_ui":"C:/ui"}},"outbounds":[{"type":"direct","tag":"x"}]}`, "experimental.clash_api.external_ui"},
		{"rule_set local", `{"route":{"rule_set":[{"type":"local","tag":"r","path":"C:/r.srs"}]},"outbounds":[{"type":"direct","tag":"x"}]}`, "route.rule_set[0].path"},
		{"inbound tls.key_path", `{"inbounds":[{"type":"mixed","tag":"in","tls":{"key_path":"C:/k.pem"}}],"outbounds":[{"type":"direct","tag":"x"}]}`, "inbounds[0].tls.key_path"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := &model.Profile{
				ID: 6, Type: model.TypeCustomConfig, Address: "n/a", Port: 1,
				Detail:                &model.CustomDetail{Core: "internal-full", FullConfigJSON: tc.full},
				CustomFieldsConfirmed: true,
			}
			_, err := Build(BuildRequest{Profile: p})
			wantInvalidArgument(t, err, tc.wantKey)
		})
	}
}

// custom-outbound (core=internal) целиком задаёт outbound: tor с
// executable_path запускал бы любую программу.
func TestBuildRefusesFilePathInCustomInternalOutbound(t *testing.T) {
	p := &model.Profile{
		ID: 5, Type: model.TypeCustomOutbound, Address: "ignored", Port: 1,
		Detail: &model.CustomDetail{
			Core:         "internal",
			OutboundJSON: `{"type":"tor","executable_path":"C:/Windows/System32/calc.exe"}`,
		},
	}
	_, err := Build(BuildRequest{Profile: p})
	wantInvalidArgument(t, err, "custom.executable_path")
}

func mustMarshal(t *testing.T, cfg map[string]any) []byte {
	t.Helper()
	b, err := MarshalIndent(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
