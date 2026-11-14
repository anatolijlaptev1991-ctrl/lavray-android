// SPDX-License-Identifier: GPL-3.0-only

package api

import (
	"fmt"
	"testing"

	"connectrpc.com/connect"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/presets"
)

// TestPlanInsertPure: PlanInsert не меняет входной список, даёт свободный id,
// не зависит от id во входном наборе и держит лимит; пачка вставок работает
// через List предыдущего шага.
func TestPlanInsertPure(t *testing.T) {
	embedded, _ := presets.BuiltinPresets()
	var base *presets.Preset
	for _, p := range embedded {
		if p.ID == "global" {
			base = p
		}
	}
	mk := func(name string) *presets.Preset {
		p := presets.NewCustomFrom(base, "custom:ignored")
		p.Name = presets.Localized{"en": name}
		return p
	}
	var list []*presets.Preset
	for i, want := range []string{"custom:a", "custom:a-2", "custom:a-3"} {
		plan, err := PlanInsert(list, mk("A"))
		if err != nil || plan.Preset.ID != want || len(plan.List) != i+1 || len(list) != i {
			t.Fatalf("шаг %d: %+v %v (len(list)=%d)", i, plan.Preset, err, len(list))
		}
		list = plan.List
	}
	in := mk("A")
	in.Builtin, in.Revision, in.ID = true, 77, "global"
	plan, err := PlanInsert(list, in)
	if err != nil || plan.Preset.Builtin || plan.Preset.Revision != 1 || plan.Preset.ID != "custom:a-4" {
		t.Fatalf("входные поля не очищены: %+v %v", plan.Preset, err)
	}
	if in.ID != "global" || in.Revision != 77 {
		t.Fatal("PlanInsert изменил входной набор")
	}
	if plan.Preset.SourceID != "global" {
		t.Fatalf("источник копии потерян: %q", plan.Preset.SourceID)
	}
	var invalid *presetInvalidError
	bad := mk("Плохой")
	bad.Routing.DirectDomain = []string{"geoip:ru"}
	if _, err := PlanInsert(nil, bad); !asInvalid(err, &invalid) {
		t.Fatalf("невалидный набор: %v", err)
	}
	full := make([]*presets.Preset, presets.MaxCustomPresets)
	for i := range full {
		full[i] = &presets.Preset{ID: fmt.Sprintf("custom:p%d", i)}
	}
	if _, err := PlanInsert(full, mk("Ещё")); err == nil || connect.CodeOf(mapErr(err)) != connect.CodeResourceExhausted {
		t.Fatalf("лимит: %v", err)
	}
}

func asInvalid(err error, target **presetInvalidError) bool {
	for err != nil {
		if e, ok := err.(*presetInvalidError); ok {
			*target = e
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
