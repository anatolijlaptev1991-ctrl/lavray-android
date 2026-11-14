// SPDX-License-Identifier: GPL-3.0-only

package backup

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
)

// testHeader — заголовок с заметными значениями каждого поля.
func testHeader() Header {
	h := Header{Params: Params{T: 3, MKiB: 64 * 1024, P: 4}}
	for i := range h.Salt {
		h.Salt[i] = byte(0x10 + i)
	}
	for i := range h.Nonce {
		h.Nonce[i] = byte(0x40 + i)
	}
	for i := range h.KeyCheck {
		h.KeyCheck[i] = byte(0x80 + i)
	}
	return h
}

func TestHeaderRoundTrip(t *testing.T) {
	h := testHeader()
	raw := h.Marshal()
	if len(raw) != HeaderSize {
		t.Fatalf("len = %d, want %d", len(raw), HeaderSize)
	}
	if !bytes.HasPrefix(raw, []byte("LAVBK1")) {
		t.Fatalf("magic: %q", raw[:6])
	}
	got, err := ParseHeader(raw)
	if err != nil {
		t.Fatalf("ParseHeader: %v", err)
	}
	if got != h {
		t.Fatalf("round trip: %+v != %+v", got, h)
	}
	// хвост после заголовка (шифртекст) разбору не мешает
	if _, err := ParseHeader(append(raw, 1, 2, 3)); err != nil {
		t.Fatalf("с хвостом: %v", err)
	}
}

// TestHeaderRejectsBadMagicVersionParams — I, S: любой выход за границы
// заголовка отвергается до дорогого KDF (защита от DoS).
func TestHeaderRejectsBadMagicVersionParams(t *testing.T) {
	mut := func(f func(b []byte)) []byte {
		b := testHeader().Marshal()
		f(b)
		return b
	}
	put32 := func(b []byte, off int, v uint32) { binary.LittleEndian.PutUint32(b[off:], v) }
	cases := []struct {
		name string
		raw  []byte
	}{
		{"пусто", nil},
		{"короче заголовка", testHeader().Marshal()[:HeaderSize-1]},
		{"magic", mut(func(b []byte) { b[0] = 'X' })},
		{"версия 0", mut(func(b []byte) { b[offVersion] = 0 })},
		{"версия 2", mut(func(b []byte) { b[offVersion] = 2 })},
		{"kdf 0", mut(func(b []byte) { b[offKDF] = 0 })},
		{"kdf 2", mut(func(b []byte) { b[offKDF] = 2 })},
		{"t=0", mut(func(b []byte) { put32(b, offT, 0) })},
		{"t=11", mut(func(b []byte) { put32(b, offT, 11) })},
		{"t=огромное", mut(func(b []byte) { put32(b, offT, 0xFFFFFFFF) })},
		{"m=8МиБ-1КиБ", mut(func(b []byte) { put32(b, offM, 8*1024-1) })},
		{"m=512МиБ+1КиБ", mut(func(b []byte) { put32(b, offM, 512*1024+1) })},
		{"m=огромное", mut(func(b []byte) { put32(b, offM, 0xFFFFFFFF) })},
		{"p=0", mut(func(b []byte) { b[offP] = 0 })},
		{"p=17", mut(func(b []byte) { b[offP] = 17 })},
		{"reserved не нулевой", mut(func(b []byte) { b[HeaderSize-1] = 1 })},
	}
	for _, c := range cases {
		if _, err := ParseHeader(c.raw); !errors.Is(err, ErrCorrupt) {
			t.Errorf("%s: err = %v, want ErrCorrupt", c.name, err)
		}
	}
	// границы допустимы: t 1 и 10, m 8 и 512 МиБ, p 1 и 16
	for _, p := range []Params{{1, 8 * 1024, 1}, {10, 512 * 1024, 16}} {
		h := testHeader()
		h.Params = p
		if _, err := ParseHeader(h.Marshal()); err != nil {
			t.Errorf("граница %+v отвергнута: %v", p, err)
		}
	}
}

func FuzzParseHeader(f *testing.F) {
	f.Add(testHeader().Marshal())
	f.Add([]byte("LAVBK1"))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		h, err := ParseHeader(data)
		if err != nil {
			if !errors.Is(err, ErrCorrupt) {
				t.Fatalf("ошибка не ErrCorrupt: %v", err)
			}
			return
		}
		if !bytes.Equal(h.Marshal(), data[:HeaderSize]) {
			t.Fatalf("разбор и сборка не взаимно обратны")
		}
	})
}
