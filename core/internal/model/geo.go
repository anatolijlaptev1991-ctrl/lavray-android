// SPDX-License-Identifier: GPL-3.0-only

// geo.go — определение страны сервера для группировки в UI (FR-UI-008/009):
// «несколько ключей по странам» — обычный случай подписки, поэтому каждый
// профиль получает ISO-код (ru/jp/nl/…) из имени (эмодзи-флаг, код страны,
// англ./рус. название) или из домена/адреса. Неизвестно — пусто.
package model

import (
	"strings"
	"unicode"
)

// geoAliases — нормализованные алиасы → ISO 3166-1 alpha-2 (нижний регистр).
// Только ходовые для прокси-подписок страны; расширяется без миграций.
var geoAliases = map[string]string{
	// Русские названия и сокращения
	"россия": "ru", "рф": "ru", "russia": "ru", "russian": "ru", "москва": "ru", "moscow": "ru",
	"германия": "de", "germany": "de", "франкфурт": "de", "frankfurt": "de", "berlin": "de",
	"нидерланды": "nl", "netherlands": "nl", "holland": "nl", "amsterdam": "nl", "амстердам": "nl",
	"сша": "us", "usa": "us", "united states": "us", "america": "us", "new york": "us", "нью-йорк": "us",
	"финляндия": "fi", "finland": "fi", "helsinki": "fi", "хельсинки": "fi",
	"швеция": "se", "sweden": "se", "stockholm": "se", "стокгольм": "se",
	"япония": "jp", "japan": "jp", "tokyo": "jp", "токио": "jp", "osaka": "jp",
	"сингапур": "sg", "singapore": "sg",
	"китай": "cn", "china": "cn", "hong kong": "hk", "hongkong": "hk", "гонконг": "hk",
	"тайвань": "tw", "taiwan": "tw",
	"корея": "kr", "korea": "kr", "south korea": "kr", "seoul": "kr", "сеул": "kr",
	"турция": "tr", "turkey": "tr", "istanbul": "tr", "стамбул": "tr",
	"польша": "pl", "poland": "pl", "warsaw": "pl", "варшава": "pl",
	"чехия": "cz", "czech": "cz", "prague": "cz", "прага": "cz",
	"австрия": "at", "austria": "at", "vienna": "at", "вена": "at",
	"швейцария": "ch", "switzerland": "ch", "zurich": "ch", "цюрих": "ch",
	"великобритания": "gb", "uk": "gb", "united kingdom": "gb", "britain": "gb", "london": "gb", "лондон": "gb",
	"франция": "fr", "france": "fr", "paris": "fr", "париж": "fr",
	"италия": "it", "italy": "it", "milan": "it", "милан": "it",
	"испания": "es", "spain": "es", "madrid": "es", "мадрид": "es",
	"канада": "ca", "canada": "ca", "toronto": "ca", "торонто": "ca",
	"бразилия": "br", "brazil": "br",
	"австралия": "au", "australia": "au", "sydney": "au",
	"индия": "in", "india": "in",
	"армения": "am", "armenia": "am", "yerevan": "am", "ереван": "am",
	"казахстан": "kz", "kazakhstan": "kz", "almaty": "kz", "алматы": "kz",
	"украина": "ua", "ukraine": "ua", "kyiv": "ua", "киев": "ua",
	"латвия": "lv", "latvia": "lv", "riga": "lv", "рига": "lv",
	"литва": "lt", "lithuania": "lt", "vilnius": "lt", "вильнюс": "lt",
	"эстония": "ee", "estonia": "ee", "tallinn": "ee", "таллин": "ee",
	"молдова": "md", "moldova": "md",
	"сербия": "rs", "serbia": "rs", "belgrade": "rs",
	"румыния": "ro", "romania": "ro", "bucharest": "ro",
	"болгария": "bg", "bulgaria": "bg", "sofia": "bg",
	"греция": "gr", "greece": "gr", "athens": "gr",
	"норвегия": "no", "norway": "no", "oslo": "no",
	"дания": "dk", "denmark": "dk", "copenhagen": "dk",
	"ирландия": "ie", "ireland": "ie", "dublin": "ie",
	"израиль": "il", "israel": "il", "tel aviv": "il",
	"оаэ": "ae", "uae": "ae", "dubai": "ae", "emirates": "ae",
	"юар": "za", "south africa": "za",
	"мексика": "mx", "mexico": "mx",
	"аргентина": "ar", "argentina": "ar",
	"чили": "cl", "chile": "cl",
	"вьетнам": "vn", "vietnam": "vn",
	"таиланд": "th", "thailand": "th", "bangkok": "th",
	"индонезия": "id", "indonesia": "id", "jakarta": "id",
	"малайзия": "my", "malaysia": "my",
	"филиппины": "ph", "philippines": "ph",
	"пакистан": "pk", "pakistan": "pk",
	"грузия": "ge", "georgia": "ge", "tbilisi": "ge",
	"азербайджан": "az", "azerbaijan": "az", "baku": "az",
	"беларусь": "by", "belarus": "by", "minsk": "by", "минск": "by",
	"узбекистан": "uz", "uzbekistan": "uz", "tashkent": "uz",
	"киргизия": "kg", "kyrgyzstan": "kg",
	"люксембург": "lu", "luxembourg": "lu",
	"бельгия": "be", "belgium": "be", "brussels": "be",
	"португалия": "pt", "portugal": "pt", "lisbon": "pt",
	"венгрия": "hu", "hungary": "hu", "budapest": "hu",
	"хорватия": "hr", "croatia": "hr",
	"словения": "si", "slovenia": "si",
	"словакия": "sk", "slovakia": "sk",
	"исландия": "is", "iceland": "is",
	"новая зеландия": "nz", "new zealand": "nz",
	"саудовская аравия": "sa", "saudi arabia": "sa",
	"египет": "eg", "egypt": "eg",
	"нигерия": "ng", "nigeria": "ng",
	"кения": "ke", "kenya": "ke",
	"колумбия": "co", "colombia": "co",
	"перу": "pe", "peru": "pe",
}

// flagToISO — эмодзи-флаг (два regional indicator) → ISO-код.
func flagToISO(name string) (string, bool) {
	runes := []rune(name)
	for i := 0; i+1 < len(runes); i++ {
		a, b := runes[i], runes[i+1]
		if a >= 0x1F1E6 && a <= 0x1F1FF && b >= 0x1F1E6 && b <= 0x1F1FF {
			return string([]rune{unicode.ToLower(a - 0x1F1E6 + 'A'), unicode.ToLower(b - 0x1F1E6 + 'A')}), true
		}
	}
	return "", false
}

// DetectGeo определяет ISO-код страны профиля по имени/адресу (FR-UI-008).
// Порядок: эмодзи-флаг → явный код вида «[NL]»/«_nl_»/«-JP» → алиас названия
// в имени → TLD адреса (.ru/.de), кроме общих (com/net/org/io).
func DetectGeo(name, address string) string {
	if code, ok := flagToISO(name); ok {
		return code
	}
	// Явные коды стран в скобках/разделителях: [NL], (de), _jp_, -us-
	for _, sep := range [][2]string{{"[", "]"}, {"(", ")"}, {"_", "_"}, {"-", "-"}} {
		if code := codeBetween(name, sep[0], sep[1]); code != "" {
			return code
		}
	}
	lower := strings.ToLower(name)
	// Алиасы — от длинных к коротким, чтобы «new zealand» не поймал «zealand».
	best := ""
	bestLen := 0
	for alias, code := range geoAliases {
		if len(alias) > bestLen && containsAlias(lower, alias) {
			best, bestLen = code, len(alias)
		}
	}
	if best != "" {
		return best
	}
	// TLD адреса (только двухбуквенные национальные, кроме служебных).
	if i := strings.LastIndexByte(address, '.'); i >= 0 && i+2 < len(address) {
		tld := strings.ToLower(address[i+1:])
		if len(tld) == 2 && geoAliases[tld] == "" && tld != "io" && tld != "to" && tld != "me" && tld != "tv" && tld != "cc" && tld != "ai" {
			return tld
		}
	}
	return ""
}

// codeBetween ищет двухбуквенный код между разделителями.
func codeBetween(s, left, right string) string {
	for i := 0; i+4 <= len(s); i++ {
		if string(s[i]) == left && string(s[i+3]) == right {
			code := strings.ToLower(s[i+1 : i+3])
			// T-54: только настоящий код страны и не тег протокола (ws, ss, cf…).
			if isAlpha2(code) && isoCountryCodes[code] && !notCountryTags[code] {
				return code
			}
		}
	}
	return ""
}

func isAlpha2(s string) bool {
	if len(s) != 2 {
		return false
	}
	for _, r := range s {
		if r < 'a' || r > 'z' {
			return false
		}
	}
	return true
}
