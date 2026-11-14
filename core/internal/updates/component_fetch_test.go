// SPDX-License-Identifier: GPL-3.0-only

package updates

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

func TestFetchBlobExactLengthAndProgress(t *testing.T) {
	data := bytes.Repeat([]byte("geo"), 50_000)
	var seen []int64
	got, err := FetchBlob(context.Background(), []ArtifactSource{&memArt{data: data}},
		BlobSpec{Path: "geo/set.zip", Sha256: packSha(data), Length: int64(len(data))},
		func(done, total int64) {
			if total != int64(len(data)) {
				t.Errorf("total=%d", total)
			}
			seen = append(seen, done)
		})
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("FetchBlob: %v", err)
	}
	if len(seen) < 2 || seen[len(seen)-1] != int64(len(data)) {
		t.Fatalf("прогресс: %v", seen)
	}
	for i := 1; i < len(seen); i++ {
		if seen[i] < seen[i-1] {
			t.Fatalf("прогресс убывает: %v", seen)
		}
	}
}

// R: плохое зеркало (подмена, обрыв, лишние байты) пропускается, берётся следующее.
func TestFetchBlobFallsBackToNextMirror(t *testing.T) {
	good := []byte("good-content")
	bad := []byte("evil-content")
	spec := BlobSpec{Path: "p", Sha256: packSha(good), Length: int64(len(good))}
	for name, first := range map[string]ArtifactSource{
		"tampered": &memArt{data: bad},
		"extra":    &memArt{data: good, extra: 5},
		"broken":   &memArt{data: good, breakAt: 3},
		"down":     &memArt{openErr: errors.New("нет сети")},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := FetchBlob(context.Background(), []ArtifactSource{first, &memArt{data: good}}, spec, nil)
			if err != nil || !bytes.Equal(got, good) {
				t.Fatalf("%s: %v", name, err)
			}
		})
	}
}

func TestFetchBlobIntegrityErrorsWin(t *testing.T) {
	good := []byte("good-content")
	spec := BlobSpec{Path: "p", Sha256: packSha(good), Length: int64(len(good))}
	_, err := FetchBlob(context.Background(), []ArtifactSource{&memArt{data: []byte("evil-content")}, &memArt{openErr: errors.New("нет сети")}}, spec, nil)
	if !errors.Is(err, ErrHashMismatch) {
		t.Fatalf("подмена важнее сетевой ошибки: %v", err)
	}
	_, err = FetchBlob(context.Background(), []ArtifactSource{&memArt{data: good, extra: 3}}, spec, nil)
	if !errors.Is(err, ErrLengthMismatch) {
		t.Fatalf("лишние байты: %v", err)
	}
	_, err = FetchBlob(context.Background(), []ArtifactSource{&memArt{data: good[:4]}}, spec, nil)
	if !errors.Is(err, ErrLengthMismatch) {
		t.Fatalf("усечённое тело: %v", err)
	}
}

func TestFetchBlobMaxWithoutLength(t *testing.T) {
	data := bytes.Repeat([]byte("x"), 100)
	spec := BlobSpec{Path: "p", Sha256: packSha(data), Max: 99}
	if _, err := FetchBlob(context.Background(), []ArtifactSource{&memArt{data: data}}, spec, nil); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("больше предела: %v", err)
	}
	spec.Max = 100
	if got, err := FetchBlob(context.Background(), []ArtifactSource{&memArt{data: data}}, spec, nil); err != nil || len(got) != 100 {
		t.Fatalf("ровно предел: %v", err)
	}
}

func TestFetchBlobNoMirrorsAndCancel(t *testing.T) {
	spec := BlobSpec{Path: "p", Sha256: packSha(nil), Max: 10}
	if _, err := FetchBlob(context.Background(), nil, spec, nil); !errors.Is(err, ErrNetwork) {
		t.Fatalf("нет зеркал: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := FetchBlob(ctx, []ArtifactSource{&memArt{data: []byte("x")}}, spec, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("отмена: %v", err)
	}
}

func TestFetchBlobRejectsServerOffset(t *testing.T) {
	data := []byte("abcdef")
	spec := BlobSpec{Path: "p", Sha256: packSha(data), Length: 6}
	if _, err := FetchBlob(context.Background(), []ArtifactSource{&offsetLiar{data: data}}, spec, nil); err == nil {
		t.Fatal("сервер, отдающий не с начала, отвергается")
	}
}
