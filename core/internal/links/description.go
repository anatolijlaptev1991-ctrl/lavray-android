// SPDX-License-Identifier: GPL-3.0-only

package links

// description.go — служебный хвост имени сервера «?serverDescription=<base64>»
// (0.2.1). Панели подписок дописывают к имени описание сервера: UTF-8 текст в
// base64. Хвост — не часть имени: в списке серверов он выглядел мусором, а у
// длинного описания мог вывести имя за предел maxRemarkLength, и вся ссылка
// отвергалась. Хвост отрезается, а описание, если оно разобралось, попадает в
// заметки профиля. Некорректный base64 — хвост просто отрезается.

import (
	"encoding/base64"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

const (
	// descriptionMarker — начало хвоста в имени.
	descriptionMarker = "?serverDescription="
	// descriptionMarkerEncoded — то же, если «?» и «=» в ссылке закодированы.
	descriptionMarkerEncoded = "%3FserverDescription%3D"
	// maxDescriptionLength — предел текста описания в заметках, байт.
	maxDescriptionLength = 2048
)

// lastIndexFold — позиция последнего вхождения ASCII-маркера без учёта регистра.
func lastIndexFold(s, marker string) int {
	for i := len(s) - len(marker); i >= 0; i-- {
		if strings.EqualFold(s[i:i+len(marker)], marker) {
			return i
		}
	}
	return -1
}

// decodeDescription разбирает значение хвоста: base64 (обычный или url-safe, с
// паддингом и без) от UTF-8 текста. Пусто — разобрать не удалось.
func decodeDescription(value string) string {
	value = strings.TrimSpace(value)
	if i := strings.IndexByte(value, '&'); i >= 0 {
		value = value[:i]
	}
	if value == "" {
		return ""
	}
	for _, enc := range []*base64.Encoding{
		base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding,
	} {
		raw, err := enc.DecodeString(value)
		if err != nil || !utf8.Valid(raw) {
			continue
		}
		text := strings.TrimSpace(strings.Map(func(r rune) rune {
			if r < ' ' && r != '\n' {
				return -1 // управляющие символы в заметки не нужны
			}
			return r
		}, string(raw)))
		for len(text) > maxDescriptionLength {
			_, size := utf8.DecodeLastRuneInString(text)
			text = text[:len(text)-size]
		}
		return text
	}
	return ""
}

// stripNameDescription отделяет от имени хвост «?serverDescription=…» и
// возвращает имя без него и разобранное описание ("" — описания нет или оно
// не разобралось).
func stripNameDescription(name string) (clean, description string) {
	i := lastIndexFold(name, descriptionMarker)
	if i < 0 {
		return name, ""
	}
	return strings.TrimRight(name[:i], " \t"), decodeDescription(name[i+len(descriptionMarker):])
}

// splitLinkDescription отрезает хвост от фрагмента (после «#») сырой ссылки до
// разбора: имя с длинным описанием не должно упираться в предел длины. Хвост
// ищется и в виде «?serverDescription=», и закодированным (%3F…%3D).
func splitLinkDescription(raw string) (rest, description string) {
	hash := strings.LastIndexByte(raw, '#')
	if hash < 0 {
		return raw, ""
	}
	frag := raw[hash+1:]
	for _, marker := range []string{descriptionMarker, descriptionMarkerEncoded} {
		i := lastIndexFold(frag, marker)
		if i < 0 {
			continue
		}
		value := frag[i+len(marker):]
		if marker == descriptionMarkerEncoded {
			if dec, err := url.PathUnescape(value); err == nil {
				value = dec
			}
		}
		return raw[:hash+1+i], decodeDescription(value)
	}
	return raw, ""
}

// applyServerDescription убирает хвост из имени разобранного профиля (vmess
// v2rayN хранит имя в JSON, а не во фрагменте) и кладёт описание в заметки,
// если они пусты.
func applyServerDescription(p *model.Profile, fromLink string) {
	name, fromName := stripNameDescription(p.Name)
	p.Name = name
	description := fromLink
	if description == "" {
		description = fromName
	}
	if p.Notes == "" {
		p.Notes = description
	}
}
