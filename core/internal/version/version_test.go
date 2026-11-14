// SPDX-License-Identifier: GPL-3.0-only

package version

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// TestDefaultMatchesAppVersion: версия ядра — та же, что у приложения Android (versionName
// в app/build.gradle.kts), без суффиксов этапов. Тест лежит в core/internal/version; корень
// репозитория — тремя уровнями выше. Ядро, собранное вне репозитория, проверку пропускает.
func TestDefaultMatchesAppVersion(t *testing.T) {
	if !regexp.MustCompile(`^\d+\.\d+\.\d+$`).MatchString(Version) {
		t.Fatalf("Version = %q: ожидается X.Y.Z без суффиксов этапов (w3, w6)", Version)
	}
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "app", "build.gradle.kts"))
	if os.IsNotExist(err) {
		t.Skip("app/build.gradle.kts не найден: ядро собрано вне репозитория")
	}
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^\s*versionName\s*=\s*"([^"]+)"`).FindSubmatch(raw)
	if m == nil {
		t.Fatal("в app/build.gradle.kts не найдена строка versionName")
	}
	if string(m[1]) != Version {
		t.Errorf("app/build.gradle.kts: versionName = %q, а в core/internal/version = %q; обновите оба", m[1], Version)
	}
}
