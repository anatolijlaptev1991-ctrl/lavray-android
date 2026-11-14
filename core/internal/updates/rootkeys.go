// SPDX-License-Identifier: GPL-3.0-only

// rootkeys.go — проверка ключей и ролей root.json. Роли root/timestamp/
// manifest/sources подписываются ключами ed25519 (alg "ed25519", pub — 32 байта
// base64); роль artifact — ключами minisign (alg "minisign", pub — 42 байта
// base64: «Ed + keyid + ed25519», как в файле .pub). Смешивать нельзя:
// ключ minisign не может подписать метаданные, ключ ed25519 — установщик.
package updates

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
)

const (
	algEd25519  = "ed25519"
	algMinisign = "minisign"
)

// validateRootKeys — алгоритм и длина каждого ключа.
func validateRootKeys(r *Root) error {
	for id, k := range r.Keys {
		pub, err := base64.StdEncoding.DecodeString(k.Pub)
		switch k.Alg {
		case algEd25519:
			if err != nil || len(pub) != ed25519.PublicKeySize {
				return fmt.Errorf("%w: ключ %s: pub не является ed25519-ключом (base64, %d байт)", ErrBadValue, id, ed25519.PublicKeySize)
			}
		case algMinisign:
			if err != nil {
				return fmt.Errorf("%w: ключ %s: pub не base64", ErrBadValue, id)
			}
			if _, kerr := MinisignKeyFromBlob(pub); kerr != nil {
				return fmt.Errorf("%w: ключ %s: %v", ErrBadValue, id, kerr)
			}
		default:
			return fmt.Errorf("%w: ключ %s: alg=%q (поддерживаются ed25519 и minisign)", ErrBadValue, id, k.Alg)
		}
	}
	return nil
}

// validateRootRoles — четыре обязательные роли с ключами ed25519 и
// необязательная artifact с ключами minisign (её отсутствие — не ошибка
// разбора, а ErrArtifactRoleMissing при попытке проверить установщик).
func validateRootRoles(r *Root) error {
	for _, role := range []string{RoleRoot, RoleTimestamp, RoleManifest, RoleSources} {
		if _, ok := r.Roles[role]; !ok {
			return fmt.Errorf("%w: root.roles без роли %q", ErrMissingField, role)
		}
		if err := validateRole(r, role, algEd25519); err != nil {
			return err
		}
	}
	if _, ok := r.Roles[RoleArtifact]; ok {
		return validateRole(r, RoleArtifact, algMinisign)
	}
	return nil
}

func validateRole(r *Root, role, alg string) error {
	rl := r.Roles[role]
	if rl.Threshold < 1 {
		return fmt.Errorf("%w: роль %s: threshold=%d < 1", ErrBadValue, role, rl.Threshold)
	}
	if len(rl.Keyids) < rl.Threshold {
		return fmt.Errorf("%w: роль %s: keyids (%d) меньше threshold (%d)", ErrBadValue, role, len(rl.Keyids), rl.Threshold)
	}
	seen := map[string]bool{}
	for _, id := range rl.Keyids {
		if id == "" {
			return fmt.Errorf("%w: роль %s: пустой keyid", ErrBadValue, role)
		}
		if seen[id] {
			return fmt.Errorf("%w: роль %s: дубликат keyid %s", ErrBadValue, role, id)
		}
		seen[id] = true
		key, ok := r.Keys[id]
		if !ok {
			return fmt.Errorf("%w: роль %s: keyid %s отсутствует в keys", ErrBadValue, role, id)
		}
		if key.Alg != alg {
			return fmt.Errorf("%w: роль %s: ключ %s имеет alg=%q, нужен %q", ErrBadValue, role, id, key.Alg, alg)
		}
	}
	return nil
}

// ArtifactKeys — ключи minisign роли artifact. Нет роли или ключей —
// ErrArtifactRoleMissing: клиент не принимает установщик без подписи.
func (r *Root) ArtifactKeys() ([]*MinisignKey, error) {
	rl, ok := r.Roles[RoleArtifact]
	if !ok || len(rl.Keyids) == 0 {
		return nil, ErrArtifactRoleMissing
	}
	keys := make([]*MinisignKey, 0, len(rl.Keyids))
	for _, id := range rl.Keyids {
		pub, err := base64.StdEncoding.DecodeString(r.Keys[id].Pub)
		if err != nil {
			continue
		}
		if k, err := MinisignKeyFromBlob(pub); err == nil {
			keys = append(keys, k)
		}
	}
	if len(keys) == 0 {
		return nil, ErrArtifactRoleMissing
	}
	return keys, nil
}

// VerifyArtifactSignature проверяет prehashed-подпись установщика любым из
// ключей роли artifact (keyid подписи выбирает ключ) и сверяет trusted comment.
func (r *Root) VerifyArtifactSignature(sig *Minisig, digest [DigestSize]byte, want ArtifactClaims) error {
	keys, err := r.ArtifactKeys()
	if err != nil {
		return err
	}
	var last error = ErrMinisigKeyID
	for _, k := range keys {
		if k.KeyID != sig.KeyID {
			continue
		}
		if last = k.VerifyDigest(sig, digest); last == nil {
			return sig.CheckClaims(want)
		}
	}
	return last
}
