// SPDX-License-Identifier: GPL-3.0-only

// platform.go — ключ актива манифеста для платформы (вместо «первого ключа по
// алфавиту»).
package updates

import "runtime"

// AssetKeyFor — ключ актива манифеста для платформы: windows-x64,
// windows-arm64, linux-x64, linux-arm64, macos-universal. Неизвестная
// платформа — пустая строка (актив не выбирается «первым по алфавиту»).
func AssetKeyFor(goos, goarch string) string {
	arch := map[string]string{"amd64": "x64", "arm64": "arm64"}[goarch]
	switch goos {
	case "windows", "linux":
		if arch != "" {
			return goos + "-" + arch
		}
	case "darwin":
		return "macos-universal"
	}
	return ""
}

// CurrentAssetKey — ключ актива для платформы, на которой работает ядро.
func CurrentAssetKey() string { return AssetKeyFor(runtime.GOOS, runtime.GOARCH) }
