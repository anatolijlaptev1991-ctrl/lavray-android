// SPDX-License-Identifier: GPL-3.0-only

package nekoimport

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"strings"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/presets"
)

// routes.go — маршруты NekoBox: config/routes_box/<имя> (и routes/ старых
// форков) → набор LAVRay через presets.ParseNekoRoute (ConvertNekoRayRoute).
// Id набора выдаёт api.PlanInsert: имя «Neko: <имя>» даёт `custom:neko-<slug>`.

// routeDirs — каталоги маршрутов; при совпадении имён побеждает первый.
var routeDirs = []string{"routes_box", "routes"}

// routeNamePrefix — приставка к имени набора и к его slug.
const routeNamePrefix = "Neko: "

// displayRouteName — имя набора в пределах presets.MaxNameRunes.
func displayRouteName(name string) string {
	r := []rune(routeNamePrefix + name)
	if len(r) > presets.MaxNameRunes {
		r = r[:presets.MaxNameRunes]
	}
	return string(r)
}

// scanRoutes разбирает маршруты; негодный файл попадает в Skipped, не фатально.
func (sc *scanner) scanRoutes() error {
	rep := &sc.plan.Report
	seen := map[string]bool{}
	for _, sub := range routeDirs {
		names, err := sc.rd.names(sub)
		if err != nil {
			return err
		}
		for _, file := range names {
			name := strings.TrimSuffix(file, ".json")
			rel := filepath.Join(sub, file)
			if name == "" || seen[name] {
				continue
			}
			b, err := sc.rd.readFile(sub, file)
			if errors.Is(err, ErrLimits) {
				return err
			}
			if err != nil {
				rep.skip(rel, 0, "route", err.Error())
				continue
			}
			p, warns, err := presets.ParseNekoRoute(b, displayRouteName(name))
			if err != nil {
				rep.skip(rel, 0, "route", err.Error())
				continue
			}
			seen[name] = true
			sum := sha256.Sum256(b)
			sc.plan.Routes = append(sc.plan.Routes, PlanRoute{
				Name: name, File: rel, Preset: p, Warnings: warns,
				Key: SourceKey(0, KindPreset, name+"#"+hex.EncodeToString(sum[:8])),
			})
		}
	}
	return nil
}
