// SPDX-License-Identifier: GPL-3.0-only

package updates

import "testing"

func manifestWith(version, minFrom string, assets ...string) *Manifest {
	m := map[string]Asset{}
	for _, a := range assets {
		m[a] = Asset{Path: "x", Sha256: "00", Length: 1}
	}
	return &Manifest{App: &AppSection{Version: version, MinUpgradeFrom: minFrom, Assets: m}}
}

func TestCheckUpdatesIgnoresOlderVersion(t *testing.T) {
	a := Decide(manifestWith("0.4.0", "", "windows-x64"), "0.5.0", "windows-x64")
	if a.Available || a.Reason != ReasonUpToDate {
		t.Fatalf("более старая версия в манифесте не обновление: %+v", a)
	}
	// serial манифеста выше сохранённого ничего не меняет: решает версия
	if a := Decide(manifestWith("0.6.0", "", "windows-x64"), "0.5.0", "windows-x64"); !a.Available || a.Version != "0.6.0" {
		t.Fatalf("новее — доступно: %+v", a)
	}
}

func TestEqualVersionNotAvailable(t *testing.T) {
	if a := Decide(manifestWith("0.5.0", "", "windows-x64"), "0.5.0", "windows-x64"); a.Available {
		t.Fatalf("равная версия: %+v", a)
	}
	// 0.5.1-rc.1 новее 0.5.0, но не новее 0.5.1
	if a := Decide(manifestWith("0.5.1-rc.1", "", "windows-x64"), "0.5.0", "windows-x64"); !a.Available {
		t.Fatalf("rc новее релиза предыдущей версии: %+v", a)
	}
	if a := Decide(manifestWith("0.5.1-rc.1", "", "windows-x64"), "0.5.1", "windows-x64"); a.Available {
		t.Fatalf("rc не новее релиза той же версии: %+v", a)
	}
}

func TestMinUpgradeFromBlocks(t *testing.T) {
	mf := manifestWith("1.0.0", "0.5.0", "windows-x64")
	if a := Decide(mf, "0.4.9", "windows-x64"); a.Available || a.Reason != ReasonMinUpgrade || a.MinUpgradeFrom != "0.5.0" {
		t.Fatalf("ниже minUpgradeFrom: %+v", a)
	}
	if a := Decide(mf, "0.5.0", "windows-x64"); !a.Available {
		t.Fatalf("ровно minUpgradeFrom разрешён (граница): %+v", a)
	}
}

func TestMissingPlatformAssetNotAvailable(t *testing.T) {
	mf := manifestWith("1.0.0", "", "linux-x64")
	if a := Decide(mf, "0.5.0", "windows-x64"); a.Available || a.Reason != ReasonNoPlatformAst {
		t.Fatalf("нет актива платформы: %+v", a)
	}
	if a := Decide(&Manifest{}, "0.5.0", "windows-x64"); a.Available || a.Reason != ReasonNoApp {
		t.Fatalf("манифест без app (только пресеты): %+v", a)
	}
	if a := Decide(nil, "0.5.0", "windows-x64"); a.Available {
		t.Fatalf("nil-манифест: %+v", a)
	}
	if a := Decide(mf, "", "linux-x64"); a.Available {
		t.Fatalf("неизвестная версия приложения: %+v", a)
	}
}

func TestAssetKeyFor(t *testing.T) {
	cases := map[[2]string]string{
		{"windows", "amd64"}: "windows-x64", {"windows", "arm64"}: "windows-arm64",
		{"linux", "amd64"}: "linux-x64", {"darwin", "arm64"}: "macos-universal",
		{"windows", "386"}: "", {"plan9", "amd64"}: "",
	}
	for in, want := range cases {
		if got := AssetKeyFor(in[0], in[1]); got != want {
			t.Errorf("AssetKeyFor(%v)=%q, ожидалось %q", in, got, want)
		}
	}
}
