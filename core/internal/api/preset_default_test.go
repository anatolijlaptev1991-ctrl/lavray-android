// SPDX-License-Identifier: GPL-3.0-only

package api

// preset_default_test.go — набор по умолчанию у новой установки (0.9.3).

import (
	"testing"

	"connectrpc.com/connect"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
)

func TestFreshInstallUsesLavRU(t *testing.T) {
	s := newSvcFixture(t, nil).srv
	if got := s.activePresetID(); got != "lav-ru" {
		t.Fatalf("активный id = %q, ждём lav-ru", got)
	}
	// Значение по умолчанию обязано указывать на существующий встроенный набор.
	p, err := s.activePreset()
	if err != nil || p == nil || p.ID != "lav-ru" {
		t.Fatalf("activePreset = %+v, %v; ждём lav-ru", p, err)
	}
	list, err := s.ListPresets(bg, connect.NewRequest(&lavrayv1.ListPresetsRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	if list.Msg.ActivePresetId != "lav-ru" {
		t.Fatalf("ListPresets.active_preset_id = %q, ждём lav-ru", list.Msg.ActivePresetId)
	}
}

func TestExplicitGlobalSurvivesNewDefault(t *testing.T) {
	s := newSvcFixture(t, nil).srv
	activate(t, s, "global")
	if got := s.activePresetID(); got != "global" {
		t.Fatalf("явный выбор потерян: активен %q", got)
	}
}
