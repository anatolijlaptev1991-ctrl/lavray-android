// SPDX-License-Identifier: GPL-3.0-only

package store

import (
	"errors"
	"testing"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

// D-04: счётчики трафика пишутся отдельным UPDATE (UpdateProfile их не пишет),
// остальные поля профиля при этом не затрагиваются.
func TestSaveTraffic(t *testing.T) {
	s := openTemp(t)
	g := newGroup("G", model.GroupBasic)
	if err := s.CreateGroup(g); err != nil {
		t.Fatal(err)
	}
	p := newProfile(g.ID, model.TypeTrojan, "srv", "1.1.1.1", 443)
	p.Traffic = model.TrafficCounters{UplinkBytes: 500, DownlinkBytes: 900}
	if err := s.CreateProfile(p); err != nil {
		t.Fatal(err)
	}

	// UpdateProfile не трогает счётчики (контракт метода).
	edited, _ := s.GetProfile(p.ID)
	edited.Traffic = model.TrafficCounters{}
	edited.Name = "renamed"
	if err := s.UpdateProfile(edited); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetProfile(p.ID); got.Traffic != (model.TrafficCounters{UplinkBytes: 500, DownlinkBytes: 900}) {
		t.Fatalf("UpdateProfile изменил счётчики: %+v", got.Traffic)
	}
	before, _ := s.GetProfile(p.ID)

	if err := s.SaveTraffic(p.ID, model.TrafficCounters{}); err != nil {
		t.Fatalf("SaveTraffic: %v", err)
	}
	after, err := s.GetProfile(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Traffic != (model.TrafficCounters{}) {
		t.Fatalf("после сброса %+v, ожидалось {0 0}", after.Traffic)
	}
	if after.Name != "renamed" || after.UpdatedAtMs != before.UpdatedAtMs {
		t.Fatalf("SaveTraffic тронул другие поля: имя %q, updated_at %d -> %d", after.Name, before.UpdatedAtMs, after.UpdatedAtMs)
	}

	if err := s.SaveTraffic(p.ID, model.TrafficCounters{UplinkBytes: 7, DownlinkBytes: 8}); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetProfile(p.ID); got.Traffic != (model.TrafficCounters{UplinkBytes: 7, DownlinkBytes: 8}) {
		t.Fatalf("запись значений: %+v", got.Traffic)
	}

	if err := s.SaveTraffic(999999, model.TrafficCounters{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("несуществующий профиль: %v, ожидалось ErrNotFound", err)
	}
}

// T-79: AddTraffic прибавляет к колонкам и к ключу traffic в JSON профиля.
func TestAddTrafficAccumulates(t *testing.T) {
	s := openTemp(t)
	g := newGroup("G", model.GroupBasic)
	if err := s.CreateGroup(g); err != nil {
		t.Fatal(err)
	}
	p := newProfile(g.ID, model.TypeTrojan, "srv", "1.1.1.1", 443)
	p.Traffic = model.TrafficCounters{UplinkBytes: 500, DownlinkBytes: 900}
	if err := s.CreateProfile(p); err != nil {
		t.Fatal(err)
	}
	before, _ := s.GetProfile(p.ID)

	for i := 0; i < 2; i++ {
		if err := s.AddTraffic(p.ID, 10, 20); err != nil {
			t.Fatal(err)
		}
	}
	after, _ := s.GetProfile(p.ID)
	if after.Traffic != (model.TrafficCounters{UplinkBytes: 520, DownlinkBytes: 940}) {
		t.Fatalf("счётчики после двух прибавок: %+v", after.Traffic)
	}
	if after.Name != before.Name || after.UpdatedAtMs != before.UpdatedAtMs {
		t.Fatalf("AddTraffic тронул другие поля: %q/%d -> %q/%d", before.Name, before.UpdatedAtMs, after.Name, after.UpdatedAtMs)
	}
	var up, down int64
	if err := s.db.QueryRow(`SELECT json_extract(data, '$.traffic.uplink_bytes'),
		json_extract(data, '$.traffic.downlink_bytes') FROM profiles WHERE id = ?`, p.ID).Scan(&up, &down); err != nil {
		t.Fatal(err)
	}
	if up != 520 || down != 940 {
		t.Fatalf("JSON профиля разошёлся с колонками: ↑%d ↓%d", up, down)
	}
	if err := s.AddTraffic(p.ID, -1, 0); !errors.Is(err, ErrNegativeTraffic) {
		t.Fatalf("отрицательная прибавка: %v", err)
	}
}

func TestAddTrafficDeletedProfileNoop(t *testing.T) {
	s := openTemp(t)
	if err := s.AddTraffic(424242, 5, 5); err != nil {
		t.Fatalf("несуществующий профиль не должен давать ошибку: %v", err)
	}
	g := newGroup("G", model.GroupBasic)
	_ = s.CreateGroup(g)
	p := newProfile(g.ID, model.TypeTrojan, "srv", "1.1.1.1", 443)
	_ = s.CreateProfile(p)
	if err := s.DeleteProfile(p.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.AddTraffic(p.ID, 5, 5); err != nil {
		t.Fatalf("удалённый профиль: %v", err)
	}
	if _, err := s.GetProfile(p.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("AddTraffic воскресил профиль: %v", err)
	}
}
