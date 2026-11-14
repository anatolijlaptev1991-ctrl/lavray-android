// SPDX-License-Identifier: GPL-3.0-only

package presets

// codec.go — файл набора для обмена (T-80): экспорт в формат presets/SCHEMA.md
// и разбор при импорте с автоопределением. Принимаются два формата: набор
// LAVRay (есть объект routing) и маршрут NekoRay 4.x (RoutingBean без routing).

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// nekoKeys — поля RoutingBean: по ним узнаётся маршрут NekoRay.
var nekoKeys = []string{
	"direct_ip", "direct_domain", "proxy_ip", "proxy_domain", "block_ip", "block_domain",
	"def_outbound", "custom", "remote_dns", "direct_dns", "dns_routing", "use_dns_object",
	"sniffing_mode", "outbound_domain_strategy", "domain_strategy",
}

// ExportJSON — файл набора: JSON с отступом в 2 пробела и переводом строки в
// конце. Пустые списки пишутся как [], не null. Сам набор не меняется.
func ExportJSON(p *Preset) ([]byte, error) {
	c := clonePreset(p)
	c.fillEmptyLists()
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("экспорт набора %q: %w", p.ID, err)
	}
	return append(b, '\n'), nil
}

// DetectAndParse разбирает файл импорта и определяет формат. Для маршрута
// NekoRay name — имя набора (пусто → «NekoRay»); возвращается и список
// предупреждений конвертера. Проверку по схеме и лимитам делает PrepareUser:
// здесь только чтение, id и признаки файла не переписываются.
// Ошибки: ErrTooLarge (> MaxImportBytes), ErrFormat (не наш формат).
func DetectAndParse(data []byte, name string) (*Preset, []string, error) {
	if len(data) > MaxImportBytes {
		return nil, nil, ErrTooLarge
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(bytes.TrimSpace(data), &top); err != nil || top == nil {
		return nil, nil, fmt.Errorf("%w: ожидался JSON-объект", ErrFormat)
	}
	if raw, ok := top["routing"]; ok {
		return parseLavray(data, raw)
	}
	for _, k := range nekoKeys {
		if _, ok := top[k]; ok {
			return ParseNekoRoute(data, name)
		}
	}
	return nil, nil, fmt.Errorf("%w: нет ни routing, ни полей маршрута NekoRay", ErrFormat)
}

func parseLavray(data, routing []byte) (*Preset, []string, error) {
	if t := bytes.TrimSpace(routing); len(t) == 0 || t[0] != '{' {
		return nil, nil, fmt.Errorf("%w: routing должен быть объектом", ErrFormat)
	}
	var p Preset
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrFormat, err)
	}
	// Вставка выставит свою версию схемы, поэтому версию файла проверяем здесь.
	if p.Version <= 0 || p.Version > SchemaVersion {
		return nil, nil, fmt.Errorf("%w: version: схема %d не поддерживается (поддерживается ≤ %d)", ErrFormat, p.Version, SchemaVersion)
	}
	return &p, nil, nil
}

// ParseNekoRoute читает маршрут NekoRay 4.x (RoutingBean) и конвертирует его в
// пользовательский набор; name — его имя (пусто → «NekoRay»). Предупреждения —
// что конвертер отбросил или заменил. Ошибки: ErrTooLarge, ErrFormat.
func ParseNekoRoute(data []byte, name string) (*Preset, []string, error) {
	if len(data) > MaxImportBytes {
		return nil, nil, ErrTooLarge
	}
	if t := bytes.TrimSpace(data); len(t) == 0 || t[0] != '{' {
		return nil, nil, fmt.Errorf("%w: маршрут NekoRay — ожидался JSON-объект", ErrFormat)
	}
	var src NekoRayRoute
	if err := json.Unmarshal(data, &src); err != nil {
		return nil, nil, fmt.Errorf("%w: маршрут NekoRay: %v", ErrFormat, err)
	}
	if name == "" {
		name = "NekoRay"
	}
	meta := PresetMeta{Schema: SchemaRef, Version: SchemaVersion, Revision: 1,
		ID: CustomIDPrefix + "import", Name: Localized{"en": name}}
	return ConvertNekoRayRoute(&src, ConvertOptions{Meta: meta, DefaultUnderlyingDNS: "local"})
}
