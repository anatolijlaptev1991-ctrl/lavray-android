// SPDX-License-Identifier: GPL-3.0-only

// staging.go — защищённый каталог проверенных установщиков
// (<данные>/updates/staging). Имена файлов — «<sha256>-<имя>» без разделителей;
// путь, пришедший снаружи, принимается только если это прямой потомок каталога
// (после канонизации, не симлинк).
package updates

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// Суффиксы служебных файлов рядом с установщиком.
const (
	PartSuffix    = ".part"
	MinisigSuffix = ".minisig"
)

var (
	sha256Hex  = regexp.MustCompile(`^[0-9a-f]{64}$`)
	stagedName = regexp.MustCompile(`^[0-9a-f]{64}-[A-Za-z0-9][A-Za-z0-9._-]*$`)
)

// StagedName — «<sha256>-<имя>»; имя обязано быть простым (без разделителей,
// «..», недопустимых символов).
func StagedName(sha, name string) (string, error) {
	if !sha256Hex.MatchString(sha) {
		return "", fmt.Errorf("%w: sha256 «%s» не 64 hex-символа нижнего регистра", ErrUnsafePath, sha)
	}
	out := sha + "-" + name
	if strings.Contains(name, "..") || !stagedName.MatchString(out) {
		return "", fmt.Errorf("%w: имя файла %q", ErrUnsafePath, name)
	}
	return out, nil
}

// StagedPath — путь установщика внутри каталога staging.
func StagedPath(dir, sha, name string) (string, error) {
	n, err := StagedName(sha, name)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, n), nil
}

// InsideStaging проверяет путь, пришедший снаружи (от оболочки или UI): это
// прямой потомок каталога staging с корректным именем, обычный файл (не
// симлинк/junction). Возвращает канонический путь. Допустимые суффиксы —
// «.minisig» (подпись рядом с установщиком).
func InsideStaging(dir, p string) (string, error) {
	if p == "" || dir == "" {
		return "", fmt.Errorf("%w: пустой путь", ErrUnsafePath)
	}
	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", fmt.Errorf("%w: каталог staging: %v", ErrUnsafePath, err)
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrUnsafePath, err)
	}
	if parent := filepath.Dir(abs); !samePath(parent, filepath.Clean(dir)) && !samePath(parent, realDir) {
		return "", fmt.Errorf("%w: %q вне каталога staging", ErrUnsafePath, p)
	}
	if !stagedName.MatchString(strings.TrimSuffix(filepath.Base(abs), MinisigSuffix)) {
		return "", fmt.Errorf("%w: имя %q не вида <sha256>-<имя>", ErrUnsafePath, filepath.Base(abs))
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%w: %q — не обычный файл (ссылка или каталог)", ErrUnsafePath, p)
	}
	return abs, nil
}

// CleanStaging удаляет из staging всё, кроме keep (полный путь), старше olderThan;
// возвращает число удалённых файлов. Каталоги и чужие имена не трогаются.
func CleanStaging(dir, keep string, olderThan time.Duration, now time.Time) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	removed := 0
	for _, e := range entries {
		full := filepath.Join(dir, e.Name())
		base := strings.TrimSuffix(strings.TrimSuffix(e.Name(), PartSuffix), MinisigSuffix)
		if e.IsDir() || !stagedName.MatchString(base) || samePath(full, keep) || samePath(full, keep+MinisigSuffix) {
			continue
		}
		info, ierr := e.Info()
		if ierr != nil || now.Sub(info.ModTime()) < olderThan {
			continue
		}
		if os.Remove(full) == nil {
			removed++
		}
	}
	return removed, nil
}

// samePath сравнивает пути: на Windows без учёта регистра, на остальных ОС точно.
func samePath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if b == "" || b == "." {
		return false
	}
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}
