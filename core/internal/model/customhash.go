// SPDX-License-Identifier: GPL-3.0-only

package model

// customhash.go — хэш custom-содержимого профиля (T-08B). Подтверждение
// custom-полей хранится как этот хэш и действует, пока содержимое не
// изменилось: CustomConfirmed пересчитывает хэш при каждом чтении, поэтому
// клиент не может «выставить флаг», а любая правка содержимого снимает
// подтверждение сама.
//
//	H = sha256("LAVCF1" 0x00 ‖ seg(custom_outbound) ‖ 0x00 ‖ seg(custom_config)
//	           [‖ 0x00 ‖ canon(full_config) — только internal-full])
//
// seg(x) — CanonicalJSON(x), пустой документ и "{}" дают "" (сборка конфига
// их тоже не применяет). Канонический JSON не содержит байта 0x00, поэтому
// разделитель однозначен: (x, "") и ("", x) дают разные хэши. Документ больше
// MaxCanonBytes не канонизируется — такое содержимое подтвердить нельзя.

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// customHashDomain — префикс версии формулы: смена формулы меняет все хэши.
const customHashDomain = "LAVCF1\x00"

// IsInternalFull — профиль «свой полный конфиг» (CUSTOM_CONFIG, core
// internal-full): конфиг ядра берётся из профиля целиком.
func IsInternalFull(p *Profile) bool {
	d, ok := p.Detail.(*CustomDetail)
	return ok && p.Type == TypeCustomConfig && d.Core == "internal-full"
}

// InternalFullJSON — полный конфиг internal-full профиля ("" у остальных).
func InternalFullJSON(p *Profile) string {
	if !IsInternalFull(p) {
		return ""
	}
	return p.Detail.(*CustomDetail).FullConfigJSON
}

// customSegment — каноническое значение одного custom-поля для хэша.
func customSegment(raw string) (string, error) {
	c, err := CanonicalJSON(raw)
	if err != nil || c == "{}" {
		return "", err
	}
	return c, nil
}

// customDigest — хэш содержимого и есть ли что подтверждать; err — одно из
// полей не канонизируется (тогда has = true: содержимое есть, но невалидно).
func customDigest(p *Profile) (hash string, has bool, err error) {
	out, errOut := customSegment(p.CustomOutboundJSON)
	cfg, errCfg := customSegment(p.CustomConfigJSON)
	full := IsInternalFull(p)
	has = full || out != "" || cfg != "" || errOut != nil || errCfg != nil
	if errOut != nil {
		return "", has, errOut
	}
	if errCfg != nil {
		return "", has, errCfg
	}
	h := sha256.New()
	h.Write([]byte(customHashDomain))
	h.Write([]byte(out))
	h.Write([]byte{0})
	h.Write([]byte(cfg))
	if full {
		canon, err := CanonicalJSON(InternalFullJSON(p))
		if err != nil {
			return "", has, err
		}
		h.Write([]byte{0})
		h.Write([]byte(canon))
	}
	return hex.EncodeToString(h.Sum(nil)), has, nil
}

// HasCustomContent — есть ли у профиля что подтверждать: непустое
// custom_outbound/custom_config (невалидный JSON тоже считается содержимым)
// или профиль internal-full.
func HasCustomContent(p *Profile) bool {
	if IsInternalFull(p) {
		return true
	}
	if strings.TrimSpace(p.CustomOutboundJSON) == "" && strings.TrimSpace(p.CustomConfigJSON) == "" {
		return false
	}
	_, has, _ := customDigest(p)
	return has
}

// CustomFieldsHash — sha256 (64 hex) custom-содержимого профиля. Ошибка —
// одно из полей не разбирается как JSON (дубли ключей, больше MaxCanonBytes).
func CustomFieldsHash(p *Profile) (string, error) {
	h, _, err := customDigest(p)
	return h, err
}

// CustomConfirmed — подтверждено ли текущее содержимое: есть что подтверждать
// и сохранённый CustomHash равен хэшу текущего содержимого.
func CustomConfirmed(p *Profile) bool {
	if p.CustomHash == "" {
		return false
	}
	h, has, err := customDigest(p)
	return err == nil && has && h == p.CustomHash
}
