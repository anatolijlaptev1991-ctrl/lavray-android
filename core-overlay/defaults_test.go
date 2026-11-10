// SPDX-License-Identifier: GPL-3.0-only

package mobile

import (
	"testing"

	"github.com/anatolijlaptev1991-ctrl/lavray/core/internal/store"
)

// Регрессия: на Android подписка при поднятом туннеле шла в удалённый mixed-in
// (127.0.0.1:2080) и падала с connection refused.
func TestApplyAndroidDefaultsDisablesTunnelProxy(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := applyAndroidDefaults(st); err != nil {
		t.Fatal(err)
	}
	if v, ok, _ := st.GetSetting("sub.use_proxy"); !ok || v != "false" {
		t.Fatalf("sub.use_proxy = %q, задан=%v", v, ok)
	}
}

func TestApplyAndroidDefaultsKeepsUserChoice(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.SetSetting("sub.use_proxy", "true"); err != nil {
		t.Fatal(err)
	}
	if err := applyAndroidDefaults(st); err != nil {
		t.Fatal(err)
	}
	if v, _, _ := st.GetSetting("sub.use_proxy"); v != "true" {
		t.Fatalf("выбор пользователя перезаписан: %q", v)
	}
}

// Замеры docs/MEASUREMENTS.md: gVisor с MTU 9000 — лучшее из рабочих сочетаний на Android.
func TestApplyAndroidDefaultsTunStackAndMTU(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := applyAndroidDefaults(st); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{"tun.impl": "1", "tun.mtu": "9000"} {
		if v, ok, _ := st.GetSetting(key); !ok || v != want {
			t.Fatalf("%s = %q, задан=%v, ждали %q", key, v, ok, want)
		}
	}
}

func TestApplyAndroidDefaultsKeepsUserTunChoice(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.SetSetting("tun.mtu", "1400"); err != nil {
		t.Fatal(err)
	}
	if err := applyAndroidDefaults(st); err != nil {
		t.Fatal(err)
	}
	if v, _, _ := st.GetSetting("tun.mtu"); v != "1400" {
		t.Fatalf("выбор пользователя перезаписан: %q", v)
	}
}
