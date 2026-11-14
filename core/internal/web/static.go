// SPDX-License-Identifier: GPL-3.0-only

//go:build with_webui

package web

import (
	"embed"
	"io/fs"
)

// Каталог dist заполняет сборка (scripts/build-webui.sh копирует туда ui/dist).
// В репозитории лежит только .gitkeep: без него go:embed не соберётся.
//
//go:embed all:dist
var embeddedDist embed.FS

// embeddedFS возвращает встроенный ui/dist; ok=false — каталог не заполнен
// (нет index.html).
func embeddedFS() (fs.FS, bool) {
	sub, err := fs.Sub(embeddedDist, "dist")
	if err != nil {
		return nil, false
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return nil, false
	}
	return sub, true
}
