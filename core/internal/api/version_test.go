// SPDX-License-Identifier: GPL-3.0-only

package api

import (
	"testing"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/version"
)

// D-06: «Версия ядра» в UI, текущая версия приложения в CheckUpdates и
// самопроверка берут версию из одного места — version.Version (её подменяет
// -ldflags -X), а не из собственных плейсхолдеров пакетов.
func TestVersionsComeFromSingleSource(t *testing.T) {
	s, _ := subAutoTestServer(t)
	if got := s.coreStatus().CoreVersion; got != version.Version {
		t.Errorf("CoreStatus.core_version = %q, ожидалось version.Version = %q", got, version.Version)
	}
	if s.upd.appVersion() != version.Version {
		t.Errorf("appVersion = %q, ожидалось version.Version = %q", s.upd.appVersion(), version.Version)
	}
}
