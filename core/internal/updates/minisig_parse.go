// SPDX-License-Identifier: GPL-3.0-only

// minisig_parse.go — разбор файла подписи и публичного ключа minisign
// (формат https://jedisct1.github.io/minisign/): четыре строки подписи
// (untrusted comment, base64 «алгоритм+keyid+подпись», trusted comment,
// глобальная подпись) и ключ «Ed + keyid + ed25519». Сама проверка — в
// minisig_verify.go.
package updates

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"strings"
)

// Алгоритмы подписи файла: прямой Ed25519 («Ed», старый) и по BLAKE2b-512
// от файла («ED», prehashed — основной у minisign ≥ 0.6 и у updsign).
const (
	MinisigAlgLegacy    = "Ed"
	MinisigAlgPrehashed = "ED"
)

const (
	minisigMaxBytes     = 4096 // подпись артефакта — сотни байт; больше — мусор
	untrustedPrefix     = "untrusted comment:"
	trustedPrefix       = "trusted comment: "
	minisigSigBlobLen   = 2 + 8 + ed25519.SignatureSize
	minisigPubBlobLen   = 2 + 8 + ed25519.PublicKeySize
	minisigGlobalSigLen = ed25519.SignatureSize
)

// Minisig — разобранный файл подписи.
type Minisig struct {
	Alg              string
	KeyID            [8]byte
	Sig              [ed25519.SignatureSize]byte
	UntrustedComment string
	TrustedComment   string
	GlobalSig        [ed25519.SignatureSize]byte
}

// MinisignKey — публичный ключ minisign (keyid + ed25519).
type MinisignKey struct {
	KeyID [8]byte
	Pub   ed25519.PublicKey
}

// ParseMinisig разбирает четыре строки подписи. Допускаются окончания CRLF и
// пустые строки в конце; любая другая лишняя строка, усечение, неверный
// алгоритм или длина — ErrMinisigFormat.
func ParseMinisig(raw []byte) (*Minisig, error) {
	if len(raw) == 0 || len(raw) > minisigMaxBytes {
		return nil, fmt.Errorf("%w: размер %d байт", ErrMinisigFormat, len(raw))
	}
	lines := splitLines(raw)
	if len(lines) != 4 {
		return nil, fmt.Errorf("%w: ожидается 4 строки, получено %d", ErrMinisigFormat, len(lines))
	}
	if !strings.HasPrefix(lines[0], untrustedPrefix) {
		return nil, fmt.Errorf("%w: нет строки untrusted comment", ErrMinisigFormat)
	}
	if !strings.HasPrefix(lines[2], trustedPrefix) {
		return nil, fmt.Errorf("%w: нет строки trusted comment", ErrMinisigFormat)
	}
	blob, err := decodeBlob(lines[1], minisigSigBlobLen, "подпись")
	if err != nil {
		return nil, err
	}
	global, err := decodeBlob(lines[3], minisigGlobalSigLen, "глобальная подпись")
	if err != nil {
		return nil, err
	}
	m := &Minisig{
		Alg:              string(blob[:2]),
		UntrustedComment: strings.TrimSpace(strings.TrimPrefix(lines[0], untrustedPrefix)),
		TrustedComment:   strings.TrimPrefix(lines[2], trustedPrefix),
	}
	if m.Alg != MinisigAlgLegacy && m.Alg != MinisigAlgPrehashed {
		return nil, fmt.Errorf("%w: алгоритм %q", ErrMinisigFormat, m.Alg)
	}
	copy(m.KeyID[:], blob[2:10])
	copy(m.Sig[:], blob[10:])
	copy(m.GlobalSig[:], global)
	return m, nil
}

// ParseMinisignPublicKey принимает либо строку base64 ключа («RWQf…»), либо
// файл .pub целиком (untrusted comment + строка ключа).
func ParseMinisignPublicKey(text string) (*MinisignKey, error) {
	lines := splitLines([]byte(text))
	if len(lines) == 2 && strings.HasPrefix(lines[0], untrustedPrefix) {
		lines = lines[1:]
	}
	if len(lines) != 1 {
		return nil, fmt.Errorf("%w: публичный ключ — одна строка base64", ErrMinisigFormat)
	}
	blob, err := decodeBlob(lines[0], minisigPubBlobLen, "публичный ключ")
	if err != nil {
		return nil, err
	}
	return MinisignKeyFromBlob(blob)
}

// MinisignKeyFromBlob собирает ключ из 42 байт «Ed + keyid + ed25519»
// (так он хранится в root.json под alg "minisign").
func MinisignKeyFromBlob(blob []byte) (*MinisignKey, error) {
	if len(blob) != minisigPubBlobLen || string(blob[:2]) != MinisigAlgLegacy {
		return nil, fmt.Errorf("%w: ключ должен начинаться с %q и занимать %d байт", ErrMinisigFormat, MinisigAlgLegacy, minisigPubBlobLen)
	}
	k := &MinisignKey{Pub: ed25519.PublicKey(append([]byte(nil), blob[10:]...))}
	copy(k.KeyID[:], blob[2:10])
	return k, nil
}

// Blob возвращает 42 байта «Ed + keyid + ключ».
func (k *MinisignKey) Blob() []byte {
	out := append([]byte(MinisigAlgLegacy), k.KeyID[:]...)
	return append(out, k.Pub...)
}

// Base64 — строка публичного ключа в формате файла .pub («RWQ…»).
func (k *MinisignKey) Base64() string { return base64.StdEncoding.EncodeToString(k.Blob()) }

// splitLines режет на строки по LF, снимая CR; хвостовые пустые строки
// отбрасываются, пустая строка внутри остаётся (и сломает разбор).
func splitLines(raw []byte) []string {
	raw = bytes.TrimRight(raw, "\r\n ")
	parts := strings.Split(string(raw), "\n")
	for i := range parts {
		parts[i] = strings.TrimSuffix(parts[i], "\r")
	}
	return parts
}

func decodeBlob(line string, want int, what string) ([]byte, error) {
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(line))
	if err != nil || len(b) != want {
		return nil, fmt.Errorf("%w: %s — base64 на %d байт", ErrMinisigFormat, what, want)
	}
	return b, nil
}
