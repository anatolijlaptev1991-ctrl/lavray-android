// SPDX-License-Identifier: GPL-3.0-only

// component_pending.go — что можно предложить пользователю в CheckUpdates и что
// показать в GetUpdateState для пресетов и гео-набора (T-75/7.5): доступные
// ревизии по подписанному манифесту и сведения о прошлой ревизии для отката.
package updates

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"sort"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/presets"
)

// PendingPresets — наборы манифеста, чья ревизия выше действующей. Берутся только
// id встроенных наборов; результат отсортирован по id.
func PendingPresets(mf *Manifest, installed []*presets.Preset) []PresetOffer {
	if mf == nil || len(mf.Presets) == 0 {
		return nil
	}
	var out []PresetOffer
	for _, p := range installed {
		if e, ok := mf.Presets[p.ID]; ok && e.Rev > int64(p.Revision) {
			out = append(out, PresetOffer{ID: p.ID, Installed: p.Revision, Rev: int(e.Rev)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// CheckSha256 сверяет sha256 данных с пином манифеста за постоянное время.
func CheckSha256(data []byte, want string) error {
	sum := sha256.Sum256(data)
	if subtle.ConstantTimeCompare([]byte(hex.EncodeToString(sum[:])), []byte(want)) != 1 {
		return fmt.Errorf("%w: sha256 не совпал с манифестом", ErrHashMismatch)
	}
	return nil
}

// PresetsPrevious — id → ревизия наборов в прошлой ревизии переопределений
// (пустая карта и ok=true — прошлая ревизия есть и это встроенные наборы).
func PresetsPrevious(dir string) (map[string]int, bool) {
	prev := PrevDir(dir)
	if !isDir(prev) {
		return nil, false
	}
	out := map[string]int{}
	list, err := presets.BuiltinPresetsWith(prev)
	if err != nil {
		return out, true
	}
	embedded, _ := presets.BuiltinPresets()
	base := map[string]int{}
	for _, p := range embedded {
		base[p.ID] = p.Revision
	}
	for _, p := range list {
		if p.Revision > base[p.ID] {
			out[p.ID] = p.Revision
		}
	}
	return out, true
}

// PendingGeo — гео-набор манифеста, если он новее установленного по ревизии и
// отличается по содержимому (нулевое installed — поставка из бинарника).
func PendingGeo(mf *Manifest, installed GeoInstalled) *GeoSection {
	if mf == nil || mf.Geo == nil || mf.Geo.Sha256 == installed.Sha256 || mf.Geo.Rev <= installed.Rev {
		return nil
	}
	g := *mf.Geo
	return &g
}

// GeoPrevious — прошлая ревизия гео-набора в dir.prev (ok=false — её нет).
func GeoPrevious(dir string) (GeoInstalled, bool) {
	prev := PrevDir(dir)
	if !isDir(prev) {
		return GeoInstalled{}, false
	}
	return InstalledGeo(prev), true
}
