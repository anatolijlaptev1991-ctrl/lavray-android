// SPDX-License-Identifier: GPL-3.0-only

package presets

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// overrideOf — встроенный набор id с ревизией rev и добавленным доменом (маркер правки).
func overrideOf(t *testing.T, id string, rev int, marker string) []byte {
	t.Helper()
	list, err := BuiltinPresets()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range list {
		if p.ID != id {
			continue
		}
		c := p.Clone()
		c.Revision = rev
		c.Routing.DirectDomain = append(c.Routing.DirectDomain, marker)
		b, err := json.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	t.Fatalf("нет встроенного набора %q", id)
	return nil
}

func putOverride(t *testing.T, dir, name string, raw []byte) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func byID(t *testing.T, dir, id string) *Preset {
	t.Helper()
	list, err := BuiltinPresetsWith(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range list {
		if p.ID == id {
			return p
		}
	}
	t.Fatalf("набор %q пропал", id)
	return nil
}

func hasDomain(p *Preset, d string) bool {
	for _, x := range p.Routing.DirectDomain {
		if x == d {
			return true
		}
	}
	return false
}

func TestOverrideHigherRevWins(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "override")
	base := byID(t, "", "lav-ru")
	putOverride(t, dir, "lav-ru.json", overrideOf(t, "lav-ru", base.Revision+1, "full:marker.example"))
	got := byID(t, dir, "lav-ru")
	if got.Revision != base.Revision+1 || !hasDomain(got, "full:marker.example") || !got.Builtin {
		t.Fatalf("переопределение не применено: rev=%d builtin=%v", got.Revision, got.Builtin)
	}
	if other := byID(t, dir, "global"); other.Revision != byID(t, "", "global").Revision {
		t.Fatal("чужой набор изменился")
	}
}

func TestOverrideOrderAndCountStable(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "override")
	putOverride(t, dir, "lav-ru.json", overrideOf(t, "lav-ru", 99, "full:a.example"))
	a, _ := BuiltinPresetsWith(dir)
	b, _ := BuiltinPresets()
	if len(a) != len(b) {
		t.Fatalf("число наборов: %d против %d", len(a), len(b))
	}
	for i := range a {
		if a[i].ID != b[i].ID {
			t.Fatalf("порядок: %s против %s", a[i].ID, b[i].ID)
		}
	}
}

// Низкая и равная ревизии, чужой id внутри файла, builtin=false, битый JSON и
// гигантский файл — переопределением не становятся: побеждает встроенный набор.
func TestOverrideRejectedCases(t *testing.T) {
	base := byID(t, "", "lav-ru")
	notBuiltin := func() []byte {
		var m map[string]any
		_ = json.Unmarshal(overrideOf(t, "lav-ru", base.Revision+5, "full:x.example"), &m)
		m["builtin"] = false
		b, _ := json.Marshal(m)
		return b
	}()
	otherID := strings.Replace(string(overrideOf(t, "lav-ru", base.Revision+5, "full:x.example")), `"id":"lav-ru"`, `"id":"global"`, 1)
	cases := map[string][]byte{
		"older":       overrideOf(t, "lav-ru", base.Revision-1, "full:x.example"),
		"equal":       overrideOf(t, "lav-ru", base.Revision, "full:x.example"),
		"not_builtin": notBuiltin,
		"other_id":    []byte(otherID),
		"broken":      []byte(`{"id":"lav-ru", broken`),
		"huge":        append([]byte(`{"x":"`), make([]byte, maxOverrideBytes+1)...),
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "override")
			putOverride(t, dir, "lav-ru.json", raw)
			got := byID(t, dir, "lav-ru")
			if got.Revision != base.Revision || hasDomain(got, "full:x.example") {
				t.Fatalf("переопределение принято: rev=%d", got.Revision)
			}
		})
	}
}

// Файл с чужим именем и id не встроенного набора никогда не становится набором.
func TestOverrideNeverAddsNonBuiltinID(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "override")
	raw := overrideOf(t, "lav-ru", 50, "full:x.example")
	for _, name := range []string{"custom:evil.json", "evil.json", "lav-ru.json.bak", "..%2Fx.json"} {
		putOverride(t, dir, name, []byte(strings.Replace(string(raw), `"id":"lav-ru"`, `"id":"custom:evil"`, 1)))
	}
	list, err := BuiltinPresetsWith(dir)
	if err != nil {
		t.Fatal(err)
	}
	base, _ := BuiltinPresets()
	if len(list) != len(base) {
		t.Fatalf("появились лишние наборы: %d вместо %d", len(list), len(base))
	}
	for _, p := range list {
		if strings.Contains(p.ID, "evil") {
			t.Fatalf("чужой id попал в список: %s", p.ID)
		}
	}
}

func TestOverrideDirLayout(t *testing.T) {
	got := OverrideDir(filepath.Join("data"))
	if got != filepath.Join("data", "presets", "override") {
		t.Fatalf("каталог переопределений: %q", got)
	}
}
