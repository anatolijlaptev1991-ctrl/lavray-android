// SPDX-License-Identifier: GPL-3.0-only

package presets

import (
	"reflect"
	"testing"
)

func builtinByID(t *testing.T, id string) *Preset {
	t.Helper()
	list, err := BuiltinPresets()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range list {
		if p.ID == id {
			return p
		}
	}
	t.Fatalf("нет встроенного %s", id)
	return nil
}

// TestNewCustomFromUnmarksBuiltin: копия встроенного — не builtin, ревизия 1,
// версия схемы и $schema заданы, источник записан; исходный набор не тронут.
func TestNewCustomFromUnmarksBuiltin(t *testing.T) {
	src := builtinByID(t, "lav-ru")
	beforeName := src.Name["en"]
	beforeDomain := src.Routing.DirectDomain[0]

	c := NewCustomFrom(src, "custom:copy")
	if c.Builtin || c.ID != "custom:copy" || c.Revision != 1 || c.Version != SchemaVersion || c.Schema != SchemaRef {
		t.Fatalf("копия: builtin=%v id=%q rev=%d ver=%d schema=%q", c.Builtin, c.ID, c.Revision, c.Version, c.Schema)
	}
	if c.SourceID != "lav-ru" || c.SourceRevision != src.Revision {
		t.Fatalf("источник: %q rev %d, want lav-ru rev %d", c.SourceID, c.SourceRevision, src.Revision)
	}
	if !reflect.DeepEqual(c.Routing, src.Routing) || !reflect.DeepEqual(c.DNS, src.DNS) {
		t.Fatal("маршруты и DNS копии должны совпадать с исходными")
	}
	// Глубокая копия: правка копии не меняет исходный набор.
	c.Routing.DirectDomain[0] = "changed.example"
	c.Name["en"] = "Changed"
	if src.Routing.DirectDomain[0] != beforeDomain || src.Name["en"] != beforeName {
		t.Fatal("копия делит память с исходным набором")
	}
	if !src.Builtin || src.ID != "lav-ru" {
		t.Fatal("исходный набор изменён")
	}
	if err := NewCustomFrom(src, "custom:copy").Validate(); err != nil {
		t.Fatalf("копия не проходит Validate: %v", err)
	}
}

// Копия пользовательского набора наследует его источник (сброс остаётся доступен).
func TestNewCustomFromCustomKeepsSource(t *testing.T) {
	first := NewCustomFrom(builtinByID(t, "global"), "custom:a")
	first.Revision = 7
	second := NewCustomFrom(first, "custom:b")
	if second.SourceID != "global" || second.SourceRevision != first.SourceRevision || second.Revision != 1 {
		t.Fatalf("second: source=%q/%d rev=%d", second.SourceID, second.SourceRevision, second.Revision)
	}
	scratch := &Preset{ID: "custom:z", Name: Localized{"en": "Z"}}
	if c := NewCustomFrom(scratch, "custom:y"); c.SourceID != "" || c.SourceRevision != 0 {
		t.Fatalf("набор с нуля получил источник: %+v", c)
	}
}
