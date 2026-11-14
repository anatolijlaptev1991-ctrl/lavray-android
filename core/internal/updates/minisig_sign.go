// SPDX-License-Identifier: GPL-3.0-only

// minisig_sign.go — выпуск подписи minisign (prehashed «ED») ключом ed25519:
// им пользуются утилита updsign и тесты; клиент обновлений только проверяет.
package updates

import (
	"crypto/ed25519"
	"encoding/base64"

	"golang.org/x/crypto/blake2b"
)

// SignMinisigPrehashed подписывает BLAKE2b-512 файла и trusted comment; keyid —
// идентификатор ключа из публичного файла. Результат — четыре строки (LF).
func SignMinisigPrehashed(priv ed25519.PrivateKey, keyID [8]byte, digest [DigestSize]byte, trusted string) []byte {
	sig := ed25519.Sign(priv, digest[:])
	global := ed25519.Sign(priv, append(append([]byte(nil), sig...), trusted...))
	blob := append(append([]byte(MinisigAlgPrehashed), keyID[:]...), sig...)
	return []byte("untrusted comment: signature from lavray updsign\n" +
		base64.StdEncoding.EncodeToString(blob) + "\n" +
		"trusted comment: " + trusted + "\n" +
		base64.StdEncoding.EncodeToString(global) + "\n")
}

// DigestOf — BLAKE2b-512 содержимого (то, что подписывает «ED»).
func DigestOf(data []byte) [DigestSize]byte { return blake2b.Sum512(data) }
