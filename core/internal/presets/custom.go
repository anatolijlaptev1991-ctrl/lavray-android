// SPDX-License-Identifier: GPL-3.0-only

package presets

// custom.go — пользовательская копия набора (T-80): «Сохранить как»,
// «Дублировать», правка встроенного через копию.

// SchemaRef — значение `$schema` в файлах наборов (как у встроенных).
const SchemaRef = "preset.schema.v1.json"

// NewCustomFrom возвращает глубокую копию src под новым id: не builtin,
// ревизия 1, версия схемы текущая. Источник: у встроенного набора — он сам
// (id и ревизия); у пользовательского — его собственный источник (копия копии
// остаётся сбрасываемой); у набора «с нуля» источника нет. src не меняется.
func NewCustomFrom(src *Preset, id string) *Preset {
	c := clonePreset(src)
	c.Schema = SchemaRef
	c.Version = SchemaVersion
	c.Revision = 1
	c.ID = id
	c.Builtin = false
	if src.Builtin {
		c.SourceID, c.SourceRevision = src.ID, src.Revision
	}
	return c
}

// Clone — глубокая копия набора без изменения полей.
func (p *Preset) Clone() *Preset { return clonePreset(p) }

// clonePreset — глубокая копия без изменения полей.
func clonePreset(src *Preset) *Preset {
	c := *src
	c.Name = cloneLocalized(src.Name)
	c.Description = cloneLocalized(src.Description)
	c.Routing = cloneRouting(src.Routing)
	return &c
}

func cloneLocalized(l Localized) Localized {
	out := make(Localized, len(l))
	for k, v := range l {
		out[k] = v
	}
	return out
}

func cloneStrings(s []string) []string {
	return append([]string{}, s...)
}

func cloneRouting(r Routing) Routing {
	out := r
	out.ProxyDomain = cloneStrings(r.ProxyDomain)
	out.DirectDomain = cloneStrings(r.DirectDomain)
	out.BlockDomain = cloneStrings(r.BlockDomain)
	out.ProxyIP = cloneStrings(r.ProxyIP)
	out.DirectIP = cloneStrings(r.DirectIP)
	out.BlockIP = cloneStrings(r.BlockIP)
	out.ProcessRules = make([]ProcessRule, len(r.ProcessRules))
	for i, pr := range r.ProcessRules {
		out.ProcessRules[i] = ProcessRule{
			Platforms:    append([]string(nil), pr.Platforms...),
			Outbound:     pr.Outbound,
			ProcessNames: cloneStrings(pr.ProcessNames),
		}
	}
	return out
}
