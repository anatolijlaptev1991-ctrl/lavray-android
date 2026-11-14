// SPDX-License-Identifier: GPL-3.0-only

package model

// Вспомогательные таблицы DetectGeo (T-54, B2-21): ложные срабатывания
// «vless-ws-cdn → Самоа», «Fukuoka → GB».

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// isoCountryCodes — все 249 кодов ISO 3166-1 alpha-2 (нижний регистр). Явный
// код страны в имени сервера («[NL]», «_jp_», «-us-») принимается, только если
// это настоящий код: раньше за страну считались ЛЮБЫЕ две буквы между
// разделителями.
var isoCountryCodes = func() map[string]bool {
	const list = "ad ae af ag ai al am ao aq ar as at au aw ax az ba bb bd be bf bg bh bi bj bl bm bn bo bq br bs bt bv bw by bz " +
		"ca cc cd cf cg ch ci ck cl cm cn co cr cu cv cw cx cy cz de dj dk dm do dz ec ee eg eh er es et fi fj fk fm fo fr " +
		"ga gb gd ge gf gg gh gi gl gm gn gp gq gr gs gt gu gw gy hk hm hn hr ht hu id ie il im in io iq ir is it je jm jo jp " +
		"ke kg kh ki km kn kp kr kw ky kz la lb lc li lk lr ls lt lu lv ly ma mc md me mf mg mh mk ml mm mn mo mp mq mr ms mt mu mv mw mx my mz " +
		"na nc ne nf ng ni nl no np nr nu nz om pa pe pf pg ph pk pl pm pn pr ps pt pw py qa re ro rs ru rw " +
		"sa sb sc sd se sg sh si sj sk sl sm sn so sr ss st sv sx sy sz tc td tf tg th tj tk tl tm tn to tr tt tv tw tz " +
		"ua ug um us uy uz va vc ve vg vi vn vu wf ws ye yt za zm zw"
	m := make(map[string]bool, 249)
	for _, c := range strings.Fields(list) {
		m[c] = true
	}
	return m
}()

// notCountryTags — двухбуквенные теги протоколов и транспорта, которые
// совпадают с кодами стран: ws (WebSocket, Самоа), ss (Shadowsocks, Южный
// Судан), cf (Cloudflare, ЦАР), tg (Telegram, Того), tc (TCP, Тёркс и Кайкос).
var notCountryTags = map[string]bool{"ws": true, "ss": true, "cf": true, "tg": true, "tc": true}

// containsAlias — алиас входит в имя (lower — имя в нижнем регистре). Короткие
// алиасы (≤ 3 символов: uk, usa, uae, рф…) совпадают только целым словом, иначе
// «Fukuoka» давал uk → GB, «Busan» — usa → US, «Bukhara» — uk → GB. Цифры и
// знаки словом не считаются («usa1», «uk-2» — совпадают). Длинные алиасы ищутся
// подстрокой, как раньше («JapanTokyo»).
func containsAlias(lower, alias string) bool {
	if utf8.RuneCountInString(alias) > 3 {
		return strings.Contains(lower, alias)
	}
	for from := 0; from < len(lower); {
		i := strings.Index(lower[from:], alias)
		if i < 0 {
			return false
		}
		start, end := from+i, from+i+len(alias)
		before, _ := utf8.DecodeLastRuneInString(lower[:start])
		after, _ := utf8.DecodeRuneInString(lower[end:])
		if !unicode.IsLetter(before) && !unicode.IsLetter(after) {
			return true
		}
		from = start + 1
	}
	return false
}
