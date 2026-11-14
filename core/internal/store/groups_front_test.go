// SPDX-License-Identifier: GPL-3.0-only

package store

import (
	"errors"
	"testing"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

// SetGroupFrontProxy работает и для группы с id 0 (id NekoBox начинаются с 0,
// а UpdateGroup id 0 отвергает), снимается значением nil и входит в InTx.
func TestSetGroupFrontProxyAcceptsIDZero(t *testing.T) {
	s := openTemp(t)
	if err := s.ImportGroup(&model.Group{ID: 0, Name: "zero", Kind: model.GroupBasic}); err != nil {
		t.Fatal(err)
	}
	p := newProfile(0, model.TypeSOCKS, "front", "f.example", 1)
	p.ID = 5
	if err := s.ImportProfile(p); err != nil {
		t.Fatal(err)
	}
	set := func(id *int64) error { return s.SetGroupFrontProxy(0, id) }
	pid := int64(5)
	if err := set(&pid); err != nil {
		t.Fatalf("SetGroupFrontProxy(0): %v", err)
	}
	if g, _ := s.GetGroup(0); g.FrontProxyID == nil || *g.FrontProxyID != 5 {
		t.Fatalf("front_proxy_id = %v, ожидалось 5", g.FrontProxyID)
	}
	if err := set(nil); err != nil {
		t.Fatal(err)
	}
	if g, _ := s.GetGroup(0); g.FrontProxyID != nil {
		t.Fatalf("front_proxy_id не снят: %v", *g.FrontProxyID)
	}
	if err := s.SetGroupFrontProxy(404, &pid); !errors.Is(err, ErrNotFound) {
		t.Errorf("нет группы: ошибка %v, ожидалась ErrNotFound", err)
	}
	missing := int64(999)
	if err := set(&missing); err == nil {
		t.Error("front_proxy_id на несуществующий профиль принят")
	}
	// Внутри InTx запись входит в транзакцию серии и откатывается с ней.
	boom := errors.New("boom")
	err := s.InTx(func(tx *Store) error {
		if err := tx.SetGroupFrontProxy(0, &pid); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("InTx: %v", err)
	}
	if g, _ := s.GetGroup(0); g.FrontProxyID != nil {
		t.Error("front_proxy_id остался после отката InTx")
	}
}
