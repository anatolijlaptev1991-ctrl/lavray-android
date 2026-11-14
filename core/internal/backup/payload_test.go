// SPDX-License-Identifier: GPL-3.0-only

package backup

import (
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"runtime"
	"testing"
)

// countWriter запоминает, сколько байт получено и каким самым крупным куском.
type countWriter struct{ total, maxChunk int64 }

func (w *countWriter) Write(p []byte) (int, error) {
	w.total += int64(len(p))
	w.maxChunk = max(w.maxChunk, int64(len(p)))
	return len(p), nil
}

// bomb — тело копии с gzip из mib МиБ нулей (они сжимаются примерно в mib КиБ).
func bomb(t *testing.T, mib int) []byte {
	t.Helper()
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	zero := make([]byte, 1<<20)
	for i := 0; i < mib; i++ {
		if _, err := zw.Write(zero); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	body := append(binary.LittleEndian.AppendUint32(nil, 2), []byte("{}")...)
	return append(body, gz.Bytes()...)
}

func TestPackUnpackRoundTrip(t *testing.T) {
	summary := []byte(`{"groups":2}`)
	snap := bytes.Repeat([]byte("SQLite format 3\x00"), 1000)
	body, err := Pack(summary, bytes.NewReader(snap))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	gotSum, err := Unpack(body, &out)
	if err != nil {
		t.Fatalf("Unpack: %v", err)
	}
	if !bytes.Equal(gotSum, summary) || !bytes.Equal(out.Bytes(), snap) {
		t.Fatal("данные не совпали")
	}
}

func TestUnpackRejectsMalformed(t *testing.T) {
	good, _ := Pack([]byte("{}"), bytes.NewReader([]byte("snapshot")))
	notGzip := append(binary.LittleEndian.AppendUint32(nil, 2), []byte("{}not gzip")...)
	cases := map[string][]byte{
		"пусто":               nil,
		"короче префикса":     {1, 2},
		"длина сводки больше": append(binary.LittleEndian.AppendUint32(nil, 1<<30), 1, 2, 3),
		"не gzip":             notGzip,
		"обрезан gzip":        good[:len(good)-4],
		"мусор после gzip":    append(bytes.Clone(good), 0, 0, 0),
	}
	for name, body := range cases {
		if _, err := Unpack(body, &bytes.Buffer{}); !errors.Is(err, ErrCorrupt) {
			t.Errorf("%s: err = %v, want ErrCorrupt", name, err)
		}
	}
}

func TestPackRejectsHugeSummary(t *testing.T) {
	if _, err := Pack(make([]byte, MaxSummaryBytes+1), bytes.NewReader(nil)); err == nil {
		t.Fatal("сводка больше предела принята")
	}
}

// TestGzipBombCapped — S: маленький gzip, распаковывающийся в десятки МиБ,
// режется пределом: писателю не уходит ни байта сверх предела.
func TestGzipBombCapped(t *testing.T) {
	body := bomb(t, 64)
	if len(body) > 1<<20 {
		t.Fatalf("бомба не сжалась: %d байт", len(body))
	}
	w := &countWriter{}
	if _, err := unpackLimit(body, w, 1<<20); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("предел 1 МиБ: err = %v, want ErrCorrupt", err)
	}
	if w.total > 1<<20 {
		t.Errorf("писателю ушло %d байт при пределе 1 МиБ", w.total)
	}
	// ровно по границе: 64 МиБ проходят при пределе 64 МиБ и не проходят при пределе на байт меньше
	w = &countWriter{}
	if _, err := unpackLimit(body, w, 64<<20); err != nil || w.total != 64<<20 {
		t.Fatalf("предел 64 МиБ: %d байт, %v", w.total, err)
	}
	w = &countWriter{}
	if _, err := unpackLimit(body, w, 64<<20-1); !errors.Is(err, ErrCorrupt) || w.total > 64<<20-1 {
		t.Fatalf("предел 64 МиБ-1: %d байт, %v", w.total, err)
	}
}

// TestUnpackStreams — S: распаковка идёт потоком (малыми кусками), а не целиком
// в память: 128 МиБ распаковываются почти без выделений.
func TestUnpackStreams(t *testing.T) {
	body := bomb(t, 128)
	w := &countWriter{}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	if _, err := Unpack(body, w); err != nil {
		t.Fatal(err)
	}
	runtime.ReadMemStats(&after)
	if w.total != 128<<20 {
		t.Fatalf("распаковано %d байт", w.total)
	}
	if w.maxChunk > 1<<20 {
		t.Errorf("писатель получил кусок в %d байт: распаковка не потоковая", w.maxChunk)
	}
	if grown := after.TotalAlloc - before.TotalAlloc; grown > 16<<20 {
		t.Errorf("за распаковку выделено %d МиБ памяти (ждём единицы МиБ)", grown>>20)
	}
}

func TestUnpackHardCapIs128MiB(t *testing.T) {
	if MaxSnapshotBytes != 128<<20 {
		t.Fatalf("MaxSnapshotBytes = %d", MaxSnapshotBytes)
	}
	w := &countWriter{}
	if _, err := Unpack(bomb(t, 129), w); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("129 МиБ приняты: %v", err)
	}
	if w.total > MaxSnapshotBytes {
		t.Errorf("писателю ушло %d байт", w.total)
	}
}

// failWriter — писатель, у которого кончилось место.
type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("диск полон") }

// TestUnpackWriteErrorIsNotCorruption — ошибка записи (диск) не выдаётся за
// повреждение файла копии.
func TestUnpackWriteErrorIsNotCorruption(t *testing.T) {
	body, _ := Pack([]byte("{}"), bytes.NewReader([]byte("snapshot")))
	_, err := Unpack(body, failWriter{})
	if err == nil || errors.Is(err, ErrCorrupt) {
		t.Fatalf("err = %v, ждали ошибку записи без ErrCorrupt", err)
	}
}

// TestPackCapsSnapshot — S: снимок больше предела не упаковывается: экспорт
// падает сразу, а не создаёт копию, которую потом нельзя восстановить.
func TestPackCapsSnapshot(t *testing.T) {
	snap := bytes.Repeat([]byte{7}, 1<<20)
	if _, err := packLimit(nil, bytes.NewReader(snap), 1<<20); err != nil {
		t.Fatalf("ровно по пределу: %v", err)
	}
	if _, err := packLimit(nil, bytes.NewReader(append(snap, 1)), 1<<20); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("на байт больше предела: err = %v, want ErrTooLarge", err)
	}
	// Pack и Unpack согласованы: всё, что упаковано, распаковывается
	body, err := packLimit([]byte("{}"), bytes.NewReader(snap), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := unpackLimit(body, &bytes.Buffer{}, 1<<20); err != nil {
		t.Fatalf("упакованное по пределу не распаковывается: %v", err)
	}
}

// TestPackCapsBody — несжимаемый снимок, после gzip не помещающийся в 64 МиБ,
// даёт ErrTooLarge, а не бесконтрольный рост буфера.
func TestPackCapsBody(t *testing.T) {
	noise := make([]byte, MaxFileBytes+1<<20)
	if _, err := rand.Read(noise); err != nil {
		t.Fatal(err)
	}
	if _, err := Pack([]byte("{}"), bytes.NewReader(noise)); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
}

// FuzzUnpack — Unpack на произвольных байтах не паникует, ошибка — только
// ErrCorrupt, а писателю не уходит больше предела.
func FuzzUnpack(f *testing.F) {
	good, _ := Pack([]byte(`{"a":1}`), bytes.NewReader(bytes.Repeat([]byte("snapshot "), 100)))
	f.Add(good)
	f.Add(good[:len(good)/2])
	f.Add(append(bytes.Clone(good), 1, 2, 3))
	f.Add([]byte{0xff, 0xff, 0xff, 0xff, 1, 2, 3})
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		w := &countWriter{}
		summary, err := unpackLimit(data, w, 1<<16)
		if w.total > 1<<16 {
			t.Fatalf("писателю ушло %d байт при пределе %d", w.total, 1<<16)
		}
		if err != nil {
			if !errors.Is(err, ErrCorrupt) {
				t.Fatalf("ошибка не ErrCorrupt: %v", err)
			}
			return
		}
		if len(summary) > MaxSummaryBytes {
			t.Fatalf("сводка %d байт больше предела", len(summary))
		}
	})
}
