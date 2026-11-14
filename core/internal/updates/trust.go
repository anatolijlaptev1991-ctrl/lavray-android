// SPDX-License-Identifier: GPL-3.0-only

// trust.go — корень доверия: пин вшитого root.json, проверка документов по
// ролям, ротация N → N+1 и история принятых корней. Цепочка корней от вшитого
// хранится у Trust: её сохраняют на диск и перепроверяют от вшитого корня при
// каждой загрузке, поэтому подделанный «принятый корень» в хранилище бесполезен.
package updates

import "fmt"

// Trust — текущий корень доверия: проверенный root.json (пин приложения или
// результат ротации) + разобранная подписанная часть + путь от вшитого корня.
type Trust struct {
	raw     []byte
	root    *Root
	history []*Root  // разобранные корни от вшитого до текущего включительно
	chain   [][]byte // документы корней после вшитого (версии base+1 … текущая)
}

// NewTrust — пин корня в приложении: строгий разбор + самоподпись по
// собственной роли root (целостность внедрённого документа).
func NewTrust(rootJSON []byte) (*Trust, error) {
	env, err := ParseEnvelope(rootJSON)
	if err != nil {
		return nil, err
	}
	root, err := ParseRoot(env.Signed)
	if err != nil {
		return nil, err
	}
	if err := VerifyEnvelope(env, RoleRoot, root); err != nil {
		return nil, err
	}
	return &Trust{raw: append([]byte(nil), rootJSON...), root: root, history: []*Root{root}}, nil
}

// Root — разобранная подписанная часть корня.
func (t *Trust) Root() *Root { return t.root }

// Raw — байты текущего доверенного root.json.
func (t *Trust) Raw() []byte { return append([]byte(nil), t.raw...) }

// Chain — документы принятых корней после вшитого (для сохранения на диск).
func (t *Trust) Chain() [][]byte {
	out := make([][]byte, len(t.chain))
	for i, raw := range t.chain {
		out[i] = append([]byte(nil), raw...)
	}
	return out
}

// Verify — разобрать и проверить подпись документа для роли корня доверия.
// Возвращает конверт; подписанная часть — env.Signed.
func (t *Trust) Verify(raw []byte, role string) (*Envelope, error) {
	env, err := ParseEnvelope(raw)
	if err != nil {
		return nil, err
	}
	if err := VerifyEnvelope(env, role, t.root); err != nil {
		return nil, err
	}
	return env, nil
}

// VerifyRootUpdate — ротация корня (§3 правила, SECURITY §4.1): root.json
// версии N+1, подписанный порогом СТАРЫХ ключей роли root. Возвращает новый
// Trust; старый остаётся нетронутым при ошибке.
func (t *Trust) VerifyRootUpdate(raw []byte) (*Trust, error) {
	env, err := ParseEnvelope(raw)
	if err != nil {
		return nil, err
	}
	newRoot, err := ParseRoot(env.Signed)
	if err != nil {
		return nil, err
	}
	if newRoot.Version != t.root.Version+1 {
		return nil, fmt.Errorf("%w: текущая %d, кандидат %d (нужна ровно N+1)", ErrRootNotAdvanced, t.root.Version, newRoot.Version)
	}
	if err := VerifyEnvelope(env, RoleRoot, t.root); err != nil {
		return nil, fmt.Errorf("ротация root: %w", err)
	}
	// M1 (аудит round 2): TUF требует, чтобы новый root был подписан и
	// НОВЫМИ ключами самого документа — иначе один скомпрометированный
	// старый ключ переписывает всю базу доверия.
	if err := VerifyEnvelope(env, RoleRoot, newRoot); err != nil {
		return nil, fmt.Errorf("ротация root: не подписан новыми ключами: %w", err)
	}
	// Срок действия промежуточного корня не проверяется: «просроченный
	// встроенный корень → валидный по цепочке» (rootchain.go проверяет только
	// итоговый корень).
	next := &Trust{raw: append([]byte(nil), raw...), root: newRoot}
	next.history = append(append([]*Root(nil), t.history...), newRoot)
	next.chain = append(append([][]byte(nil), t.chain...), append([]byte(nil), raw...))
	return next, nil
}

// KeysRemovedSince: из роли role исчез хотя бы один ключ, который был в ней в корне
// версии since. Ключи сравниваются по значению (алгоритм и ключ), а не по имени:
// добавление второго ключа или переименование того же ключа счётчики не сбрасывает,
// пока старый ключ действует. Корня такой версии нет в истории (например, сборка
// получила новый вшитый корень) — считаем, что ключ удалён: счётчики безопаснее
// сбросить, чем навсегда застрять на устаревших.
func (t *Trust) KeysRemovedSince(since int64, role string) bool {
	for _, past := range t.history {
		if past.Version != since {
			continue
		}
		now := roleKeyValues(t.root, role)
		for key := range roleKeyValues(past, role) {
			if !now[key] {
				return true
			}
		}
		return false
	}
	return true
}

// roleKeyValues — ключи роли как множество «алгоритм|ключ».
func roleKeyValues(r *Root, role string) map[string]bool {
	out := map[string]bool{}
	for _, id := range r.Roles[role].Keyids {
		k := r.Keys[id]
		out[k.Alg+"|"+k.Pub] = true
	}
	return out
}
