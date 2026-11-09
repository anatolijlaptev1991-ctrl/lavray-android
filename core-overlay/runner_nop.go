// SPDX-License-Identifier: GPL-3.0-only

//go:build !singbox_run

package mobile

import "github.com/anatolijlaptev1991-ctrl/lavray/core/internal/inst"

// newRunner без тега singbox_run: заглушка (проверка компиляции и тесты патча).
func newRunner(*host) inst.Runner { return inst.NopRunner{} }
