// SPDX-License-Identifier: GPL-3.0-only

package mobile

// tz.go — часовой пояс на Android. time.Local там всегда UTC (initLocal в
// time/zoneinfo_android.go: «TODO getprop persist.sys.timezone»), поэтому
// пояс передаёт Kotlin именем IANA, а база зон читается из упакованного файла
// tzdata Android. Формат разобран по time/zoneinfo_android.go (лицензия Go, BSD).

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

const (
	tzHeaderSize = 12 + 3*4
	tzNameSize   = 40
	tzEntrySize  = tzNameSize + 3*4
)

// tzdataSources — где Android держит упакованный tzdata, по убыванию свежести
// (обновление через Mainline, APEX, образ системы).
func tzdataSources() []string {
	var out []string
	add := func(root, rel string) {
		if root != "" {
			out = append(out, filepath.Join(root, rel))
		}
	}
	add(os.Getenv("ANDROID_DATA"), "misc/zoneinfo/current/tzdata")
	add(os.Getenv("ANDROID_TZDATA_ROOT"), "etc/tz/tzdata")
	add(os.Getenv("ANDROID_ROOT"), "usr/share/zoneinfo/tzdata")
	return append(out,
		"/data/misc/zoneinfo/current/tzdata",
		"/apex/com.android.tzdata/etc/tz/tzdata",
		"/system/usr/share/zoneinfo/tzdata",
	)
}

// loadAndroidZone ищет зону name в упакованных файлах tzdata.
func loadAndroidZone(name string, sources []string) (*time.Location, error) {
	var lastErr error = errors.New("нет источников tzdata")
	for _, src := range sources {
		data, err := readPackedZone(src, name)
		if err != nil {
			lastErr = err
			continue
		}
		loc, err := time.LoadLocationFromTZData(name, data)
		if err != nil {
			lastErr = fmt.Errorf("%s: %w", src, err)
			continue
		}
		return loc, nil
	}
	return nil, lastErr
}

// readPackedZone достаёт TZif зоны name из файла tzdata (индекс читается
// по частям: файл занимает сотни килобайт).
func readPackedZone(path, name string) ([]byte, error) {
	if len(name) > tzNameSize {
		return nil, fmt.Errorf("имя зоны длиннее %d байт", tzNameSize)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	head := make([]byte, tzHeaderSize)
	if _, err := io.ReadFull(f, head); err != nil || string(head[:6]) != "tzdata" {
		return nil, fmt.Errorf("%s: не файл tzdata", path)
	}
	indexOff := binary.BigEndian.Uint32(head[12:])
	dataOff := binary.BigEndian.Uint32(head[16:])
	finalOff := binary.BigEndian.Uint32(head[20:])
	if indexOff > dataOff || dataOff > finalOff {
		return nil, fmt.Errorf("%s: повреждённый заголовок", path)
	}
	index := make([]byte, dataOff-indexOff)
	if _, err := f.ReadAt(index, int64(indexOff)); err != nil {
		return nil, fmt.Errorf("%s: индекс: %w", path, err)
	}
	for i := 0; i+tzEntrySize <= len(index); i += tzEntrySize {
		entry := index[i : i+tzEntrySize]
		if cString(entry[:tzNameSize]) != name {
			continue
		}
		start := binary.BigEndian.Uint32(entry[tzNameSize:])
		size := binary.BigEndian.Uint32(entry[tzNameSize+4:])
		data := make([]byte, size)
		if _, err := f.ReadAt(data, int64(dataOff)+int64(start)); err != nil {
			return nil, fmt.Errorf("%s: зона %s: %w", path, name, err)
		}
		return data, nil
	}
	return nil, fmt.Errorf("%s: зоны %s нет", path, name)
}

func cString(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}

// resolveZone — зона по имени: сначала tzdata Android, затем стандартные
// источники Go. Пустое имя даёт nil без ошибки.
func resolveZone(name string, sources []string) (*time.Location, error) {
	if name == "" {
		return nil, nil
	}
	loc, err := loadAndroidZone(name, sources)
	if err == nil {
		return loc, nil
	}
	if loc, goErr := time.LoadLocation(name); goErr == nil {
		return loc, nil
	}
	return nil, fmt.Errorf("часовой пояс %q: %w", name, err)
}

// applyTimeZone заменяет time.Local. Ошибка не фатальна для запуска: ядро
// остаётся на UTC, вызывающий пишет предупреждение.
func applyTimeZone(name string) error {
	loc, err := resolveZone(name, tzdataSources())
	if err != nil {
		return err
	}
	if loc != nil {
		time.Local = loc
	}
	return nil
}
