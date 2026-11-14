// SPDX-License-Identifier: GPL-3.0-only

//go:build singbox_check

package cfgbuild

// singboxcheck_test.go — «sing-box check» для golden-конфигов (TEST-PLAN
// набор 2 / §3.2): конфиг парсится опциями форка и поднимается box.New
// (эквивалент cmd/sing-box check + libbox.CheckConfig), без запуска
// инстанса: box.New сразу закрывается.
//
// Без тега with_quic (например, голый `go test -tags singbox_check`) протоколы
// на QUIC (hysteria2/tuic) не скомпилированы в реестр — box.New возвращает
// «... is not included in this build»; такие конфиги считаются валидными на
// уровне парсера (ErrFeatureTagMissing), полная проверка — с тегами ядра.
//
// D-01: когда тест запущен со ВСЕМИ with_*-тегами из core/BUILD_TAGS,
// «is not included in this build» — провал, а не успех: иначе проверка молча
// урезалась бы (так G35a/G35c с неверным server_ports прошли бы незамеченными).
// Команда: go test -tags "singbox_check,$(tr -d '[:space:]' < BUILD_TAGS)" ...

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"

	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/common/srs"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/include"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/service"
)

// TestMain — перед прогонами генерирует минимальные валидные .srs для
// каждого rule-set-тега из golden-эталонов: конфиги ссылаются на локальные
// «geo/<kind>/<name>.srs» из поставки (LNB-31/36), и sing-box check честно
// открывает файлы. Каталог geo/ создаётся внутри пакета на время теста
// (в .gitignore) и удаляется после.
func TestMain(m *testing.M) {
	if err := generateGoldenGeo(); err != nil {
		fmt.Fprintln(os.Stderr, "geo fixtures:", err)
		os.Exit(1)
	}
	code := m.Run()
	_ = os.RemoveAll("geo")
	os.Exit(code)
}

// generateGoldenGeo — собирает теги (route.rule_set[].tag = "<kind>-<name>")
// из всех G*.want.json и пишет по одному минимальному .srs (SRS v1 — читается
// всеми версиями 1.14): geosite — domain_suffix, geoip — ip_cidr.
func generateGoldenGeo() error {
	tags := map[string]bool{}
	paths, err := filepath.Glob(filepath.Join("testdata", "golden", "G*.want.json"))
	if err != nil {
		return err
	}
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		var cfg struct {
			Route struct {
				RuleSet []struct {
					Tag string `json:"tag"`
				} `json:"rule_set"`
			} `json:"route"`
		}
		if err := json.Unmarshal(raw, &cfg); err != nil {
			continue // не-JSON эталона не бывает, но не падаем на регенерации
		}
		for _, rsDef := range cfg.Route.RuleSet {
			if rsDef.Tag != "" {
				tags[rsDef.Tag] = true
			}
		}
	}
	for tag := range tags {
		kind, name, ok := strings.Cut(tag, "-")
		if !ok || (kind != "geosite" && kind != "geoip") {
			return fmt.Errorf("неожидаемый rule-set тег %q", tag)
		}
		ruleJSON := `{"domain_suffix":["example.com"]}`
		if kind == "geoip" {
			ruleJSON = `{"ip_cidr":["192.0.2.0/24"]}`
		}
		var plain option.PlainRuleSet
		if err := json.Unmarshal([]byte(`{"rules":[`+ruleJSON+`]}`), &plain); err != nil {
			return err
		}
		dir := filepath.Join("geo", kind)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		f, err := os.Create(filepath.Join(dir, name+".srs"))
		if err != nil {
			return err
		}
		err = srs.Write(f, plain, C.RuleSetVersion1)
		f.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// ErrFeatureTagMissing — outbound требует сборочного тега (например
// with_quic для hysteria2/tuic): конфиг распарсен, полная инициализация
// требует `go test -tags with_quic`.
var ErrFeatureTagMissing = errors.New("cfgbuild: sing-box build tag missing for a protocol in this config")

// singBoxCheck — parse (option.Options + UnmarshallExcluded через реестры
// форка) + box.New + Close. Порядок — как cmd/sing-box check.
func singBoxCheck(configJSON string) error {
	ctx := service.ContextWithDefaultRegistry(context.Background())
	ctx = box.Context(ctx,
		include.InboundRegistry(),
		include.OutboundRegistry(),
		include.EndpointRegistry(),
		include.DNSTransportRegistry(),
		include.ServiceRegistry(),
		include.CertificateProviderRegistry(),
	)

	var options option.Options
	if err := options.UnmarshalJSONContext(ctx, []byte(configJSON)); err != nil {
		return fmt.Errorf("parse: %w", err)
	}

	ctxRun, cancel := context.WithCancel(service.ExtendContext(ctx))
	defer cancel()
	instance, err := box.New(box.Options{Context: ctxRun, Options: options})
	if err == nil {
		instance.Close()
		return nil
	}
	if strings.Contains(err.Error(), "is not included in this build") {
		return ErrFeatureTagMissing
	}
	return err
}

// buildTagsPath — единый список тегов ядра (cwd теста — каталог пакета).
const buildTagsPath = "../../BUILD_TAGS"

// featureTags — теги из BUILD_TAGS, подключающие протоколы/возможности
// sing-box (with_*); singbox_run влияет на раннер, не на реестры проверки.
func featureTags(buildTags string) []string {
	var out []string
	for _, tag := range strings.Split(strings.TrimSpace(buildTags), ",") {
		if tag = strings.TrimSpace(tag); strings.HasPrefix(tag, "with_") {
			out = append(out, tag)
		}
	}
	return out
}

// activeTags — теги, с которыми собран этот тестовый бинарь (go test -tags).
func activeTags() map[string]bool {
	active := map[string]bool{}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return active
	}
	for _, setting := range info.Settings {
		if setting.Key != "-tags" {
			continue
		}
		for _, tag := range strings.FieldsFunc(setting.Value, func(r rune) bool { return r == ',' || r == ' ' }) {
			active[tag] = true
		}
	}
	return active
}

// strictFeatureTags — тест запущен со всеми with_*-тегами из BUILD_TAGS
// (а список непустой): отсутствие тега в реестре тогда — реальный дефект.
func strictFeatureTags(required []string, active map[string]bool) bool {
	if len(required) == 0 {
		return false
	}
	for _, tag := range required {
		if !active[tag] {
			return false
		}
	}
	return true
}

func TestStrictFeatureTags(t *testing.T) {
	req := featureTags("singbox_run,with_quic,with_gvisor,with_utls\n")
	if want := []string{"with_quic", "with_gvisor", "with_utls"}; strings.Join(req, ",") != strings.Join(want, ",") {
		t.Fatalf("featureTags = %v, ждали %v", req, want)
	}
	all := map[string]bool{"singbox_check": true, "with_quic": true, "with_gvisor": true, "with_utls": true}
	if !strictFeatureTags(req, all) {
		t.Error("со всеми тегами BUILD_TAGS проверка обязана быть строгой")
	}
	if strictFeatureTags(req, map[string]bool{"singbox_check": true, "with_quic": true}) {
		t.Error("без with_gvisor/with_utls проверка не может быть строгой")
	}
	if strictFeatureTags(nil, all) {
		t.Error("пустой список тегов не включает строгий режим")
	}
}

// TestGoldenSingBoxCheck — каждый golden-эталон валиден для sing-box 1.14
// (парсинг опций форка + box.New). Без with_*-тегов из BUILD_TAGS QUIC-
// протоколы → ErrFeatureTagMissing (частичная проверка); со всеми — провал.
func TestGoldenSingBoxCheck(t *testing.T) {
	rawTags, err := os.ReadFile(buildTagsPath)
	if err != nil {
		t.Fatalf("core/BUILD_TAGS: %v", err)
	}
	required := featureTags(string(rawTags))
	strict := strictFeatureTags(required, activeTags())
	for _, inPath := range loadGolden(t) {
		name := filepath.Base(inPath)
		t.Run(name, func(t *testing.T) {
			wantPath := filepath.Join("testdata", "golden", name[:len(name)-len(".in.json")]+".want.json")
			raw, err := os.ReadFile(wantPath)
			if err != nil {
				t.Fatalf("эталон не найден: %v", err)
			}
			if err := singBoxCheck(string(raw)); err != nil {
				if errors.Is(err, ErrFeatureTagMissing) {
					if strict {
						t.Fatalf("%s: в сборке нет протокола при полном наборе тегов %v: %v", name, required, err)
					}
					t.Logf("%s: ok (parse; полная проверка — теги из core/BUILD_TAGS)", name)
					return
				}
				t.Fatalf("sing-box check: %v", err)
			}
		})
	}
}
