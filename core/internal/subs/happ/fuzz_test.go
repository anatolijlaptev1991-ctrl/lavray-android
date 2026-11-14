// SPDX-License-Identifier: GPL-3.0-only

package happ

import (
	"testing"
)

// FuzzDecrypt — инвариант LNB-13/THREAT-MODEL «паника ядра»: Decrypt на любом
// входе либо возвращает ошибку, либо строку; паник нет. В CI — 30 с
// (core-port-plan §4.3; NIGHTLY-прогон длиннее).
func FuzzDecrypt(f *testing.F) {
	for _, v := range verifiedVectors {
		f.Add(v.Input)
	}
	f.Add("happ://crypt/")
	f.Add("happ://crypt5/short")
	f.Add("happ://crypt5/AAAAAAAAAAAAAAAA")
	f.Add("happ://crypt6/QUJD")
	f.Add("happ://crypt5/" + "\x00\x01\x02QZ\x00\x00\x00\x00\x00\x00\x00\x00\x0012fQQ==")
	f.Add("HAPP://CRYPT5/QUJD")
	f.Add("")
	f.Fuzz(func(t *testing.T, s string) {
		got, err := Decrypt(s)
		if err == nil && got == "" && s != "" {
			// Пустой результат без ошибки теоретически возможен (пустой
			// plaintext), не ошибка.
			_ = got
		}
	})
}
