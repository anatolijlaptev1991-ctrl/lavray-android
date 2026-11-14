// SPDX-License-Identifier: GPL-3.0-only

//go:build !with_webui

package web

import "io/fs"

// embeddedFS в сборке без тега with_webui ничего не встраивает.
func embeddedFS() (fs.FS, bool) { return nil, false }
