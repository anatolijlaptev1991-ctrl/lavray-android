// SPDX-License-Identifier: GPL-3.0-only

// Package backup — формат зашифрованной резервной копии LAVBK1 (T-83):
// строгий заголовок, Argon2id, XChaCha20-Poly1305. Пакет ничего не знает о
// базе данных: он шифрует и упаковывает готовые байты снимка.
//
// Файл = заголовок (HeaderSize байт, он же AAD шифра) ‖ шифртекст с тегом.
// Раскладка заголовка (числа little-endian):
//
//	0   6  magic "LAVBK1"
//	6   1  ver = 1
//	7   1  kdf = 1 (Argon2id)
//	8   4  t      — число проходов
//	12  4  m_KiB  — память, КиБ
//	16  1  p      — потоки
//	17  16 salt
//	33  24 nonce
//	57  32 keycheck = HMAC-SHA256(key, "LAVBK1 key check")
//	89  7  reserved (нули)
package backup

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

// Размеры и смещения полей заголовка.
const (
	Magic      = "LAVBK1"
	Version    = 1
	KDFArgon2  = 1
	HeaderSize = 96

	SaltSize     = 16
	NonceSize    = 24
	KeyCheckSize = 32
	TagSize      = 16 // тег Poly1305

	offVersion  = 6
	offKDF      = 7
	offT        = 8
	offM        = 12
	offP        = 16
	offSalt     = 17
	offNonce    = 33
	offKeyCheck = 57
	offReserved = 89
)

// Границы параметров Argon2id при разборе: чужой файл не должен заставить нас
// считать ключ часами или занять гигабайты памяти (DoS).
const (
	MinT, MaxT       = 1, 10
	MinMKiB, MaxMKiB = 8 * 1024, 512 * 1024
	MinP, MaxP       = 1, 16
)

// Params — параметры Argon2id.
type Params struct {
	T    uint32 // проходы
	MKiB uint32 // память, КиБ
	P    uint8  // параллелизм
}

// DefaultParams — параметры новой копии: t=3, m=64 МиБ, p=4.
func DefaultParams() Params { return Params{T: 3, MKiB: 64 * 1024, P: 4} }

// Validate проверяет границы параметров.
func (p Params) Validate() error {
	switch {
	case p.T < MinT || p.T > MaxT:
		return fmt.Errorf("t=%d вне [%d..%d]", p.T, MinT, MaxT)
	case p.MKiB < MinMKiB || p.MKiB > MaxMKiB:
		return fmt.Errorf("m=%d КиБ вне [%d..%d]", p.MKiB, MinMKiB, MaxMKiB)
	case p.P < MinP || p.P > MaxP:
		return fmt.Errorf("p=%d вне [%d..%d]", p.P, MinP, MaxP)
	}
	return nil
}

// Header — разобранный заголовок.
type Header struct {
	Params   Params
	Salt     [SaltSize]byte
	Nonce    [NonceSize]byte
	KeyCheck [KeyCheckSize]byte
}

// Marshal собирает HeaderSize байт; reserved — нули.
func (h Header) Marshal() []byte {
	b := make([]byte, HeaderSize)
	copy(b, Magic)
	b[offVersion] = Version
	b[offKDF] = KDFArgon2
	binary.LittleEndian.PutUint32(b[offT:], h.Params.T)
	binary.LittleEndian.PutUint32(b[offM:], h.Params.MKiB)
	b[offP] = h.Params.P
	copy(b[offSalt:], h.Salt[:])
	copy(b[offNonce:], h.Nonce[:])
	copy(b[offKeyCheck:], h.KeyCheck[:])
	return b
}

// ParseHeader разбирает начало data строго: магия, версия, kdf, границы
// параметров и нулевой reserved. Любое отступление — ErrCorrupt. Шифртекст
// после заголовка не читается.
func ParseHeader(data []byte) (Header, error) {
	var h Header
	if len(data) < HeaderSize {
		return h, fmt.Errorf("%w: заголовок короче %d байт", ErrCorrupt, HeaderSize)
	}
	if string(data[:len(Magic)]) != Magic {
		return h, fmt.Errorf("%w: нет сигнатуры %s", ErrCorrupt, Magic)
	}
	if data[offVersion] != Version || data[offKDF] != KDFArgon2 {
		return h, fmt.Errorf("%w: версия %d / kdf %d не поддерживаются", ErrCorrupt, data[offVersion], data[offKDF])
	}
	h.Params = Params{
		T:    binary.LittleEndian.Uint32(data[offT:]),
		MKiB: binary.LittleEndian.Uint32(data[offM:]),
		P:    data[offP],
	}
	if err := h.Params.Validate(); err != nil {
		return Header{}, fmt.Errorf("%w: параметры Argon2id: %v", ErrCorrupt, err)
	}
	if !bytes.Equal(data[offReserved:HeaderSize], make([]byte, HeaderSize-offReserved)) {
		return Header{}, fmt.Errorf("%w: reserved не нулевой", ErrCorrupt)
	}
	copy(h.Salt[:], data[offSalt:])
	copy(h.Nonce[:], data[offNonce:])
	copy(h.KeyCheck[:], data[offKeyCheck:])
	return h, nil
}
