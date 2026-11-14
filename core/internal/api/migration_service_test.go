// SPDX-License-Identifier: GPL-3.0-only

package api

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/store/nekoimport"
)

// Переносимая установка рядом с запущенным NekoBox и каталоги в профиле (P).
func TestDetectNekoBoxFindsPortableAndAppdata(t *testing.T) {
	base := mkNekoTemp(t)
	install := filepath.Join(base, "install") // как переносимая установка: <install>/config
	copyNekoFixtureInto(t, filepath.Join(install, "config"))
	appdata := mkNekoTemp(t)
	if err := os.MkdirAll(filepath.Join(appdata, "nekoray", "config", "groups"), 0o755); err != nil {
		t.Fatal(err)
	}
	f := newMigrationFixture(t, &nekoimport.Env{
		AppData: appdata,
		Processes: func() ([]nekoimport.Process, error) {
			return []nekoimport.Process{{Name: "nekobox.exe", ExeDir: install}}, nil
		},
	})
	res, err := f.migrate.DetectNekoBox(context.Background(), connect.NewRequest(&lavrayv1.DetectNekoBoxRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]*lavrayv1.NekoBoxCandidate{}
	for _, c := range res.Msg.Candidates {
		got[c.Source] = c
	}
	p, a := got["process"], got["appdata"]
	if p == nil || a == nil || len(got) != 2 {
		t.Fatalf("кандидаты: %v", res.Msg.Candidates)
	}
	if p.Groups != 2 || p.Profiles != 9 || p.Routes != 2 || !p.LooksRunning {
		t.Errorf("переносимая установка: %v", p)
	}
	if !a.LooksRunning {
		// Режим -appdata: exe лежит в другом месте, путь процесса с config не
		// сопоставить, поэтому запущенный NekoBox даёт предупреждение и здесь.
		t.Errorf("appdata не помечен запущенным при работающем NekoBox: %v", a)
	}
	// Ручной путь с относительным именем игнорируется, абсолютный дубль не удваивает.
	res, err = f.migrate.DetectNekoBox(context.Background(), connect.NewRequest(&lavrayv1.DetectNekoBoxRequest{
		ExtraPaths: []string{"relative", filepath.Join(install, "config")},
	}))
	if err != nil || len(res.Msg.Candidates) != 2 {
		t.Fatalf("после extra_paths: %v %v", res, err)
	}
}

// Предпросмотр не пишет, а его числа совпадают с отчётом переноса (D).
func TestPreviewMatchesImport(t *testing.T) {
	f := newMigrationFixture(t, nil)
	dir := copyNekoFixture(t)
	req := &lavrayv1.NekoBoxImportRequest{Path: dir, IncludeRoutes: true, IncludeSettings: true}
	before := profileCount(t, f.st)

	preview := f.previewNeko(t, req)
	if !preview.DryRun || profileCount(t, f.st) != before {
		t.Fatalf("предпросмотр записал данные или не помечен dry_run: %v", preview)
	}
	if p, _ := f.srv.presetRepo.List(); len(p) != 0 {
		t.Fatalf("предпросмотр записал наборы: %d", len(p))
	}
	real := f.importNeko(t, req)
	if real.DryRun {
		t.Error("отчёт переноса помечен dry_run")
	}
	real.DryRun = true
	if !proto.Equal(preview, real) {
		t.Errorf("предпросмотр:\n%v\nперенос:\n%v", preview, real)
	}
	if real.GroupsImported != 2 || real.RoutesImported != 2 || len(real.Skipped) != 2 {
		t.Errorf("отчёт: %v", real)
	}
	if profileCount(t, f.st) != before+7 {
		t.Errorf("профилей %d, ожидалось %d", profileCount(t, f.st), before+7)
	}
}

// Маршруты становятся наборами custom:neko-<slug>; повтор не создаёт дублей;
// активный маршрут NekoBox указывает на набор.
func TestImportRoutesCreatesPreset(t *testing.T) {
	f := newMigrationFixture(t, nil)
	dir := copyNekoFixture(t)
	req := &lavrayv1.NekoBoxImportRequest{Path: dir, IncludeRoutes: true}
	rep := f.importNeko(t, req)
	if rep.RoutesFound != 2 || rep.RoutesImported != 2 {
		t.Fatalf("маршруты: %v", rep)
	}
	const want = "custom:neko-ru-bypass-games"
	if rep.NekoboxActiveRoute == nil || *rep.NekoboxActiveRoute != want {
		t.Fatalf("активный маршрут: %v, ожидался %s", rep.NekoboxActiveRoute, want)
	}
	got, err := f.routing.GetPreset(context.Background(), connect.NewRequest(&lavrayv1.GetPresetRequest{Id: want}))
	if err != nil {
		t.Fatalf("набор %s: %v", want, err)
	}
	if got.Msg.Builtin || got.Msg.Revision != 1 || len(got.Msg.DirectDomain) != 3 {
		t.Errorf("набор: %v", got.Msg)
	}
	again := f.importNeko(t, req)
	if again.RoutesImported != 0 || again.GroupsImported != 0 || again.AlreadyPresent != 11 { // 2 группы + 7 профилей + 2 набора
		t.Errorf("повторный перенос: %v", again)
	}
	list, err := f.srv.presetRepo.List()
	if err != nil || len(list) != 2 {
		t.Errorf("наборов %d (%v), ожидалось 2", len(list), err)
	}
	if again.NekoboxActiveRoute == nil || *again.NekoboxActiveRoute != want {
		t.Errorf("активный маршрут после повтора: %v", again.NekoboxActiveRoute)
	}
}

// Без include_routes наборов не создаётся, но найденные маршруты считаются (E).
func TestImportWithoutRoutes(t *testing.T) {
	f := newMigrationFixture(t, nil)
	rep := f.importNeko(t, &lavrayv1.NekoBoxImportRequest{Path: copyNekoFixture(t)})
	if rep.RoutesFound != 2 || rep.RoutesImported != 0 || rep.NekoboxActiveRoute != nil {
		t.Errorf("отчёт: %v", rep)
	}
	if list, _ := f.srv.presetRepo.List(); len(list) != 0 {
		t.Errorf("созданы наборы: %d", len(list))
	}
	if len(rep.SettingsImported) != 0 {
		t.Errorf("настройки перенесены без include_settings: %v", rep.SettingsImported)
	}
}

// Настройки проходят валидатор SetSettings: допустимые пишутся, остальные — в предупреждения.
func TestImportSettingsValidated(t *testing.T) {
	f := newMigrationFixture(t, nil)
	dir := copyNekoFixture(t)
	if err := os.WriteFile(filepath.Join(dir, "groups", "nekobox.json"),
		[]byte(`{"inbound_socks_port":3090,"vpn_mtu":99999,"mux_protocol":"quic","sub_use_proxy":false,"remember_id":3}`), 0o644); err != nil {
		t.Fatal(err)
	}
	rep := f.importNeko(t, &lavrayv1.NekoBoxImportRequest{Path: dir, IncludeSettings: true})
	want := []string{"inbound.port", "remember.id", "sub.use_proxy"}
	got := append([]string(nil), rep.SettingsImported...)
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("перенесены %v, ожидалось %v", got, want)
	}
	joined := strings.Join(rep.Warnings, "\n")
	for _, w := range []string{"tun.mtu", "mux.protocol"} {
		if !strings.Contains(joined, "настройка "+w+" не перенесена") {
			t.Errorf("нет предупреждения про %s:\n%s", w, joined)
		}
	}
	res, err := f.settings.GetSettings(context.Background(), connect.NewRequest(&lavrayv1.GetSettingsRequest{Keys: []string{"inbound.port", "remember.id"}}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Msg.Values["inbound.port"].GetIntValue() != 3090 || res.Msg.Values["remember.id"].GetIntValue() != 3 {
		t.Errorf("значения: %v", res.Msg.Values)
	}
}

// Пути: пустой, относительный, файл, каталог без groups/profiles → INVALID_ARGUMENT;
// нет пути → NOT_FOUND (I).
func TestPathValidation(t *testing.T) {
	f := newMigrationFixture(t, nil)
	file := filepath.Join(mkNekoTemp(t), "file.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, path string
		code       connect.Code
	}{
		{"пустой", "", connect.CodeInvalidArgument},
		{"пробелы", "   ", connect.CodeInvalidArgument},
		{"относительный", filepath.Join("a", "b"), connect.CodeInvalidArgument},
		{"файл", file, connect.CodeInvalidArgument},
		{"пустой каталог", mkNekoTemp(t), connect.CodeInvalidArgument},
		{"нет пути", filepath.Join(mkNekoTemp(t), "нет"), connect.CodeNotFound},
	}
	for _, c := range cases {
		for _, call := range []string{"preview", "import"} {
			req := connect.NewRequest(&lavrayv1.NekoBoxImportRequest{Path: c.path})
			var err error
			if call == "preview" {
				_, err = f.migrate.PreviewNekoBoxImport(context.Background(), req)
			} else {
				_, err = f.migrate.ImportNekoBox(context.Background(), req)
			}
			if connect.CodeOf(err) != c.code {
				t.Errorf("%s/%s: код %v (%v), ожидался %v", c.name, call, connect.CodeOf(err), err, c.code)
			}
		}
	}
}

// Каталог установки с вложенным config принимается.
func TestImportAcceptsInstallDir(t *testing.T) {
	f := newMigrationFixture(t, nil)
	install := filepath.Join(mkNekoTemp(t), "install")
	copyNekoFixtureInto(t, filepath.Join(install, "config"))
	if rep := f.importNeko(t, &lavrayv1.NekoBoxImportRequest{Path: install}); rep.GroupsImported != 2 {
		t.Errorf("отчёт: %v", rep)
	}
}

// Параллельный перенос → ABORTED (C).
func TestConcurrentImportAborted(t *testing.T) {
	f := newMigrationFixture(t, nil)
	req := &lavrayv1.NekoBoxImportRequest{Path: copyNekoFixture(t)}
	f.srv.migration.busy.Lock()
	for name, call := range map[string]func() error{
		"preview": func() error {
			_, err := f.migrate.PreviewNekoBoxImport(context.Background(), connect.NewRequest(req))
			return err
		},
		"import": func() error {
			_, err := f.migrate.ImportNekoBox(context.Background(), connect.NewRequest(req))
			return err
		},
	} {
		if err := call(); connect.CodeOf(err) != connect.CodeAborted {
			t.Errorf("%s при занятом переносе: %v", name, err)
		}
	}
	f.srv.migration.busy.Unlock()
	if rep := f.importNeko(t, req); rep.GroupsImported != 2 {
		t.Errorf("после освобождения: %v", rep)
	}
}

// Превышение пределов разбора → RESOURCE_EXHAUSTED.
func TestImportLimitsExhausted(t *testing.T) {
	f := newMigrationFixture(t, nil)
	saved := nekoimport.DefaultLimits
	nekoimport.DefaultLimits.MaxFiles = 3
	t.Cleanup(func() { nekoimport.DefaultLimits = saved })
	_, err := f.migrate.PreviewNekoBoxImport(context.Background(),
		connect.NewRequest(&lavrayv1.NekoBoxImportRequest{Path: copyNekoFixture(t)}))
	if connect.CodeOf(err) != connect.CodeResourceExhausted {
		t.Errorf("код %v (%v)", connect.CodeOf(err), err)
	}
}

// Агентский токен не видит и не запускает перенос ни в одном режиме (S).
func TestAgentDeniedMigration(t *testing.T) {
	f := newAgentFixture(t)
	dir := copyNekoFixture(t)
	calls := []agentCall{
		{"DetectNekoBox", func(c *apiClients) error {
			return unary(c.migration.DetectNekoBox, &lavrayv1.DetectNekoBoxRequest{})
		}},
		{"PreviewNekoBoxImport", func(c *apiClients) error {
			return unary(c.migration.PreviewNekoBoxImport, &lavrayv1.NekoBoxImportRequest{Path: dir})
		}},
		{"ImportNekoBox", func(c *apiClients) error {
			return unary(c.migration.ImportNekoBox, &lavrayv1.NekoBoxImportRequest{Path: dir, IncludeRoutes: true})
		}},
	}
	before := profileCount(t, f.srv.st)
	for _, mode := range []string{"read", "control"} {
		f.setMode(t, mode)
		for _, call := range calls {
			if err := call.run(f.agent); !denied(err) {
				t.Errorf("режим %s, агент, %s: %v — ожидался PERMISSION_DENIED", mode, call.name, err)
			}
		}
	}
	if profileCount(t, f.srv.st) != before {
		t.Error("отказанный агент изменил профили")
	}
	if err := calls[0].run(f.main); err != nil {
		t.Errorf("основной токен, DetectNekoBox: %v", err)
	}
}

func TestCandidateAndReportProto(t *testing.T) {
	rep := &nekoimport.Report{GroupsImported: 1, ProfilesByType: map[string]int{"vless": 2},
		Skipped: []nekoimport.Skipped{{File: "profiles/9.json", ID: 9, Type: "wireguard", Reason: "unknown"}}, ActiveRoute: "custom:x"}
	out := reportProto(rep)
	if out.ProfilesByType["vless"] != 2 || len(out.Skipped) != 1 || out.Skipped[0].Id != 9 || out.GetNekoboxActiveRoute() != "custom:x" {
		t.Errorf("reportProto: %v", out)
	}
	if reportProto(&nekoimport.Report{}).NekoboxActiveRoute != nil {
		t.Error("пустой активный маршрут должен быть не задан")
	}
}
