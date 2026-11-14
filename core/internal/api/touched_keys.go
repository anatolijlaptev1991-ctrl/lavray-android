// SPDX-License-Identifier: GPL-3.0-only

package api

// touched_keys.go — какие части конфига меняют custom-поля (T-08B): пути
// JSON до touchedKeysDepth уровней — "dns.servers", "outbounds[0].tls".
// Значения не выводятся, только пути.

import (
	"reflect"
	"sort"
	"strconv"
)

const (
	touchedKeysDepth = 3   // сегментов пути: ключ объекта или [индекс]
	maxTouchedKeys   = 100 // больше путей UI всё равно не покажет
)

// touchedKeys — отсортированные пути, по которым a и b различаются. Конфиги —
// в виде encoding/json (map[string]any, []any, скаляры); nil — пустой объект.
func touchedKeys(a, b map[string]any) []string {
	if a == nil {
		a = map[string]any{}
	}
	if b == nil {
		b = map[string]any{}
	}
	set := map[string]bool{}
	walkTouched("", a, b, touchedKeysDepth+1, set)
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// capTouchedKeys — первые maxTouchedKeys путей для ответа.
func capTouchedKeys(keys []string) []string {
	if len(keys) > maxTouchedKeys {
		return keys[:maxTouchedKeys]
	}
	return keys
}

// walkTouched сравнивает значения по пути path; depth — сколько сегментов
// ещё можно добавить к пути (корень не считается).
func walkTouched(path string, a, b any, depth int, out map[string]bool) {
	if reflect.DeepEqual(a, b) {
		return
	}
	if depth == 1 && path != "" {
		out[path] = true
		return
	}
	am, aObj := a.(map[string]any)
	bm, bObj := b.(map[string]any)
	if aObj && bObj {
		for k := range unionKeys(am, bm) {
			walkTouched(joinKey(path, k), am[k], bm[k], depth-1, out)
		}
		return
	}
	as, aArr := a.([]any)
	bs, bArr := b.([]any)
	if aArr && bArr && len(as) == len(bs) && path != "" {
		for i := range as {
			walkTouched(path+"["+strconv.Itoa(i)+"]", as[i], bs[i], depth-1, out)
		}
		return
	}
	if path != "" {
		out[path] = true
	}
}

func unionKeys(a, b map[string]any) map[string]bool {
	keys := make(map[string]bool, len(a)+len(b))
	for k := range a {
		keys[k] = true
	}
	for k := range b {
		keys[k] = true
	}
	return keys
}

func joinKey(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}
