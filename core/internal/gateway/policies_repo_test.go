// SPDX-License-Identifier: GPL-3.0-only

package gateway

import (
	"errors"
	"fmt"
	"testing"
)

// memKV — настройки в памяти.
type memKV struct {
	m       map[string]string
	failSet bool
}

func newMemKV() *memKV { return &memKV{m: map[string]string{}} }

func (k *memKV) GetSetting(key string) (string, bool, error) {
	v, ok := k.m[key]
	return v, ok, nil
}

func (k *memKV) SetSetting(key, value string) error {
	if k.failSet {
		return errors.New("диск полон")
	}
	k.m[key] = value
	return nil
}

func TestPolicyRepoRoundTrip(t *testing.T) {
	repo := NewPolicyRepo(newMemKV())
	if list, err := repo.List(); err != nil || len(list) != 0 {
		t.Fatalf("пустой список: %v %v", list, err)
	}
	in := []DevicePolicy{
		{MAC: "aa:bb:cc:dd:ee:20", Policy: PolicyDirect},
		{MAC: "aa:bb:cc:dd:ee:10", Policy: PolicyPreset, PresetID: "ru-direct"},
	}
	if err := repo.Save(in); err != nil {
		t.Fatal(err)
	}
	got, err := repo.List()
	if err != nil || len(got) != 2 || got[0].MAC != "aa:bb:cc:dd:ee:10" || got[0].PresetID != "ru-direct" || got[1].Policy != PolicyDirect {
		t.Fatalf("после Save: %+v err=%v", got, err)
	}
}

func TestPolicyRepoLimitAndCorruption(t *testing.T) {
	kv := newMemKV()
	repo := NewPolicyRepo(kv)
	many := make([]DevicePolicy, MaxPolicies+1)
	for i := range many {
		many[i] = DevicePolicy{MAC: fmt.Sprintf("02:00:00:00:%02x:%02x", i/256, i%256), Policy: PolicyProxy}
	}
	if err := repo.Save(many); !errors.Is(err, ErrTooManyPolicies) {
		t.Fatalf("сверх лимита: %v", err)
	}
	if err := repo.Save(many[:MaxPolicies]); err != nil {
		t.Fatalf("ровно на лимите: %v", err)
	}
	for name, raw := range map[string]string{
		"не JSON":         "{",
		"плохой MAC":      `[{"mac":"zz","policy":"proxy"}]`,
		"плохое действие": `[{"mac":"aa:bb:cc:dd:ee:00","policy":"nuke"}]`,
	} {
		kv.m[SettingPolicies] = raw
		if _, err := repo.List(); err == nil {
			t.Errorf("%s: повреждённый список принят", name)
		}
	}
}

func TestUpserted(t *testing.T) {
	list := []DevicePolicy{{MAC: "aa:bb:cc:dd:ee:00", Policy: PolicyProxy}}
	up := Upserted(list, DevicePolicy{MAC: "aa:bb:cc:dd:ee:00", Policy: PolicyDirect}, false)
	if len(up) != 1 || up[0].Policy != PolicyDirect || list[0].Policy != PolicyProxy {
		t.Fatalf("замена: %+v (исходный %+v)", up, list)
	}
	if rm := Upserted(list, DevicePolicy{MAC: "aa:bb:cc:dd:ee:00"}, true); len(rm) != 0 {
		t.Fatalf("удаление: %+v", rm)
	}
}
