// SPDX-License-Identifier: GPL-3.0-only

package api

// preset_activate_test.go — активация набора против удаления (T-80, C): активным
// не может остаться набор, которого уже нет.

import (
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
)

// ActivatePreset проверяет набор и записывает активный id под тем же замком, что
// и правка наборов: пока замок занят (идёт DeletePreset), активация ждёт.
func TestActivatePresetWaitsForPresetLock(t *testing.T) {
	f := newSvcFixture(t, nil)
	s := f.srv
	made, err := s.CreatePreset(bg, connect.NewRequest(&lavrayv1.CreatePresetRequest{BuiltinSourceId: strPtr("global")}))
	if err != nil {
		t.Fatal(err)
	}
	s.presetRepo.mu.Lock()
	done := make(chan error, 1)
	go func() {
		_, err := s.ActivatePreset(bg, connect.NewRequest(&lavrayv1.ActivatePresetRequest{Id: made.Msg.Id}))
		done <- err
	}()
	select {
	case err := <-done:
		s.presetRepo.mu.Unlock()
		t.Fatalf("активация прошла при занятом замке наборов: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	s.presetRepo.mu.Unlock()
	if err := <-done; err != nil {
		t.Fatalf("активация после освобождения замка: %v", err)
	}
	if got := s.activePresetID(); got != made.Msg.Id {
		t.Fatalf("активен %q", got)
	}
}

// TestActivateRacesDeleteKeepsActiveAlive: после любой перестановки Activate и
// Delete активный набор существует — либо активация проиграла (NOT_FOUND), либо
// удаление переключило на global.
func TestActivateRacesDeleteKeepsActiveAlive(t *testing.T) {
	f := newSvcFixture(t, nil)
	s := f.srv
	for i := 0; i < 40; i++ {
		made, err := s.CreatePreset(bg, connect.NewRequest(&lavrayv1.CreatePresetRequest{BuiltinSourceId: strPtr("global")}))
		if err != nil {
			t.Fatal(err)
		}
		id := made.Msg.Id
		var wg sync.WaitGroup
		var actErr, delErr error
		start := make(chan struct{})
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, actErr = s.ActivatePreset(bg, connect.NewRequest(&lavrayv1.ActivatePresetRequest{Id: id}))
		}()
		go func() {
			defer wg.Done()
			<-start
			_, delErr = s.DeletePreset(bg, connect.NewRequest(&lavrayv1.DeletePresetRequest{Id: id}))
		}()
		close(start)
		wg.Wait()
		if delErr != nil {
			t.Fatalf("итерация %d: удаление: %v", i, delErr)
		}
		if actErr != nil && connect.CodeOf(actErr) != connect.CodeNotFound {
			t.Fatalf("итерация %d: активация: %v", i, actErr)
		}
		if _, ok, err := s.findPreset(s.activePresetID()); err != nil || !ok {
			t.Fatalf("итерация %d: активен несуществующий набор %q (activate=%v)", i, s.activePresetID(), actErr)
		}
	}
}

// Если в настройках всё же остался id удалённого набора, сборка конфига и
// GetActivePreset работают с global, а список сообщает global активным.
func TestStaleActivePresetFallsBackToGlobal(t *testing.T) {
	f := newSvcFixture(t, nil)
	s := f.srv
	if err := s.setActivePreset("custom:vanished"); err != nil {
		t.Fatal(err)
	}
	p, err := s.activePreset()
	if err != nil || p == nil || p.ID != "global" {
		t.Fatalf("activePreset = %+v, %v; ожидался global", p, err)
	}
	got, err := s.GetActivePreset(bg, connect.NewRequest(&lavrayv1.GetActivePresetRequest{}))
	if err != nil || got.Msg.Id != "global" {
		t.Fatalf("GetActivePreset = %v, %v", got, err)
	}
	list, err := s.ListPresets(bg, connect.NewRequest(&lavrayv1.ListPresetsRequest{}))
	if err != nil || list.Msg.ActivePresetId != "global" {
		t.Fatalf("ListPresets.active = %v, %v", list, err)
	}
	pr := f.trojan(t)
	if _, err := s.Connect(bg, connect.NewRequest(&lavrayv1.ConnectRequest{ProfileId: &pr.ID})); err != nil {
		t.Fatalf("Connect с устаревшим активным id: %v", err)
	}
}
