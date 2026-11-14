// SPDX-License-Identifier: GPL-3.0-only

package nekoimport

// limits.go — пределы разбора чужого каталога и безопасное чтение файлов.
// Каталог NekoBox выбирает пользователь, но его содержимому не доверяем: файлов
// не больше 20 000, файл до 2 МиБ, всего до 64 МиБ. Файлы открываются только
// после Lstat: символическая ссылка не читается (иначе ссылка из каталога
// указала бы на любой файл диска). Из имён используются только базовые имена
// из os.ReadDir, поэтому выхода из каталога через «..» нет.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Limits — пределы одного разбора.
type Limits struct {
	MaxFiles      int   // записей в каталогах groups, profiles, routes_box, routes
	MaxFileBytes  int64 // размер одного файла
	MaxTotalBytes int64 // размер всех прочитанных файлов
}

// DefaultLimits — боевые пределы.
var DefaultLimits = Limits{MaxFiles: 20000, MaxFileBytes: 2 << 20, MaxTotalBytes: 64 << 20}

// ErrLimits — превышено число файлов или общий размер (RESOURCE_EXHAUSTED).
var ErrLimits = errors.New("превышены пределы разбора каталога NekoBox")

// Причины пропуска отдельного файла (не фатальны: остальные переносятся).
var (
	errNotRegular = errors.New("not a regular file (symlink and special files are not followed)")
	errTooBig     = errors.New("file is larger than the per-file limit")
)

// reader читает файлы каталога config под общим бюджетом.
type reader struct {
	root  string
	lim   Limits
	files int
	total int64
}

func newReader(root string, lim Limits) *reader { return &reader{root: root, lim: lim} }

// hasDir — подкаталог sub есть и это настоящий каталог (не ссылка).
func (r *reader) hasDir(sub string) bool {
	fi, err := os.Lstat(filepath.Join(r.root, sub))
	return err == nil && fi.IsDir()
}

// readChunk — сколько записей каталога читается за раз. Каталог не загружается
// целиком: чтение прекращается, как только превышен предел числа файлов.
var readChunk = 1024

// names — имена записей подкаталога sub по возрастанию; каждая запись входит в
// бюджет числа файлов. Нет подкаталога или он ссылка — пусто. Каталог читается
// порциями по readChunk записей и бросается на пределе, поэтому каталог с
// миллионами записей не съедает память.
func (r *reader) names(sub string) ([]string, error) {
	if !r.hasDir(sub) {
		return nil, nil
	}
	dir, err := os.Open(filepath.Join(r.root, sub))
	if err != nil {
		return nil, nil // нечитаемый каталог = пустой: ошибка попадёт в отчёт отсутствием данных
	}
	defer func() { _ = dir.Close() }()
	var out []string
	for {
		entries, err := dir.ReadDir(readChunk)
		r.files += len(entries)
		if r.files > r.lim.MaxFiles {
			return nil, fmt.Errorf("%w: больше %d файлов", ErrLimits, r.lim.MaxFiles)
		}
		for _, e := range entries {
			out = append(out, e.Name())
		}
		if err != nil { // io.EOF — конец каталога; прочая ошибка — читаем что успели
			break
		}
	}
	sort.Strings(out)
	return out, nil
}

// readFile читает файл sub/name. Не обычный файл → errNotRegular, больше
// MaxFileBytes → errTooBig (оба — причина пропуска); общий бюджет → ErrLimits.
func (r *reader) readFile(sub, name string) ([]byte, error) {
	path := filepath.Join(r.root, sub, name)
	fi, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, errNotRegular
	}
	if fi.Size() > r.lim.MaxFileBytes {
		return nil, errTooBig
	}
	r.total += fi.Size()
	if r.total > r.lim.MaxTotalBytes {
		return nil, fmt.Errorf("%w: больше %d байт", ErrLimits, r.lim.MaxTotalBytes)
	}
	return os.ReadFile(path)
}
