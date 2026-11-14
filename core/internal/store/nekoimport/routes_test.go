// SPDX-License-Identifier: GPL-3.0-only

package nekoimport

import (
	"path/filepath"
	"strings"
	"testing"
)

// routes_box/ разобран в наборы: имя «Neko: <имя>», списки и процессные правила (H).
func TestRoutesBoxConverted(t *testing.T) {
	plan, err := Scan(fixtureConfig)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Routes) != 2 {
		t.Fatalf("маршрутов %d, ожидалось 2", len(plan.Routes))
	}
	var ru *PlanRoute
	for i := range plan.Routes {
		if plan.Routes[i].Name == "RU-bypass + Games" {
			ru = &plan.Routes[i]
		}
	}
	if ru == nil {
		t.Fatalf("маршрут RU-bypass + Games не найден: %+v", plan.Routes)
	}
	if got := ru.Preset.Name["en"]; got != "Neko: RU-bypass + Games" {
		t.Errorf("имя набора %q", got)
	}
	if len(ru.Preset.Routing.DirectDomain) != 3 || len(ru.Preset.Routing.DirectIP) != 2 {
		t.Errorf("списки: %+v", ru.Preset.Routing)
	}
	if len(ru.Preset.Routing.ProcessRules) == 0 {
		t.Errorf("процессные правила потеряны: %+v", ru.Preset.Routing)
	}
	if ru.Key == "" || ru.File != filepath.Join("routes_box", "RU-bypass + Games") {
		t.Errorf("ключ/файл: %q %q", ru.Key, ru.File)
	}
	if plan.ActiveRoute != "RU-bypass + Games" {
		t.Errorf("активный маршрут NekoBox = %q", plan.ActiveRoute)
	}
}

// Старый каталог routes/ принимается; при совпадении имён побеждает routes_box/.
func TestLegacyRoutesDir(t *testing.T) {
	dir := mkTempDir(t)
	write(t, filepath.Join(dir, "routes", "Old.json"), `{"def_outbound":"direct","direct_domain":"domain:a.example"}`)
	write(t, filepath.Join(dir, "routes", "Same"), `{"def_outbound":"direct"}`)
	write(t, filepath.Join(dir, "routes_box", "Same"), `{"def_outbound":"proxy"}`)
	plan, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, r := range plan.Routes {
		got[r.Name] = r.Preset.Routing.DefOutbound
	}
	if len(got) != 2 || got["Old"] != "direct" || got["Same"] != "proxy" {
		t.Errorf("маршруты %v: ожидались Old=direct и Same=proxy (routes_box важнее)", got)
	}
}

// Негодный маршрут попадает в отчёт и не мешает остальным (R).
func TestBrokenRouteReportedNotFatal(t *testing.T) {
	dir := copyTree(t, fixtureConfig)
	write(t, filepath.Join(dir, "routes_box", "Broken"), `{not json`)
	write(t, filepath.Join(dir, "routes_box", "BadOutbound"), `{"def_outbound":"teleport"}`)
	plan, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Routes) != 2 {
		t.Errorf("годных маршрутов %d, ожидалось 2", len(plan.Routes))
	}
	skipped := map[string]bool{}
	for _, sk := range plan.Report.Skipped {
		if sk.Type == "route" {
			skipped[sk.File] = true
		}
	}
	if !skipped[filepath.Join("routes_box", "Broken")] || !skipped[filepath.Join("routes_box", "BadOutbound")] {
		t.Errorf("негодные маршруты не в отчёте: %+v", plan.Report.Skipped)
	}
}

func TestDisplayRouteNameFitsLimit(t *testing.T) {
	name := displayRouteName(strings.Repeat("я", 200))
	if n := len([]rune(name)); n != 64 || !strings.HasPrefix(name, "Neko: ") {
		t.Errorf("имя %q (%d символов)", name, n)
	}
}
