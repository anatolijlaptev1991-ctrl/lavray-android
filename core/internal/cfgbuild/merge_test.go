// SPDX-License-Identifier: GPL-3.0-only

package cfgbuild

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"connectrpc.com/connect"
)

// baseConfig — минимальный каркас для мерж-тестов.
func baseConfig(t *testing.T) map[string]any {
	t.Helper()
	var cfg map[string]any
	if err := json.Unmarshal([]byte(`{
	  "log": {"level": "info"},
	  "dns": {"servers": [{"tag": "dns-local", "type": "local"}]},
	  "route": {"rules": [{"outbound": "block", "domain": ["ads.example"]}]},
	  "outbounds": [
	    {"type": "socks", "tag": "proxy", "server": "1.2.3.4", "server_port": 1080},
	    {"type": "direct", "tag": "direct"}
	  ],
	  "experimental": {"cache_file": {"enabled": false}}
	}`), &cfg); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func wantInvalidArgument(t *testing.T, err error, key string) {
	t.Helper()
	if err == nil {
		t.Fatalf("ожидалась ошибка INVALID_ARGUMENT для ключа %q, получен nil", key)
	}
	e, ok := AsError(err)
	if !ok {
		t.Fatalf("ожидался *cfgbuild.Error, получен %T: %v", err, err)
	}
	if e.Code != connect.CodeInvalidArgument {
		t.Fatalf("код: want INVALID_ARGUMENT, got %v (%v)", e.Code, err)
	}
	if e.Key != key {
		t.Fatalf("ключ в ошибке: want %q, got %q (err: %v)", key, e.Key, err)
	}
	if !strings.Contains(err.Error(), key) {
		t.Fatalf("сообщение ошибки не содержит имя ключа %q: %v", key, err)
	}
}

// ---- запрещённые ключи: каждый ловится (§3.1.1) ----------------------------

func TestAllowlistForbiddenKeys(t *testing.T) {
	cases := []struct {
		name    string
		custom  string
		wantKey string
	}{
		{"inbounds", `{"inbounds": [{"type": "mixed"}]}`, "inbounds"},
		{"inbounds с +", `{"+inbounds": [{"type": "mixed"}]}`, "inbounds"},
		{"log.output", `{"log": {"level": "debug", "output": "/tmp/x.log"}}`, "log.output"},
		{"experimental.clash_api", `{"experimental": {"clash_api": {"external_controller": "0.0.0.0:9090"}}}`, "experimental.clash_api"},
		{"experimental.clash_api.external_ui_download_url", `{"experimental": {"clash_api": {"external_ui_download_url": "http://evil/"}}}`, "experimental.clash_api"},
		{"experimental.v2ray_api", `{"experimental": {"v2ray_api": {"listen": "127.0.0.1:8080"}}}`, "experimental.v2ray_api"},
		{"experimental.debug", `{"experimental": {"debug": {"gc": true}}}`, "experimental.debug"},
		{"неизвестный корневой ключ", `{"endpoints": []}`, "endpoints"},
		{"raw TTL-подобный ключ", `{"rlay": {}}`, "rlay"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := baseConfig(t)
			err := MergeCustomConfig(cfg, tc.custom, []string{"proxy"})
			wantInvalidArgument(t, err, tc.wantKey)
		})
	}
}

// ---- разрешённые ключи: каждый проходит ------------------------------------

func TestAllowlistAllowedKeys(t *testing.T) {
	cases := []struct {
		name   string
		custom string
		check  func(t *testing.T, cfg map[string]any)
	}{
		{
			"log.level",
			`{"log": {"level": "debug"}}`,
			func(t *testing.T, cfg map[string]any) {
				log := cfg["log"].(map[string]any)
				if log["level"] != "debug" {
					t.Fatalf("log.level = %v", log["level"])
				}
			},
		},
		{
			"route.rules перезапись",
			`{"route": {"rules": [{"outbound": "direct", "domain": ["a.ru"]}]}}`,
			func(t *testing.T, cfg map[string]any) {
				route := cfg["route"].(map[string]any)
				rules := route["rules"].([]any)
				if len(rules) != 1 {
					t.Fatalf("route.rules = %v", rules)
				}
			},
		},
		{
			// ConfigBuilder.cpp: "+key" → QJSONARRAY_ADD(src, dst): src-элементы ПЕРВЫМИ.
			"route +rules конкатенация (src в начало)",
			`{"route": {"+rules": [{"outbound": "direct", "domain": ["b.ru"]}]}}`,
			func(t *testing.T, cfg map[string]any) {
				rules := cfg["route"].(map[string]any)["rules"].([]any)
				if len(rules) != 2 {
					t.Fatalf("route.rules после +rules = %v", rules)
				}
				first := rules[0].(map[string]any)
				dom := first["domain"].([]any)
				if dom[0] != "b.ru" {
					t.Fatalf("первым должен быть src-элемент: %v", rules)
				}
			},
		},
		{
			"route rules+ конкатенация (src в конец)",
			`{"route": {"rules+": [{"outbound": "direct", "domain": ["c.ru"]}]}}`,
			func(t *testing.T, cfg map[string]any) {
				rules := cfg["route"].(map[string]any)["rules"].([]any)
				if len(rules) != 2 {
					t.Fatalf("route.rules после rules+ = %v", rules)
				}
			},
		},
		{
			"dns.servers добавление",
			`{"dns": {"servers": [{"tag": "dns-remote", "type": "udp", "server": "8.8.8.8"}]}}`,
			func(t *testing.T, cfg map[string]any) {
				servers := cfg["dns"].(map[string]any)["servers"].([]any)
				if len(servers) != 1 {
					t.Fatalf("dns.servers перезаписываются массивом (семантика MergeJson): %v", servers)
				}
			},
		},
		{
			"experimental.cache_file",
			`{"experimental": {"cache_file": {"enabled": true, "cache_id": "lav"}}}`,
			func(t *testing.T, cfg map[string]any) {
				exp := cfg["experimental"].(map[string]any)
				cf := exp["cache_file"].(map[string]any)
				if cf["enabled"] != true {
					t.Fatalf("cache_file = %v", cf)
				}
			},
		},
		{
			"outbounds собственный тег (мерж по тегу)",
			`{"outbounds": [{"tag": "proxy", "domain_resolver": "dns-local"}]}`,
			func(t *testing.T, cfg map[string]any) {
				outs := cfg["outbounds"].([]any)
				if len(outs) != 2 {
					t.Fatalf("outbounds = %v", outs)
				}
				proxy := outs[0].(map[string]any)
				if proxy["type"] != "socks" {
					t.Fatalf("мерж по тегу не сохранил type: %v", proxy)
				}
				if _, ok := proxy["domain_resolver"]; !ok {
					t.Fatalf("мерж по тегу не добавил поле: %v", proxy)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := baseConfig(t)
			if err := MergeCustomConfig(cfg, tc.custom, []string{"proxy"}); err != nil {
				t.Fatalf("разрешённый мерж отклонён: %v", err)
			}
			tc.check(t, cfg)
		})
	}
}

// ---- outbounds в custom_config: только собственный тег -----------------------

func TestAllowlistOutboundsForeignTag(t *testing.T) {
	cfg := baseConfig(t)
	err := MergeCustomConfig(cfg, `{"outbounds": [{"tag": "direct", "type": "block"}]}`, []string{"proxy"})
	wantInvalidArgument(t, err, "outbounds")

	cfg = baseConfig(t)
	err = MergeCustomConfig(cfg, `{"outbounds": [{"type": "socks"}]}`, []string{"proxy"})
	wantInvalidArgument(t, err, "outbounds[0]")
}

// ---- семантика MergeJSON (порт MergeJson) -----------------------------------

func TestMergeJSONSemantics(t *testing.T) {
	// "+key": dst-элементы в конец; "key+": src-элементы в конец.
	dst := map[string]any{"list": []any{"a"}}
	MergeJSON(dst, map[string]any{"+list": []any{"b"}})
	if got := fmtSlice(dst["list"]); got != "b,a" {
		t.Fatalf("+list = %q", got)
	}

	dst = map[string]any{"list": []any{"a"}}
	MergeJSON(dst, map[string]any{"list+": []any{"b"}})
	if got := fmtSlice(dst["list"]); got != "a,b" {
		t.Fatalf("list+ = %q", got)
	}

	// Новый массив без маркера — просто присваивается.
	dst = map[string]any{}
	MergeJSON(dst, map[string]any{"fresh": []any{1}})
	if got := fmtSlice(dst["fresh"]); got != "1" {
		t.Fatalf("fresh = %q", got)
	}

	// Массив при наличии ключа в dst — перезапись, НЕ конкатенация
	// (ветка dst.contains(key) в оракуле).
	dst = map[string]any{"list": []any{"a"}}
	MergeJSON(dst, map[string]any{"list": []any{"b"}})
	if got := fmtSlice(dst["list"]); got != "b" {
		t.Fatalf("list перезапись = %q", got)
	}

	// Объекты — рекурсивно; скаляры — перезапись.
	dst = map[string]any{"o": map[string]any{"x": 1, "keep": true}}
	MergeJSON(dst, map[string]any{"o": map[string]any{"x": 2}})
	o := dst["o"].(map[string]any)
	if fmt.Sprint(o["x"]) != "2" || o["keep"] != true {
		t.Fatalf("рекурсивный мерж объекта: %v", o)
	}
}

func fmtSlice(v any) string {
	arr, _ := v.([]any)
	parts := make([]string, len(arr))
	for i, e := range arr {
		parts[i] = toString(e)
	}
	return strings.Join(parts, ",")
}

func toString(v any) string {
	if s, ok := v.(string); ok {
		return s // без JSON-кавычек — сравнение "b,a" читаемо
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "?"
	}
	return string(b)
}

// Пустой custom_config — no-op.
func TestMergeCustomConfigEmpty(t *testing.T) {
	cfg := baseConfig(t)
	before, _ := json.Marshal(cfg)
	for _, s := range []string{"", "   ", "{}"} {
		if err := MergeCustomConfig(cfg, s, nil); err != nil {
			t.Fatalf("пустой custom_config %q: %v", s, err)
		}
	}
	after, _ := json.Marshal(cfg)
	if string(before) != string(after) {
		t.Fatal("пустой custom_config изменил конфиг")
	}
}

// Невалидный JSON — INVALID_ARGUMENT.
func TestMergeCustomConfigInvalidJSON(t *testing.T) {
	cfg := baseConfig(t)
	err := MergeCustomConfig(cfg, `{"route": `, nil)
	wantInvalidArgument(t, err, "custom_config")
}
