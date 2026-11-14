// SPDX-License-Identifier: GPL-3.0-only

package subs

import (
	"testing"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/version"
)

// D-06: User-Agent подписок несёт единую версию, а не плейсхолдер пакета subs.
func TestDefaultUserAgentUsesSharedVersion(t *testing.T) {
	if want := "LAVRay/" + version.Version; DefaultUserAgent != want {
		t.Fatalf("DefaultUserAgent = %q, ожидалось %q", DefaultUserAgent, want)
	}
}
