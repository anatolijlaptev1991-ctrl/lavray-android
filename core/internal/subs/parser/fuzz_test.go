// SPDX-License-Identifier: GPL-3.0-only

package parser

import (
	"os"
	"path/filepath"
	"testing"
)

// FuzzParseContent — инвариант (core-port-plan §4.3, W3): парсер контента
// подписки на любом входе не паникует и возвращает либо результат, либо
// структурную ошибку. CI: 30 с (merge-гейт), nightly — дольше.
func FuzzParseContent(f *testing.F) {
	seeds := []string{
		"vmess://eyJwcyI6Ik5NVDEiLCJhZGQiOiJleGFtcGxlLmNvbSIsInBvcnQiOjQ0MywiaWQiOiI4MzQwY2ZjNy0zNDU2LTQ3ODktYTg5Yi0wMTIzNDU2Nzg5YWJjIn0=",
		"trojan://pass@example.org:443?sni=cdn#TR",
		"ss://YWVzLTI1Ni1nY206c2VjcmV0QDEuMi4zLjQ6ODM4OA==",
		`{"outbounds":[{"type":"vmess","tag":"a","server":"s","server_port":1,"uuid":"u"}]}`,
		"[{\"type\":\"selector\",\"outbounds\":[]}]",
		"happ://crypt5/short",
		"happ://crypt/QUJD",
		"<html><body>stub</body></html>",
		"#profile-title: base64:0JLRgdC10Lwg0YfQsNGB",
		"#subscription-userinfo: upload=1; expire=notanumber",
		"\x00\x01\x02\x03binary\xff",
		"   ",
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}
	f.Add(readSeedFixtures())
	f.Fuzz(func(t *testing.T, data []byte) {
		res, err := Parse(data, Options{MaxProfiles: 50})
		if err == nil && res == nil {
			t.Fatal("Parse вернул (nil, nil)")
		}
		// Профили из мусора допустимы (если строки распознались как ссылки);
		// инвариант — отсутствие паники.
	})
}

// readSeedFixtures собирает все фикстуры в один seed-корм.
func readSeedFixtures() []byte {
	out := make([]byte, 0, 4096)
	for _, name := range []string{
		"plain.txt", "base64.txt", "singbox.json", "happ.txt",
		"html.txt", "broken.txt", "singbox-broken.json", "json-no-outbounds.json",
	} {
		if b := mustRead(name); b != nil {
			out = append(out, b...)
			out = append(out, '\n')
		}
	}
	return out
}

func mustRead(name string) []byte {
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		return nil
	}
	return b
}
