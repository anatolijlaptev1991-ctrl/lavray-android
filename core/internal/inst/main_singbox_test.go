// SPDX-License-Identifier: GPL-3.0-only

//go:build singbox_run

package inst

import (
	"fmt"
	"os"
	"testing"
)

// TestMain переводит тесты с настоящим sing-box в пустой временный каталог.
//
// Зачем. С PlatformLogWriter (T-79) форк создаёт cache_file даже в конфиге без
// experimental.cache_file, а путь по умолчанию — «cache.db» в рабочем каталоге.
// Рабочие конфиги ядра задают путь в каталоге данных явно, а вот руками
// написанные конфиги тестов оставили бы cache.db в каталоге пакета.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "lavray-inst-cwd-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, "TestMain:", err)
		os.Exit(1)
	}
	if err := os.Chdir(dir); err != nil {
		fmt.Fprintln(os.Stderr, "TestMain:", err)
		os.Exit(1)
	}
	code := m.Run()
	_ = os.Chdir(os.TempDir())
	_ = os.RemoveAll(dir) // не критично: файл может ещё держать антивирус
	os.Exit(code)
}
