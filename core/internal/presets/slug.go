// SPDX-License-Identifier: GPL-3.0-only

package presets

// slug.go — id пользовательских наборов: `custom:<slug>` (T-80). Slug строится
// из имени: латиница в нижнем регистре, кириллица транслитом, всё прочее —
// разделитель «-». Результат безопасен как часть id, ключ мапы и имя файла.

import (
	"strconv"
	"strings"
)

const (
	// CustomIDPrefix — префикс id пользовательских наборов (id встроенных
	// его не содержат, поэтому пересечься с ними нельзя).
	CustomIDPrefix = "custom:"
	// SlugMax — предел длины slug (без префикса).
	SlugMax = 48
	// slugFallback — slug, когда из имени не получилось ни одного символа.
	slugFallback = "set"
	// mintLimit — страховка от зацикливания, если taken всегда отвечает «занято».
	mintLimit = 1 << 16
)

// translit — кириллица и частые латинские буквы с диакритикой → ASCII.
var translit = map[rune]string{
	'а': "a", 'б': "b", 'в': "v", 'г': "g", 'д': "d", 'е': "e", 'ё': "e", 'ж': "zh",
	'з': "z", 'и': "i", 'й': "y", 'к': "k", 'л': "l", 'м': "m", 'н': "n", 'о': "o",
	'п': "p", 'р': "r", 'с': "s", 'т': "t", 'у': "u", 'ф': "f", 'х': "kh", 'ц': "ts",
	'ч': "ch", 'ш': "sh", 'щ': "shch", 'ъ': "", 'ы': "y", 'ь': "", 'э': "e", 'ю': "yu",
	'я': "ya", 'і': "i", 'ї': "yi", 'є': "ye", 'ґ': "g",
	'à': "a", 'á': "a", 'â': "a", 'ã': "a", 'ä': "a", 'å': "a", 'æ': "ae", 'ç': "c",
	'è': "e", 'é': "e", 'ê': "e", 'ë': "e", 'ì': "i", 'í': "i", 'î': "i", 'ï': "i",
	'ñ': "n", 'ò': "o", 'ó': "o", 'ô': "o", 'õ': "o", 'ö': "o", 'ø': "o", 'œ': "oe",
	'ù': "u", 'ú': "u", 'û': "u", 'ü': "u", 'ý': "y", 'ÿ': "y", 'ß': "ss",
}

// Slugify приводит имя к slug: только [a-z0-9-], без «-» по краям и подряд,
// 1..SlugMax символов. Имя без пригодных символов даёт "set".
func Slugify(name string) string {
	var sb strings.Builder
	sep := true // «-» в начале не нужен
	emit := func(s string) {
		for _, c := range s {
			if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' {
				sb.WriteRune(c)
				sep = false
			} else if !sep {
				sb.WriteByte('-')
				sep = true
			}
		}
	}
	for _, r := range strings.ToLower(name) {
		if tr, ok := translit[r]; ok {
			emit(tr)
		} else if r < 0x80 {
			emit(string(r))
		} else if !sep { // прочие письменности (иероглифы и т.п.) — разделитель
			sb.WriteByte('-')
			sep = true
		}
	}
	return cutSlug(sb.String(), SlugMax)
}

// cutSlug обрезает slug до max символов и убирает «-» по краям; пустой → "set".
func cutSlug(s string, max int) string {
	if len(s) > max {
		s = s[:max]
	}
	s = strings.Trim(s, "-")
	if s == "" {
		return slugFallback
	}
	return s
}

// MintID выдаёт свободный id `custom:<slug>`: при коллизии добавляет -2, -3…
// (суффикс укладывается в SlugMax, свободная «дырка» занимается первой).
// taken сообщает, занят ли id; чистая функция — состояния не хранит.
func MintID(name string, taken func(id string) bool) string {
	base := Slugify(name)
	if id := CustomIDPrefix + base; !taken(id) {
		return id
	}
	for n := 2; n < mintLimit; n++ {
		suffix := "-" + strconv.Itoa(n)
		head := strings.Trim(base[:min(len(base), SlugMax-len(suffix))], "-")
		if head == "" {
			head = slugFallback
		}
		if id := CustomIDPrefix + head + suffix; !taken(id) {
			return id
		}
	}
	return CustomIDPrefix + base + "-" + strconv.Itoa(mintLimit)
}
