// SPDX-License-Identifier: GPL-3.0-only

package mobile

// env.go — окружение процесса на Android: у Go там нет /tmp, домашнего
// каталога и /etc/localtime.

import (
	"errors"
	"fmt"
	"math"
	"os"
	"runtime/debug"
)

// prepareEnv создаёт каталоги и задаёт TMPDIR и HOME (os.TempDir на Android
// без TMPDIR вернул бы /data/local/tmp, недоступный приложению; HOME нужен
// os.UserCacheDir и os.UserHomeDir). chdir выполняется отдельно в Start.
func prepareEnv(o *Options) error {
	if o.DataDir == "" || o.TempDir == "" {
		return errors.New("mobile: DataDir и TempDir обязательны")
	}
	for _, dir := range []string{o.DataDir, o.TempDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("mobile: каталог %s: %w", dir, err)
		}
	}
	if err := os.Setenv("TMPDIR", o.TempDir); err != nil {
		return fmt.Errorf("mobile: TMPDIR: %w", err)
	}
	if err := os.Setenv("HOME", o.DataDir); err != nil {
		return fmt.Errorf("mobile: HOME: %w", err)
	}
	return nil
}

// applyMemoryLimit — мягкий предел памяти Go (debug.SetMemoryLimit): при его
// приближении сборщик мусора работает чаще, а фоновый scavenger возвращает
// страницы системе. 0 снимает предел. Замеры docs/MEASUREMENTS.md: предел 64 МБ и GOGC=50
// память :vpn не уменьшили (куча Go около 8 МБ из 69 МБ PSS) и чуть снизили скорость,
// поэтому приложение передаёт 0, а GOGC остаётся 100.
func applyMemoryLimit(mb int32) {
	if mb <= 0 {
		debug.SetMemoryLimit(math.MaxInt64)
		return
	}
	debug.SetMemoryLimit(int64(mb) << 20)
}
