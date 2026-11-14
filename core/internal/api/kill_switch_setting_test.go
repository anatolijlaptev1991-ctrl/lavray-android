// SPDX-License-Identifier: GPL-3.0-only

package api

// kill_switch_setting_test.go — настройка connect.kill_switch_proxy (хвосты B, 7.3):
// каталог, значение по умолчанию, запись и сброс. Читает её оболочка из зеркала
// ui-state.json, поэтому ядро только хранит значение.

import (
	"context"
	"testing"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
)

const killSwitchKey = "connect.kill_switch_proxy"

func TestKillSwitchProxyDescriptor(t *testing.T) {
	f := newSvcFixture(t, nil)
	res, err := f.srv.DescribeSettings(context.Background(), newReq(&lavrayv1.DescribeSettingsRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range res.Msg.Descriptors {
		if d.Key != killSwitchKey {
			continue
		}
		if d.Type != lavrayv1.SettingType_SETTING_TYPE_BOOL || !d.ShellOwned {
			t.Errorf("тип %v, shell_owned=%v; ожидались BOOL и shell_owned", d.Type, d.ShellOwned)
		}
		if d.DefaultValueJson == nil || *d.DefaultValueJson != "false" {
			t.Errorf("умолчание = %v, ожидалось false", d.DefaultValueJson)
		}
		return
	}
	t.Fatalf("ключа %s нет в каталоге", killSwitchKey)
}

func TestKillSwitchProxyRoundTripAndReset(t *testing.T) {
	f := newSvcFixture(t, nil)
	if f.srv.settingBool(killSwitchKey) {
		t.Fatal("по умолчанию прокси должен сниматься (kill switch выключен)")
	}
	_, err := f.srv.SetSettings(context.Background(), newReq(&lavrayv1.SetSettingsRequest{
		Values: map[string]*lavrayv1.SettingValue{killSwitchKey: {Value: &lavrayv1.SettingValue_BoolValue{BoolValue: true}}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !f.srv.settingBool(killSwitchKey) {
		t.Fatal("значение не сохранилось")
	}
	if _, err := f.reset(killSwitchKey); err != nil {
		t.Fatal(err)
	}
	if f.srv.settingBool(killSwitchKey) {
		t.Fatal("сброс не вернул умолчание")
	}
}

// Неверный тип значения отклоняется: оболочка читает только "1"/"0".
func TestKillSwitchProxyRejectsWrongType(t *testing.T) {
	f := newSvcFixture(t, nil)
	_, err := f.srv.SetSettings(context.Background(), newReq(&lavrayv1.SetSettingsRequest{
		Values: map[string]*lavrayv1.SettingValue{killSwitchKey: {Value: &lavrayv1.SettingValue_StringValue{StringValue: "yes"}}},
	}))
	if err == nil {
		t.Fatal("строка вместо bool принята")
	}
}
