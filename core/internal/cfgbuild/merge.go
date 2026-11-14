// SPDX-License-Identifier: GPL-3.0-only

// merge.go — MergeJson (порт db/ConfigBuilder.cpp:32–68) и allowlist-мерж
// пользовательского custom_config (core-port-plan §3.1.1, LNB-25).
package cfgbuild

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// MergeJSON рекурсивно мержит src в dst — семантика 1:1 с C++-оракулом
// MergeJson (ConfigBuilder.cpp):
//   - ключ есть в dst и оба значения — объекты → рекурсивный мерж;
//   - значения-массивы при ОТСУТСТВИИ ключа в dst:
//     "+key"  → dst[key2] = src-массив ++ dst[key2] (элементы dst в конец);
//     "key+"  → dst[key2] = dst[key2] ++ src-массив (элементы src в конец);
//     key     → перезапись;
//   - во всех остальных случаях (включая массив при наличии ключа в dst) —
//     значение src перезаписывает dst.
func MergeJSON(dst, src map[string]any) {
	for key, vSrc := range src {
		if vDst, ok := dst[key]; ok {
			srcObj, srcIsObj := vSrc.(map[string]any)
			dstObj, dstIsObj := vDst.(map[string]any)
			if srcIsObj && dstIsObj {
				MergeJSON(dstObj, srcObj)
				dst[key] = dstObj
				continue
			}
			dst[key] = vSrc
			continue
		}
		if srcArr, isArr := vSrc.([]any); isArr {
			switch {
			case strings.HasPrefix(key, "+"):
				key2 := strings.TrimPrefix(key, "+")
				dstArr, _ := dst[key2].([]any)
				// QJSONARRAY_ADD(v_src_arr, v_dst_arr): элементы dst в конец.
				merged := make([]any, 0, len(srcArr)+len(dstArr))
				merged = append(merged, srcArr...)
				merged = append(merged, dstArr...)
				dst[key2] = merged
			case strings.HasSuffix(key, "+"):
				key2 := strings.TrimSuffix(key, "+")
				dstArr, _ := dst[key2].([]any)
				// QJSONARRAY_ADD(v_dst_arr, v_src_arr): элементы src в конец.
				merged := make([]any, 0, len(srcArr)+len(dstArr))
				merged = append(merged, dstArr...)
				merged = append(merged, srcArr...)
				dst[key2] = merged
			default:
				dst[key] = vSrc
			}
			continue
		}
		dst[key] = vSrc
	}
}

// ---- Allowlist (§3.1.1, LNB-25) -------------------------------------------
//
// Пользовательский custom_config мержится в конфиг ТОЛЬКО по белому списку
// корневых секций. Всё вне списка (в т.ч. inbounds, log.output, experimental
// кроме cache_file) → INVALID_ARGUMENT с именем ключа.

// allowedRootKeys — корневые секции, разрешённые для мержа custom_config.
var allowedRootKeys = map[string]bool{
	"route":        true, // rules/rule_set/default_domain_resolver/…
	"dns":          true, // кроме запрещённых подсекций ниже (наследуено W5)
	"outbounds":    true, // только собственный outbound профиля — по тегу
	"log":          true, // только log.level
	"experimental": true, // только experimental.cache_file (без path, см. cacheFileKeys)
}

// cacheFileKeys — разрешённые поля experimental.cache_file в custom_config.
// path нет намеренно (T-08, A1-01): путь кэша задаёт LAVRay
// (BuildSettings.CacheFilePath). Иначе sing-box открывал бы любой файл
// пользователя, а при несовпадении формата УДАЛЯЛ его и создавал bbolt на его
// месте. Сравнение точное: "Path" и "path+" тоже не проходят.
var cacheFileKeys = map[string]bool{
	"enabled":      true,
	"cache_id":     true,
	"store_fakeip": true,
	"store_rdrc":   true,
	"store_dns":    true,
	"rdrc_timeout": true,
}

// mergeKeyBase — ключ без маркеров конкатенации ("+key"/"key+" → "key").
func mergeKeyBase(key string) string {
	return strings.TrimSuffix(strings.TrimPrefix(key, "+"), "+")
}

// MergeCustomConfig валидирует пользовательский custom_config по allowlist
// и мержит его в готовый конфиг. allowedOutboundTags — теги outbound'ов,
// созданных билдером для этого профиля; чужие теги в outbounds отклоняются.
// Пустой/пустой-объект srcJSON — no-op.
func MergeCustomConfig(cfg map[string]any, srcJSON string, allowedOutboundTags []string) error {
	srcJSON = strings.TrimSpace(srcJSON)
	if srcJSON == "" || srcJSON == "{}" {
		return nil
	}
	var src map[string]any
	if err := json.Unmarshal([]byte(srcJSON), &src); err != nil {
		return invalidArgument("custom_config", "invalid JSON: %v", err)
	}

	allowedTags := make(map[string]bool, len(allowedOutboundTags))
	for _, t := range allowedOutboundTags {
		allowedTags[t] = true
	}

	// Отсортированный обход: при нескольких нарушениях ошибка детерминирована.
	rootKeys := make([]string, 0, len(src))
	for key := range src {
		rootKeys = append(rootKeys, key)
	}
	sort.Strings(rootKeys)
	for _, key := range rootKeys {
		val := src[key]
		base := mergeKeyBase(key)
		// C4-7: конкатенационные маркеры имеют смысл только на массивах:
		// "+log":{...} / "route+":5 — ошибка; {"route":{"+rules":[...]}}
		// (маркер внутри объекта) — валидный путь MergeJSON.
		if key != base {
			if _, isArr := val.([]any); !isArr {
				return invalidArgument(base, "concat marker %q requires an array value", key)
			}
		}
		if !allowedRootKeys[base] {
			// inbounds, log.output (корень), прочие неизвестные корневые ключи.
			return invalidArgument(base, "key %q is not allowed in custom_config (allowlist: route, dns, outbounds, log.level, experimental.cache_file)", base)
		}
		// T-08: в разрешённых секциях, где допустимы произвольные поддеревья,
		// ни один ключ не может задавать путь к файлу (filekeys.go). log и
		// experimental проверяются строже — по точному списку полей.
		switch base {
		case "route", "dns", "outbounds":
			if err := rejectFileKeys("", map[string]any{base: val}); err != nil {
				return err
			}
		}
		switch base {
		case "route", "dns":
			// Полные секции — обычный MergeJSON (с семантикой "+key"/"key+"
			// внутри, например {"route":{"+rules":[...]}}).
			// C4-7: секция обязана быть объектом, иначе невалидный конфиг
			// молча ({"route":5}).
			if _, ok := val.(map[string]any); !ok {
				return invalidArgument(base, "%s override must be an object", base)
			}
			MergeJSON(cfg, map[string]any{key: val})
		case "log":
			if err := mergeLogKey(cfg, key, val); err != nil {
				return err
			}
		case "experimental":
			if err := mergeExperimentalKey(cfg, key, val); err != nil {
				return err
			}
		case "outbounds":
			if err := mergeOutboundsKey(cfg, key, val, allowedTags); err != nil {
				return err
			}
		}
	}
	return nil
}

// mergeLogKey — из секции log разрешён только ключ level (log.output и
// прочие подсекции — INVALID_ARGUMENT, LNB-25).
func mergeLogKey(cfg map[string]any, key string, val any) error {
	obj, ok := val.(map[string]any)
	if !ok {
		return invalidArgument("log", "log override must be an object")
	}
	for sub, _ := range obj {
		if mergeKeyBase(sub) != "level" {
			return invalidArgument("log."+mergeKeyBase(sub), "key %q is not allowed in custom_config (only log.level)", "log."+mergeKeyBase(sub))
		}
	}
	MergeJSON(cfg, map[string]any{key: val})
	return nil
}

// mergeExperimentalKey — из секции experimental разрешена только подсекция
// cache_file и только поля из cacheFileKeys; clash_api (вкл.
// external_ui_download_url), v2ray_api, debug, а также cache_file.path —
// INVALID_ARGUMENT (LNB-25, T-08).
func mergeExperimentalKey(cfg map[string]any, key string, val any) error {
	obj, ok := val.(map[string]any)
	if !ok {
		return invalidArgument("experimental", "experimental override must be an object")
	}
	subs := make([]string, 0, len(obj))
	for sub := range obj {
		subs = append(subs, sub)
	}
	sort.Strings(subs)
	for _, sub := range subs {
		if mergeKeyBase(sub) != "cache_file" {
			return invalidArgument(
				"experimental."+mergeKeyBase(sub),
				"key %q is not allowed in custom_config (only experimental.cache_file)",
				"experimental."+mergeKeyBase(sub),
			)
		}
		cacheFile, ok := obj[sub].(map[string]any)
		if !ok {
			return invalidArgument("experimental.cache_file", "experimental.cache_file override must be an object")
		}
		fields := make([]string, 0, len(cacheFile))
		for field := range cacheFile {
			fields = append(fields, field)
		}
		sort.Strings(fields)
		for _, field := range fields {
			if !cacheFileKeys[field] {
				return invalidArgument(
					"experimental.cache_file."+mergeKeyBase(field),
					"key %q is not allowed in custom_config (cache_file accepts only enabled, cache_id, store_fakeip, store_rdrc, store_dns, rdrc_timeout; the cache file path is set by LAVRay)",
					"experimental.cache_file."+mergeKeyBase(field),
				)
			}
		}
	}
	MergeJSON(cfg, map[string]any{key: val})
	return nil
}

// cfgOutboundsAny нормализует cfg["outbounds"] в []any (C4-1: Build кладёт
// []map[string]any; после JSON-круга — []any; мерж обязан работать с обоими).
func cfgOutboundsAny(cfg map[string]any) []any {
	switch arr := cfg["outbounds"].(type) {
	case []any:
		return arr
	case []map[string]any:
		out := make([]any, len(arr))
		for i, m := range arr {
			out[i] = m
		}
		return out
	}
	return nil
}

// mergeOutboundsKey — outbounds в custom_config: только объекты с тегом
// собственного outbound профиля (§3.1.1); мерж по тегу в существующий
// outbound, добавление новых запрещено.
func mergeOutboundsKey(cfg map[string]any, key string, val any, allowedTags map[string]bool) error {
	arr, ok := val.([]any)
	if !ok {
		return invalidArgument("outbounds", "outbounds override must be an array")
	}
	// C4-1 (ревью): Build кладёт []map[string]any, JSON-круг — []any.
	cfgArr := cfgOutboundsAny(cfg)

	for i, el := range arr {
		obj, ok := el.(map[string]any)
		if !ok {
			return invalidArgument(fmt.Sprintf("outbounds[%d]", i), "outbound override must be an object")
		}
		tag, _ := obj["tag"].(string)
		if tag == "" {
			return invalidArgument(fmt.Sprintf("outbounds[%d]", i), "outbound override must have a tag")
		}
		if !allowedTags[tag] {
			return invalidArgument("outbounds", "tag %q is not an outbound of this profile", tag)
		}
		var dstObj map[string]any
		for _, d := range cfgArr {
			if d, ok := d.(map[string]any); ok && d["tag"] == tag {
				dstObj = d
				break
			}
		}
		if dstObj == nil {
			return invalidArgument("outbounds", "tag %q is not an outbound of this profile", tag)
		}
		MergeJSON(dstObj, obj)
	}
	return nil
}
