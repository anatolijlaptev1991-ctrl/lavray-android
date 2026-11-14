// SPDX-License-Identifier: GPL-3.0-only

package nekoimport

import (
	"encoding/json"
	"math"
	"strconv"
)

// settings_conv.go — приведение значения DataStore к типу настройки LAVRay.
// Каждая функция возвращает значение как JSON и false, если тип не подходит.

func asString(v json.RawMessage) (string, bool) {
	var s string
	if json.Unmarshal(v, &s) != nil {
		return "", false
	}
	b, _ := json.Marshal(s)
	return string(b), true
}

func asBool(v json.RawMessage) (string, bool) {
	var b bool
	if json.Unmarshal(v, &b) == nil {
		return strconv.FormatBool(b), true
	}
	var n float64 // Qt иногда пишет 0/1
	if json.Unmarshal(v, &n) == nil && (n == 0 || n == 1) {
		return strconv.FormatBool(n == 1), true
	}
	return "", false
}

func asInt(v json.RawMessage) (string, bool) {
	var n float64
	if json.Unmarshal(v, &n) != nil || n != math.Trunc(n) || math.Abs(n) > 1<<53 {
		return "", false
	}
	return strconv.FormatInt(int64(n), 10), true
}

func asStringList(v json.RawMessage) (string, bool) {
	var list []string
	if json.Unmarshal(v, &list) != nil {
		return "", false
	}
	if list == nil {
		list = []string{}
	}
	b, _ := json.Marshal(list)
	return string(b), true
}

// portToEnabled: core_box_clash_api в NekoBox — порт Clash API, отрицательный
// (по умолчанию -9090) значит «выключен».
func portToEnabled(v json.RawMessage) (string, bool) {
	var n float64
	if json.Unmarshal(v, &n) != nil {
		return asBool(v)
	}
	return strconv.FormatBool(n > 0), true
}

// oneOf — строка из заданного набора. Темы и языки Qt (имя стиля, номер языка)
// в LAVRay не имеют смысла: подходит только значение, которое LAVRay понимает.
func oneOf(allowed ...string) func(json.RawMessage) (string, bool) {
	return func(v json.RawMessage) (string, bool) {
		var s string
		if json.Unmarshal(v, &s) != nil {
			return "", false
		}
		for _, a := range allowed {
			if s == a {
				b, _ := json.Marshal(s)
				return string(b), true
			}
		}
		return "", false
	}
}
