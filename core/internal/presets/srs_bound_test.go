// SPDX-License-Identifier: GPL-3.0-only

package presets

import (
	"bytes"
	"compress/zlib"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// srsOfZeros — .srs, чей zlib-поток разжимается в mib МиБ нулей.
func srsOfZeros(mib int) []byte {
	var zb bytes.Buffer
	zw, _ := zlib.NewWriterLevel(&zb, zlib.BestCompression)
	chunk := make([]byte, 1<<20)
	for i := 0; i < mib; i++ {
		_, _ = zw.Write(chunk)
	}
	_ = zw.Close()
	return append([]byte{'S', 'R', 'S', 1}, zb.Bytes()...)
}

// S: разжатие .srs ограничено — вложенная zlib-бомба не считается корректным файлом.
func TestValidSRSRejectsInflationBomb(t *testing.T) {
	bomb := srsOfZeros(MaxSRSInflated>>20 + 16)
	start := time.Now()
	if ValidSRS(bomb) {
		t.Fatal("файл, разжимающийся за предел, принят")
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("проверка заняла %v", d)
	}
	if !ValidSRS(srsOfZeros(2)) {
		t.Fatal("небольшой корректный файл отвергнут")
	}
}

func TestInflatedSRSSizeReportsAndCaps(t *testing.T) {
	raw := srsOfZeros(2)
	n, err := InflatedSRSSize(raw, 4<<20)
	if err != nil || n != 2<<20 {
		t.Fatalf("размер: %d %v", n, err)
	}
	if _, err := InflatedSRSSize(raw, 1<<20); !errors.Is(err, ErrSRSTooLarge) {
		t.Fatalf("выше предела: %v", err)
	}
	// Разжатие действительно прерывается на пределе, а не доводится до конца потока.
	if n, err := InflatedSRSSize(srsOfZeros(64), 1<<20); !errors.Is(err, ErrSRSTooLarge) || n > 1<<20+1 {
		t.Fatalf("разжато %d байт при пределе %d: %v", n, 1<<20, err)
	}
	if _, err := InflatedSRSSize([]byte("SRS"), 1<<20); !errors.Is(err, ErrSRSInvalid) {
		t.Fatalf("короткий файл: %v", err)
	}
	if _, err := InflatedSRSSize(append([]byte{'S', 'R', 'S', 1}, "not zlib"...), 1<<20); !errors.Is(err, ErrSRSInvalid) {
		t.Fatalf("не zlib: %v", err)
	}
	embedded, _ := GeoFile("geoip/cn.srs")
	if _, err := InflatedSRSSize(embedded, MaxSRSInflated); err != nil {
		t.Fatalf("встроенный файл: %v", err)
	}
}

// Запуск (EnsureGeo → geoFileOK → validSRS) не зависает на файле-бомбе: его просто чинят из встроенного.
func TestEnsureGeoRepairsBombFileQuickly(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "geo")
	if _, err := ExtractGeo(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, GeoMarkerFile), []byte("5 2026-01-01T00:00:00Z\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "geosite", "openai.srs")
	if err := os.WriteFile(target, srsOfZeros(MaxSRSInflated>>20+16), 0o600); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	n, err := ExtractGeo(dir)
	if err != nil || n != 1 {
		t.Fatalf("ExtractGeo: %d %v", n, err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("запуск занял %v", time.Since(start))
	}
	got, _ := os.ReadFile(target)
	want, _ := GeoFile("geosite/openai.srs")
	if !bytes.Equal(got, want) {
		t.Fatal("файл-бомба должен быть заменён встроенным")
	}
}

// Один подписанный огромный rev не должен навсегда закрыть набор.
func TestValidateCapsRevision(t *testing.T) {
	list, err := BuiltinPresets()
	if err != nil {
		t.Fatal(err)
	}
	p := list[0].Clone()
	p.Revision = MaxRevision
	if err := p.Validate(); err != nil {
		t.Fatalf("ревизия на потолке: %v", err)
	}
	p.Revision = MaxRevision + 1
	if err := p.Validate(); err == nil {
		t.Fatal("ревизия выше потолка должна отвергаться")
	}
}
