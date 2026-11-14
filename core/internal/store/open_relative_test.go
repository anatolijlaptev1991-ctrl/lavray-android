// SPDX-License-Identifier: GPL-3.0-only

package store

import (
	"os"
	"path/filepath"
	"testing"
)

// Относительный --data открывается от текущего каталога, а не от корня диска
// (раньше путь получал ведущий «/» и на Windows уходил в корень диска).
func TestOpenRelativeDir(t *testing.T) {
	base := t.TempDir()
	t.Chdir(base)
	s, err := Open(filepath.Join(".", "data"))
	if err != nil {
		t.Fatalf("Open(./data): %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if _, err := os.Stat(filepath.Join(base, "data", dbFileName)); err != nil {
		t.Fatalf("база не в <cwd>/data: %v", err)
	}
}
