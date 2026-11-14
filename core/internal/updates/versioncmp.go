// SPDX-License-Identifier: GPL-3.0-only

// versioncmp.go — сравнение версий приложения (semver без внешних зависимостей)
// и версий ядра по собственной схеме «1.14.1-lav.N».
package updates

import (
	"strconv"
	"strings"
)

// CompareVersions сравнивает версии приложения: «1.10.1» > «1.2.0»;
// пре-релиз младше релиза («0.5.1-rc.1» < «0.5.1»), идентификаторы пре-релиза
// сравниваются по semver (числовые — числом, прочие — лексически); префикс «v»
// и сборочные метаданные после «+» игнорируются. Нечисловые части ядра версии
// сравниваются лексикографически.
func CompareVersions(a, b string) int {
	coreA, preA := splitPre(a)
	coreB, preB := splitPre(b)
	if c := compareDotted(coreA, coreB); c != 0 {
		return c
	}
	switch {
	case preA == "" && preB == "":
		return 0
	case preA == "":
		return 1 // релиз старше пре-релиза
	case preB == "":
		return -1
	}
	return compareDotted(preA, preB)
}

// splitPre — «v0.5.1-rc.1+b7» → («0.5.1», «rc.1»).
func splitPre(v string) (string, string) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexByte(v, '+'); i >= 0 {
		v = v[:i]
	}
	if i := strings.IndexByte(v, '-'); i >= 0 {
		return v[:i], v[i+1:]
	}
	return v, ""
}

// compareDotted сравнивает точечные списки: числовые части — числом, остальные
// лексически; недостающие части в основной версии считаются нулём, а в
// пре-релизе более короткий список младше.
func compareDotted(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) || i < len(pb); i++ {
		sa, sb := "0", "0"
		if i < len(pa) {
			sa = pa[i]
		}
		if i < len(pb) {
			sb = pb[i]
		}
		if sa == "" {
			sa = "0"
		}
		if sb == "" {
			sb = "0"
		}
		if c := comparePart(sa, sb); c != 0 {
			return c
		}
	}
	return 0
}

func comparePart(sa, sb string) int {
	na, ea := strconv.ParseUint(sa, 10, 64)
	nb, eb := strconv.ParseUint(sb, 10, 64)
	switch {
	case ea == nil && eb == nil:
		if na < nb {
			return -1
		}
		if na > nb {
			return 1
		}
		return 0
	case ea == nil: // числовой идентификатор младше буквенного (semver)
		return -1
	case eb == nil:
		return 1
	}
	return strings.Compare(sa, sb)
}

// CompareCoreVersions — сравнение версий ядра по СОБСТВЕННОЙ схеме
// "1.14.1-lav.N" (§7: semver-сравнение с upstream запрещено; здесь
// сравниваются только lav-версии между собой: база числово, затем lav.N).
func CompareCoreVersions(a, b string) int {
	baseA, lavA := splitLav(a)
	baseB, lavB := splitLav(b)
	if c := compareDotted(baseA, baseB); c != 0 {
		return c
	}
	switch {
	case lavA < lavB:
		return -1
	case lavA > lavB:
		return 1
	}
	return 0
}

// splitLav — "1.14.1-lav.3" → ("1.14.1", 3); без суффикса → (s, 0).
func splitLav(s string) (string, int64) {
	i := strings.Index(s, "-lav.")
	if i < 0 {
		return s, 0
	}
	n, err := strconv.ParseInt(s[i+len("-lav."):], 10, 64)
	if err != nil {
		return s, 0
	}
	return s[:i], n
}
