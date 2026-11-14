// SPDX-License-Identifier: GPL-3.0-only

package model

// dedupkey.go — отпечаток «того же сервера» (Хвосты A). Один и тот же ключ
// используют импорт (пропуск уже имеющихся) и очистка дубликатов: два профиля
// равны, если из них собирается один и тот же конфиг.
//
// В ключ входят тип, адрес, порт, Detail (учётные данные и параметры типа),
// параметры транспорта и TLS (Stream) и custom-поля. Не входят имя, заметки,
// теги, избранное, страна, результат теста и трафик, id, группа и времена:
// переименование и статистика сервер другим не делают. Ключ — sha256, а не
// склейка полей: пароли в ключах-строках не нужны ни карте, ни журналу.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
)

// dedupDomain — префикс версии формулы: смена формулы меняет все ключи.
const dedupDomain = "LAVDK1\x00"

// dedupFields — поля профиля, определяющие конфиг (порядок полей фиксирован).
type dedupFields struct {
	Type           ProfileType     `json:"type"`
	Address        string          `json:"address"`
	Port           int             `json:"port"`
	Detail         Detail          `json:"detail,omitempty"`
	Stream         *StreamSettings `json:"stream,omitempty"`
	CustomOutbound string          `json:"custom_outbound,omitempty"`
	CustomConfig   string          `json:"custom_config,omitempty"`
}

// DedupKey — sha256 (64 hex) отпечатка профиля. Адрес сравнивается без учёта
// регистра и крайних пробелов: имена хостов от регистра не зависят. Пустая
// строка — отпечаток построить не удалось; такой профиль не считается дублем.
func DedupKey(p *Profile) string {
	b, err := json.Marshal(dedupFields{
		Type:           p.Type,
		Address:        strings.ToLower(strings.TrimSpace(p.Address)),
		Port:           p.Port,
		Detail:         p.Detail,
		Stream:         p.Stream,
		CustomOutbound: p.CustomOutboundJSON,
		CustomConfig:   p.CustomConfigJSON,
	})
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(append([]byte(dedupDomain), b...))
	return hex.EncodeToString(sum[:])
}
