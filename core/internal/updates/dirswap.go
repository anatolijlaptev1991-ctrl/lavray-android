// SPDX-License-Identifier: GPL-3.0-only

// dirswap.go — атомарная замена каталога данных с хранением прошлой ревизии
// (гео-набор, переопределения пресетов). Новая ревизия собирается во временном
// каталоге рядом с активным (тот же родитель, значит rename не пересекает
// тома), после чего: прошлая .prev откладывается, активный становится .prev,
// новый встаёт на его место. Любой сбой возвращает всё как было.
package updates

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const (
	prevSuffix    = ".prev"
	discardSuffix = ".discard"
	asideSuffix   = ".rbk"
	newInfix      = ".new-"
)

// renameDir — переименование каталога; тесты подменяют его, внедряя сбой.
var renameDir = os.Rename

// PrevDir — каталог прошлой ревизии рядом с активным cur.
func PrevDir(cur string) string { return cur + prevSuffix }

// NewSiblingDir создаёт пустой временный каталог рядом с cur (тот же родитель).
func NewSiblingDir(cur string) (string, error) {
	parent := filepath.Dir(cur)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return "", err
	}
	return os.MkdirTemp(parent, filepath.Base(cur)+newInfix+"*")
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

// SwapDir ставит собранный каталог next на место cur, прежний cur становится
// cur.prev (одна прошлая ревизия). При ошибке cur и cur.prev остаются как были,
// next не удаляется — это забота вызывающего. Ошибка — NewDirBusyError: наружу
// общий текст, в Detail — все сбои, включая неудавшийся возврат состояния.
func SwapDir(cur, next string) error {
	prev, discard := PrevDir(cur), cur+discardSuffix
	_ = os.RemoveAll(discard)
	hadPrev, hadCur := isDir(prev), isDir(cur)
	if hadPrev {
		if err := renameDir(prev, discard); err != nil {
			return NewDirBusyError(err)
		}
	}
	undo := func() error {
		if hadPrev {
			return renameDir(discard, prev)
		}
		return nil
	}
	if hadCur {
		if err := renameDir(cur, prev); err != nil {
			return NewDirBusyError(errors.Join(err, undo()))
		}
	}
	if err := renameDir(next, cur); err != nil {
		var back error
		if hadCur {
			back = renameDir(prev, cur)
		}
		return NewDirBusyError(errors.Join(err, back, undo()))
	}
	syncDir(filepath.Dir(cur))
	_ = os.RemoveAll(discard)
	return nil
}

// RollbackDir возвращает cur.prev на место cur; откатываемая ревизия удаляется,
// повторный откат невозможен (ErrNoPrevious).
func RollbackDir(cur string) error {
	prev, aside := PrevDir(cur), cur+asideSuffix
	if !isDir(prev) {
		return ErrNoPrevious
	}
	_ = os.RemoveAll(aside)
	hadCur := isDir(cur)
	if hadCur {
		if err := renameDir(cur, aside); err != nil {
			return NewDirBusyError(err)
		}
	}
	if err := renameDir(prev, cur); err != nil {
		var back error
		if hadCur {
			back = renameDir(aside, cur)
		}
		return NewDirBusyError(errors.Join(err, back))
	}
	syncDir(filepath.Dir(cur))
	_ = os.RemoveAll(aside)
	return nil
}

// CleanSwapLeftovers убирает временные каталоги прерванной замены рядом с cur.
// Вызывается под замком применения: работающей замены в это время нет. Каталог
// просматривается целиком, а не glob: «[» и «*» в пути данных ничего не ломают.
func CleanSwapLeftovers(cur string) {
	parent, base := filepath.Dir(cur), filepath.Base(cur)
	entries, err := os.ReadDir(parent)
	if err != nil {
		return
	}
	for _, e := range entries {
		n := e.Name()
		if strings.HasPrefix(n, base+newInfix) || n == base+discardSuffix || n == base+asideSuffix {
			_ = os.RemoveAll(filepath.Join(parent, n))
		}
	}
}

// syncDir — best-effort fsync каталога (на Windows открыть каталог для Sync нельзя).
func syncDir(p string) {
	if f, err := os.Open(p); err == nil {
		_ = f.Sync()
		_ = f.Close()
	}
}

// writeFileSynced создаёт новый файл (O_EXCL), пишет и сбрасывает на диск.
func writeFileSynced(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
