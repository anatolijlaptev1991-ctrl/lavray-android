// SPDX-License-Identifier: GPL-3.0-only

package presets

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestExportJSONRoundTrip (H/D): экспорт читается обратно в тот же набор, пустые
// списки — «[]», а не «null».
func TestExportJSONRoundTrip(t *testing.T) {
	p := NewCustomFrom(builtinByID(t, "lav-ru"), "custom:rt")
	p.Routing.ProxyIP = nil
	data, err := ExportJSON(p)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.HasSuffix(s, "}\n") || !strings.Contains(s, "\n  \"routing\": {") || strings.Contains(s, "null") {
		t.Fatalf("формат экспорта:\n%.400s", s)
	}
	got, warns, err := DetectAndParse(data, "")
	if err != nil || len(warns) != 0 {
		t.Fatalf("parse: %v %v", err, warns)
	}
	again, _ := ExportJSON(got)
	if string(again) != s {
		t.Fatal("повторный экспорт отличается от первого")
	}
	if got.ID != "custom:rt" || got.SourceID != "lav-ru" || got.Routing.ProxyIP == nil {
		t.Errorf("поля после чтения: id=%q source=%q", got.ID, got.SourceID)
	}
}

// TestDetectAndParseNekoRay: маршрут NekoRay распознаётся без подсказки и
// превращается в пользовательский набор с предупреждениями конвертера.
func TestDetectAndParseNekoRay(t *testing.T) {
	src, err := os.ReadFile(filepath.Join(testdataDir, "lav-ru.source.json"))
	if err != nil {
		t.Fatal(err)
	}
	p, warns, err := DetectAndParse(src, "Мой маршрут")
	if err != nil {
		t.Fatal(err)
	}
	if p.Builtin || p.Name["en"] != "Мой маршрут" || len(p.Routing.ProcessRules) == 0 {
		t.Fatalf("набор: builtin=%v name=%v rules=%d", p.Builtin, p.Name, len(p.Routing.ProcessRules))
	}
	if len(warns) == 0 {
		t.Error("эталонный источник даёт предупреждения (DROP-поля), их нет в ответе")
	}
	if err := p.Validate(); err != nil {
		t.Errorf("результат невалиден: %v", err)
	}
	p2, _, err := DetectAndParse([]byte(`{"direct_domain":"example.org","def_outbound":"direct"}`), "")
	if err != nil || p2.Routing.DefOutbound != "direct" || !reflect.DeepEqual(p2.Routing.DirectDomain, []string{"example.org"}) {
		t.Fatalf("минимальный маршрут: %+v %v", p2, err)
	}
}

// TestDetectAndParseInvalid (I/E): мусор, пустой ввод и «не тот объект».
func TestDetectAndParseInvalid(t *testing.T) {
	for _, in := range []string{"", "   ", "not json", "[]", "42", "null", `"x"`, `{}`, `{"unknown":1}`, `{"routing":5}`, `{"routing":null}`, `{"routing":{"def_outbound":7}}`} {
		if _, _, err := DetectAndParse([]byte(in), ""); !errors.Is(err, ErrFormat) {
			t.Errorf("DetectAndParse(%q): err = %v, want ErrFormat", in, err)
		}
	}
	for _, v := range []string{"0", "2", "-1"} {
		in := `{"version":` + v + `,"id":"x","name":{"en":"X"},"routing":{"def_outbound":"proxy"},"dns":{"mode":"simple"}}`
		if _, _, err := DetectAndParse([]byte(in), ""); !errors.Is(err, ErrFormat) || !strings.Contains(err.Error(), "version") {
			t.Errorf("version %s: err = %v", v, err)
		}
	}
	if _, _, err := DetectAndParse([]byte(`{"def_outbound":"weird"}`), ""); err == nil {
		t.Error("недопустимый def_outbound NekoRay принят")
	}
}

// TestDetectAndParseSizeCap (B): ровно 1 МиБ читается, на байт больше — ErrTooLarge.
func TestDetectAndParseSizeCap(t *testing.T) {
	body := `{"direct_domain":"example.org"}`
	ok := body + strings.Repeat(" ", MaxImportBytes-len(body))
	if _, _, err := DetectAndParse([]byte(ok), ""); err != nil {
		t.Fatalf("ровно на пределе: %v", err)
	}
	if _, _, err := DetectAndParse([]byte(ok+" "), ""); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("на байт больше: %v", err)
	}
}

// TestDetectKeepsRawFields: разбор только читает файл и не переписывает поля;
// подмену id, builtin и источника делает вставка (api.PlanInsert, там же тест S).
func TestDetectKeepsRawFields(t *testing.T) {
	p := NewCustomFrom(builtinByID(t, "global"), "custom:x")
	data, _ := ExportJSON(p)
	got, _, err := DetectAndParse(data, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "custom:x" || got.SourceID != "global" {
		t.Fatalf("разбор исказил поля: %+v", got)
	}
}

// ParseNekoRoute: только объект; «null», массив и пустой ввод — ErrFormat, а
// пустой объект — допустимый маршрут со значениями по умолчанию.
func TestParseNekoRouteStrict(t *testing.T) {
	for _, in := range []string{"", "null", "[]", `"x"`, "{oops"} {
		if _, _, err := ParseNekoRoute([]byte(in), "n"); !errors.Is(err, ErrFormat) {
			t.Errorf("ParseNekoRoute(%q): err = %v, want ErrFormat", in, err)
		}
	}
	p, _, err := ParseNekoRoute([]byte(`{}`), "Пустой")
	if err != nil || p.Name["en"] != "Пустой" || p.Routing.DefOutbound != "proxy" {
		t.Fatalf("{}: %+v %v", p, err)
	}
}
