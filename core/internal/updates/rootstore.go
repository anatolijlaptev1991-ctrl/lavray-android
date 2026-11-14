// SPDX-License-Identifier: GPL-3.0-only

// rootstore.go — хранение принятых корней доверия между запусками. Хранится
// ЦЕПОЧКА документов (N.root.json), а не «последний корень»: при каждой
// загрузке она перепроверяется от вшитого в сборку корня (порог ключей
// предыдущего корня и подпись новыми). Подложенный в хранилище самоподписанный
// корень в цепочку не встаёт и игнорируется; понизить версию хранилище тоже не
// может — вшитый корень всегда начало цепочки.
package updates

import "fmt"

// RootStore — хранилище документов принятых корней (внедряется: api — каталог
// <данные>/updates/roots; тесты — память).
type RootStore interface {
	// LoadRoots возвращает все сохранённые документы (порядок любой, мусор допустим).
	LoadRoots() ([][]byte, error)
	// SaveRoot сохраняет документ корня версии version.
	SaveRoot(version int64, raw []byte) error
}

// ResolveTrust строит доверие: вшитый корень, затем сохранённые N+1, N+2, …,
// каждый шаг — полная проверка ротации. Цепочка обрывается на первом
// отсутствующем или недействительном документе. Нет вшитого корня —
// ErrNotConfigured (обновления не настроены в этой сборке).
func ResolveTrust(embedded []byte, store RootStore) (*Trust, error) {
	if len(embedded) == 0 {
		return nil, ErrNotConfigured
	}
	cur, err := NewTrust(embedded)
	if err != nil {
		return nil, fmt.Errorf("вшитый root.json: %w", err)
	}
	if store == nil {
		return cur, nil
	}
	docs, err := store.LoadRoots()
	if err != nil {
		return cur, nil // хранилище нечитаемо — работаем от вшитого корня
	}
	byVersion := map[int64][]byte{}
	for _, raw := range docs {
		if v, ok := rootVersionOf(raw); ok {
			byVersion[v] = raw
		}
	}
	for steps := 0; steps < MaxRootSteps; steps++ {
		raw, ok := byVersion[cur.Root().Version+1]
		if !ok {
			break
		}
		next, verr := cur.VerifyRootUpdate(raw)
		if verr != nil {
			break // цепочка не сходится: дальше хранилищу не верим
		}
		cur = next
	}
	return cur, nil
}

// PersistRoot сохраняет звенья цепочки, которых ещё нет в хранилище
// (повторное сохранение ничего не пишет — идемпотентно).
func PersistRoot(store RootStore, t *Trust) error {
	if store == nil || t == nil {
		return nil
	}
	have := map[int64]bool{}
	if docs, err := store.LoadRoots(); err == nil {
		for _, raw := range docs {
			if v, ok := rootVersionOf(raw); ok {
				have[v] = true
			}
		}
	}
	for _, raw := range t.Chain() {
		v, ok := rootVersionOf(raw)
		if !ok || have[v] {
			continue
		}
		if err := store.SaveRoot(v, raw); err != nil {
			return err
		}
	}
	return nil
}

// rootVersionOf — версия из документа корня (без проверки подписей).
func rootVersionOf(raw []byte) (int64, bool) {
	env, err := ParseEnvelope(raw)
	if err != nil {
		return 0, false
	}
	root, err := ParseRoot(env.Signed)
	if err != nil {
		return 0, false
	}
	return root.Version, true
}
