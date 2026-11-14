// SPDX-License-Identifier: GPL-3.0-only

// mirror.go — какое именно зеркало помешало закрыть цепочку корней. Ротация корня
// закрывается по принципу fail-closed: конец цепочки объявляется, только если КАЖДОЕ
// зеркало ответило «нет файла». Молчащее зеркало — «проверка не удалась», и чтобы
// это было понятно, ошибка называет его адрес.
package updates

import (
	"fmt"
	"net/url"
	"strings"
)

// MirrorError — зеркала, не подтвердившие конец цепочки корней.
type MirrorError struct {
	Hosts []string
	Root  int64 // версия корня, которого не удалось исключить
}

func (e *MirrorError) Error() string {
	return fmt.Sprintf("%v: %s (не подтвердили, что корня %d нет)", ErrMirrorUnreachable, strings.Join(e.Hosts, ", "), e.Root)
}

// Is: ошибка остаётся «проверка не удалась» (ErrNetwork) и несёт причину.
func (e *MirrorError) Is(target error) bool {
	return target == ErrNetwork || target == ErrMirrorUnreachable
}

// sourceLabel — адрес источника для сообщений: хост HTTP-зеркала, иначе «источник №N».
func sourceLabel(s Source, index int) string {
	if h, ok := s.(*HTTPSource); ok {
		if u, err := url.Parse(h.Base); err == nil && u.Host != "" {
			return u.Host
		}
	}
	return fmt.Sprintf("источник №%d", index+1)
}
