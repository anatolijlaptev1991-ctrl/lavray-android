// SPDX-License-Identifier: GPL-3.0-only

package api

import (
	"testing"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
)

// TestAgentDeniedPresets (S): агентский токен не видит и не меняет наборы
// маршрутов ни в режиме read, ни в control; основной токен работает.
func TestAgentDeniedPresets(t *testing.T) {
	f := newAgentFixture(t)
	calls := []agentCall{
		{"ListPresets", func(c *apiClients) error { return unary(c.routing.ListPresets, &lavrayv1.ListPresetsRequest{}) }},
		{"GetPreset", func(c *apiClients) error { return unary(c.routing.GetPreset, &lavrayv1.GetPresetRequest{Id: "global"}) }},
		{"CreatePreset", func(c *apiClients) error {
			src := "global"
			return unary(c.routing.CreatePreset, &lavrayv1.CreatePresetRequest{BuiltinSourceId: &src})
		}},
		{"UpdatePreset", func(c *apiClients) error {
			return unary(c.routing.UpdatePreset, &lavrayv1.UpdatePresetRequest{Preset: &lavrayv1.RoutingPreset{Id: "custom:x"}})
		}},
		{"DeletePreset", func(c *apiClients) error {
			return unary(c.routing.DeletePreset, &lavrayv1.DeletePresetRequest{Id: "custom:x"})
		}},
		{"ResetBuiltinPreset", func(c *apiClients) error {
			return unary(c.routing.ResetBuiltinPreset, &lavrayv1.ResetBuiltinPresetRequest{Id: "custom:x"})
		}},
		{"ExportPreset", func(c *apiClients) error {
			return unary(c.routing.ExportPreset, &lavrayv1.ExportPresetRequest{Id: "global"})
		}},
		{"ImportPreset", func(c *apiClients) error {
			return unary(c.routing.ImportPreset, &lavrayv1.ImportPresetRequest{Json: "{}"})
		}},
		{"ImportNekoRayRoute", func(c *apiClients) error {
			return unary(c.routing.ImportNekoRayRoute, &lavrayv1.ImportNekoRayRouteRequest{NekoboxJson: "{}"})
		}},
	}
	for _, mode := range []string{"read", "control"} {
		f.setMode(t, mode)
		for _, call := range calls {
			if err := call.run(f.agent); !denied(err) {
				t.Errorf("режим %s, агент, %s: %v — ожидался PERMISSION_DENIED", mode, call.name, err)
			}
		}
	}
	// Основной токен тех же вызовов не лишён.
	if err := calls[0].run(f.main); err != nil {
		t.Errorf("основной токен, ListPresets: %v", err)
	}
	// Ни один отказ не создал и не удалил наборы.
	list, err := f.srv.presetRepo.List()
	if err != nil || len(list) != 0 {
		t.Fatalf("наборы после отказов: %d, %v", len(list), err)
	}
}
