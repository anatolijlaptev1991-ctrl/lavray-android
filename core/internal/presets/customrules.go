// SPDX-License-Identifier: GPL-3.0-only

package presets

// customrules.go — разбор custom-правил набора (FR-UI-137). В proto это
// JSON-массив правил sing-box; старые данные (конвертер NekoRay, встроенные
// наборы) хранят объект {"rules":[…]}. Принимаются оба вида, хранится массив.
// Тот же разбор использует cfgbuild: чтение и запись не расходятся.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

// MaxCustomRulesBytes — предел размера custom_rules_json (256 КиБ).
const MaxCustomRulesBytes = 256 << 10

// splitCustomRules делит запись на элементы-объекты. Пустые формы дают nil.
func splitCustomRules(raw string) ([]json.RawMessage, error) {
	raw = string(bytes.TrimSpace([]byte(raw)))
	if len(raw) > MaxCustomRulesBytes {
		return nil, fmt.Errorf("custom_rules: %d байт, предел %d", len(raw), MaxCustomRulesBytes)
	}
	if raw == "" || raw == "null" {
		return nil, nil
	}
	var items []json.RawMessage
	switch raw[0] {
	case '[':
		if err := json.Unmarshal([]byte(raw), &items); err != nil {
			return nil, fmt.Errorf("custom_rules: %w", err)
		}
	case '{':
		var obj map[string]json.RawMessage
		if err := json.Unmarshal([]byte(raw), &obj); err != nil {
			return nil, fmt.Errorf("custom_rules: %w", err)
		}
		if len(obj) == 0 {
			return nil, nil
		}
		inner, ok := obj["rules"]
		if !ok {
			return nil, errors.New(`custom_rules: в объекте нет поля "rules" (ожидался массив правил sing-box или {"rules":[…]})`)
		}
		if err := json.Unmarshal(inner, &items); err != nil {
			return nil, fmt.Errorf(`custom_rules: "rules" должно быть массивом: %w`, err)
		}
	default:
		return nil, errors.New(`custom_rules: ожидался массив правил sing-box или {"rules":[…]}`)
	}
	for i, it := range items {
		if t := bytes.TrimSpace(it); len(t) == 0 || t[0] != '{' {
			return nil, fmt.Errorf("custom_rules[%d]: правило должно быть объектом", i)
		}
	}
	return items, nil
}

// NormalizeCustomRules проверяет запись и приводит её к хранимому виду: компактный
// JSON-массив объектов; пустое правило-набор — "". Ошибка называет путь.
func NormalizeCustomRules(raw string) (string, error) {
	items, err := splitCustomRules(raw)
	if err != nil || len(items) == 0 {
		return "", err
	}
	// json.Compact, а не json.Marshal: Marshal экранирует <, > и & (< — 6 байт
	// вместо 1), и сохранённое значение перерастало бы предел, который проверяет
	// сборка конфига.
	var buf bytes.Buffer
	buf.WriteByte('[')
	for i, it := range items {
		if i > 0 {
			buf.WriteByte(',')
		}
		if err := json.Compact(&buf, it); err != nil {
			return "", fmt.Errorf("custom_rules[%d]: %w", i, err)
		}
	}
	buf.WriteByte(']')
	if buf.Len() > MaxCustomRulesBytes {
		return "", fmt.Errorf("custom_rules: %d байт после нормализации, предел %d", buf.Len(), MaxCustomRulesBytes)
	}
	return buf.String(), nil
}

// ParseCustomRules разбирает запись в список правил для сборки конфига
// (каждое — map[string]any). Пустая запись — nil.
func ParseCustomRules(raw string) ([]any, error) {
	items, err := splitCustomRules(raw)
	if err != nil || len(items) == 0 {
		return nil, err
	}
	out := make([]any, 0, len(items))
	for i, it := range items {
		var rule map[string]any
		if err := json.Unmarshal(it, &rule); err != nil {
			return nil, fmt.Errorf("custom_rules[%d]: %w", i, err)
		}
		out = append(out, rule)
	}
	return out, nil
}
