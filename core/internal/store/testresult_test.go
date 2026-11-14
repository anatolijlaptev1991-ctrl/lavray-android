// SPDX-License-Identifier: GPL-3.0-only

package store

import (
	"errors"
	"testing"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

// T-22 (B1-09, B1-28, C2-08): результат теста пишется отдельным UPDATE —
// только его поля, без чтения-изменения-записи всего профиля.
func TestSaveTestResult(t *testing.T) {
	s := openTemp(t)
	g := newGroup("G", model.GroupBasic)
	if err := s.CreateGroup(g); err != nil {
		t.Fatal(err)
	}
	p := newProfile(g.ID, model.TypeTrojan, "srv", "1.1.1.1", 443)
	if err := s.CreateProfile(p); err != nil {
		t.Fatal(err)
	}

	// Пользователь правит профиль уже после того, как тест прочитал его.
	edited, err := s.GetProfile(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	edited.Name, edited.Notes, edited.Favorite = "renamed", "заметка", true
	if err := s.UpdateProfile(edited); err != nil {
		t.Fatal(err)
	}
	before, _ := s.GetProfile(p.ID)

	lat, at := int32(42), int64(1700000000000)
	if err := s.SaveTestResult(p.ID, model.TestResult{LatencyMs: &lat, TestedAtMs: &at}); err != nil {
		t.Fatalf("SaveTestResult: %v", err)
	}
	got, err := s.GetProfile(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.TestResult.LatencyMs == nil || *got.TestResult.LatencyMs != 42 ||
		got.TestResult.TestedAtMs == nil || *got.TestResult.TestedAtMs != at {
		t.Fatalf("результат не сохранён: %+v", got.TestResult)
	}
	if got.Name != "renamed" || got.Notes != "заметка" || !got.Favorite {
		t.Fatalf("правки пользователя затёрты: name=%q notes=%q fav=%v", got.Name, got.Notes, got.Favorite)
	}
	if got.UpdatedAtMs != before.UpdatedAtMs {
		t.Fatalf("updated_at_ms изменился: %d → %d", before.UpdatedAtMs, got.UpdatedAtMs)
	}
	list, err := s.ListProfiles(g.ID)
	if err != nil || len(list) != 1 || list[0].TestResult.LatencyMs == nil || *list[0].TestResult.LatencyMs != 42 {
		t.Fatalf("ListProfiles не видит задержку: %v %+v", err, list)
	}

	// Ошибка замера: прежняя задержка сбрасывается, last_error доступен.
	if err := s.SaveTestResult(p.ID, model.TestResult{TestedAtMs: &at, LastError: "refused"}); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetProfile(p.ID)
	if got.TestResult.LatencyMs != nil || got.TestResult.LastError != "refused" || !got.Favorite {
		t.Fatalf("после ошибки: %+v fav=%v", got.TestResult, got.Favorite)
	}

	// Пустой результат очищает всё (ClearTestResults).
	if err := s.SaveTestResult(p.ID, model.TestResult{}); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetProfile(p.ID)
	if got.TestResult.LatencyMs != nil || got.TestResult.TestedAtMs != nil || got.TestResult.LastError != "" {
		t.Fatalf("результат не очищен: %+v", got.TestResult)
	}

	if err := s.SaveTestResult(987654, model.TestResult{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("несуществующий профиль: %v", err)
	}
}
