// SPDX-License-Identifier: GPL-3.0-only

package presets

import "embed"

// builtinFS — встроенные наборы (builtin/*.json). В LAVRay Android это единственная
// их копия: формат описан в SCHEMA.md рядом.
//
//go:embed builtin
var builtinFS embed.FS
