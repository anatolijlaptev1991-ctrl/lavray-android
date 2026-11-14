// SPDX-License-Identifier: GPL-3.0-only

package presets

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"
)

var slugRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// TestSlugifyCyrillic (L): кириллица переводится транслитом, регистр и
// разделители приводятся к «a-b-c».
func TestSlugifyCyrillic(t *testing.T) {
	cases := map[string]string{
		"Мой набор":         "moy-nabor",
		"Щука и Ёж":         "shchuka-i-ezh",
		"  LAV  Preset RU ": "lav-preset-ru",
		"Игры (Steam)":      "igry-steam",
		"Café №1":           "cafe-1",
		"Straße":            "strasse",
		"Київ":              "kiyiv",
	}
	for in, want := range cases {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestSlugifyEmptyFallback (E): пустое имя и имя без допустимых символов
// дают запасной «set».
func TestSlugifyEmptyFallback(t *testing.T) {
	for _, in := range []string{"", "   ", "!!!", "游戏", "---", "ъь"} {
		if got := Slugify(in); got != "set" {
			t.Errorf("Slugify(%q) = %q, want set", in, got)
		}
	}
}

// TestSlugifyLengthCut (B): длинное имя обрезается до 48 символов без «-» на конце.
func TestSlugifyLengthCut(t *testing.T) {
	long := strings.Repeat("abcdefg ", 20)
	got := Slugify(long)
	if len(got) > SlugMax || strings.HasSuffix(got, "-") || !slugRe.MatchString(got) {
		t.Fatalf("Slugify(long) = %q (len %d)", got, len(got))
	}
	if got := Slugify(strings.Repeat("a", 47) + " b"); got != strings.Repeat("a", 47) {
		t.Errorf("обрезка по границе слова оставила «-»: %q", got)
	}
}

// TestMintIDCollision (D): занятый id получает суффикс -2, -3…; свободный —
// возвращается как есть; суффикс не выводит slug за 48 символов.
func TestMintIDCollision(t *testing.T) {
	taken := map[string]bool{}
	take := func(id string) bool { return taken[id] }
	var ids []string
	for i := 0; i < 4; i++ {
		id := MintID("Мой набор", take)
		taken[id] = true
		ids = append(ids, id)
	}
	want := []string{"custom:moy-nabor", "custom:moy-nabor-2", "custom:moy-nabor-3", "custom:moy-nabor-4"}
	if fmt.Sprint(ids) != fmt.Sprint(want) {
		t.Fatalf("ids = %v, want %v", ids, want)
	}
	// Занят «дыркой»: -2 свободен, если его убрали.
	delete(taken, "custom:moy-nabor-2")
	if id := MintID("Мой набор", take); id != "custom:moy-nabor-2" {
		t.Errorf("дырка не занята: %q", id)
	}
	// Длинный slug + суффикс остаётся в пределах 48.
	long := strings.Repeat("x", 60)
	taken["custom:"+strings.Repeat("x", SlugMax)] = true
	id := MintID(long, take)
	if n := len(strings.TrimPrefix(id, CustomIDPrefix)); n > SlugMax {
		t.Errorf("slug с суффиксом = %d символов: %q", n, id)
	}
	if id == "custom:"+strings.Repeat("x", SlugMax) {
		t.Errorf("занятый id выдан повторно")
	}
	// Пустое имя — запасной вариант.
	if id := MintID("", func(string) bool { return false }); id != "custom:set" {
		t.Errorf("пустое имя: %q", id)
	}
}

// FuzzSlugify (S): на любой строке — только [a-z0-9-], 1..48 символов, без «-»
// по краям и подряд; MintID всегда даёт id с префиксом custom:.
func FuzzSlugify(f *testing.F) {
	for _, s := range []string{"", "Мой набор", "a b", "../../etc", "\x00\xff", "游戏 Steam", strings.Repeat("я", 100), "custom:x", "---a---"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		got := Slugify(in)
		if !utf8.ValidString(got) || len(got) == 0 || len(got) > SlugMax || !slugRe.MatchString(got) {
			t.Fatalf("Slugify(%q) = %q — недопустимый slug", in, got)
		}
		id := MintID(in, func(string) bool { return false })
		if !strings.HasPrefix(id, CustomIDPrefix) || !slugRe.MatchString(strings.TrimPrefix(id, CustomIDPrefix)) {
			t.Fatalf("MintID(%q) = %q", in, id)
		}
	})
}
