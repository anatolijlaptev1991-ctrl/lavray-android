// SPDX-License-Identifier: GPL-3.0-only

package updates

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// memArt — ArtifactSource в памяти с управляемыми сбоями.
type memArt struct {
	data     []byte
	ranges   bool  // понимает Range
	breakAt  int   // >0: после стольких байт тело обрывается ошибкой сети
	stallAt  int   // >0: после стольких байт тело зависает до отмены контекста
	extra    int   // добавить столько лишних байт в конец
	openErr  error // Open возвращает ошибку
	offsets  []int64
	calls    int
	progress chan struct{}
}

func (m *memArt) Open(ctx context.Context, name string, off int64) (*ArtifactStream, error) {
	m.calls++
	m.offsets = append(m.offsets, off)
	if m.openErr != nil {
		return nil, m.openErr
	}
	body := append(append([]byte(nil), m.data...), bytes.Repeat([]byte("X"), m.extra)...)
	start := int64(0)
	if m.ranges {
		start = off
		body = body[off:]
	}
	var r io.Reader = bytes.NewReader(body)
	switch {
	case m.breakAt > 0:
		r = io.MultiReader(io.LimitReader(r, int64(m.breakAt)), errReader{io.ErrUnexpectedEOF})
	case m.stallAt > 0:
		r = io.MultiReader(io.LimitReader(r, int64(m.stallAt)), &stallReader{ctx: ctx, hit: m.progress})
	}
	return &ArtifactStream{Body: io.NopCloser(r), Start: start, Total: int64(len(m.data))}, nil
}

type errReader struct{ err error }

func (e errReader) Read([]byte) (int, error) { return 0, e.err }

type stallReader struct {
	ctx context.Context
	hit chan struct{}
}

func (s *stallReader) Read([]byte) (int, error) {
	if s.hit != nil {
		close(s.hit)
		s.hit = nil
	}
	<-s.ctx.Done()
	return 0, s.ctx.Err()
}

// signedAsset — актив с честной подписью minisign и проверяющей функцией.
func signedAsset(t *testing.T, k testMinisignKey, data []byte) (Asset, ArtifactVerifier) {
	t.Helper()
	sum := sha256.Sum256(data)
	claims := ArtifactClaims{"lavray-windows", "windows-x64", "stable", "1.0.0", 7}
	text := k.sign(data, claims.String())
	sig, err := ParseMinisig(text)
	if err != nil {
		t.Fatal(err)
	}
	asset := Asset{Path: "v1/lavray-setup.exe", Sha256: hex.EncodeToString(sum[:]), Length: int64(len(data)), Minisig: string(text)}
	verify := func(d [DigestSize]byte) error {
		if err := k.key.VerifyDigest(sig, d); err != nil {
			return err
		}
		return sig.CheckClaims(claims)
	}
	return asset, verify
}

func newDL(t *testing.T, verify ArtifactVerifier, srcs ...ArtifactSource) (*Downloader, string) {
	t.Helper()
	dir := t.TempDir()
	return &Downloader{Sources: srcs, Dir: dir, Verify: verify}, dir
}

func testData(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i*7 + i/251)
	}
	return b
}

func listDir(t *testing.T, dir string) []string {
	t.Helper()
	es, _ := os.ReadDir(dir)
	var out []string
	for _, e := range es {
		out = append(out, e.Name())
	}
	return out
}

func TestDownloadStagesVerifiedFile(t *testing.T) {
	k := newMinisignKey(t)
	data := testData(300_000)
	asset, verify := signedAsset(t, k, data)
	d, dir := newDL(t, verify, &memArt{data: data, ranges: true})
	p, err := d.Fetch(context.Background(), asset)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	if !bytes.Equal(got, data) || filepath.Base(p) != asset.Sha256+"-lavray-setup.exe" {
		t.Fatalf("staging: %s, совпадение=%v", p, bytes.Equal(got, data))
	}
	if names := listDir(t, dir); len(names) != 1 {
		t.Fatalf("после загрузки в staging только итоговый файл: %v", names)
	}
	// повторный вызов переиспользует файл и не ходит в сеть
	src := d.Sources[0].(*memArt)
	calls := src.calls
	if _, err := d.Fetch(context.Background(), asset); err != nil || src.calls != calls {
		t.Fatalf("идемпотентность: calls %d→%d err=%v", calls, src.calls, err)
	}
}

func TestResumeWithRange(t *testing.T) {
	k := newMinisignKey(t)
	data := testData(200_000)
	asset, verify := signedAsset(t, k, data)
	src := &memArt{data: data, ranges: true}
	d, dir := newDL(t, verify, src)
	final, _ := StagedPath(dir, asset.Sha256, "lavray-setup.exe")
	if err := os.WriteFile(final+PartSuffix, data[:80_000], 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := d.Fetch(context.Background(), asset)
	if err != nil {
		t.Fatal(err)
	}
	if src.offsets[0] != 80_000 {
		t.Fatalf("докачка должна начинаться с 80000, запрошено %v", src.offsets)
	}
	if got, _ := os.ReadFile(p); !bytes.Equal(got, data) {
		t.Fatal("после докачки содержимое не совпало")
	}
}

func TestServerIgnoresRangeRestarts(t *testing.T) {
	k := newMinisignKey(t)
	data := testData(120_000)
	asset, verify := signedAsset(t, k, data)
	d, dir := newDL(t, verify, &memArt{data: data, ranges: false})
	final, _ := StagedPath(dir, asset.Sha256, "lavray-setup.exe")
	_ = os.WriteFile(final+PartSuffix, data[:50_000], 0o600)
	p, err := d.Fetch(context.Background(), asset)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(p); !bytes.Equal(got, data) {
		t.Fatal("сервер без Range: файл должен скачаться заново и совпасть")
	}
}

func TestLengthExceededAborts(t *testing.T) {
	k := newMinisignKey(t)
	data := testData(10_000)
	asset, verify := signedAsset(t, k, data)
	d, dir := newDL(t, verify, &memArt{data: data, extra: 5})
	if _, err := d.Fetch(context.Background(), asset); !errors.Is(err, ErrLengthMismatch) {
		t.Fatalf("лишние байты: %v", err)
	}
	if names := listDir(t, dir); len(names) != 0 {
		t.Fatalf("после превышения длины не должно остаться файлов: %v", names)
	}
}

func TestShortStreamKeepsPartAndResumes(t *testing.T) {
	k := newMinisignKey(t)
	data := testData(100_000)
	asset, verify := signedAsset(t, k, data)
	flaky := &memArt{data: data, ranges: true, breakAt: 40_000}
	d, dir := newDL(t, verify, flaky)
	if _, err := d.Fetch(context.Background(), asset); err == nil {
		t.Fatal("обрыв сети должен давать ошибку")
	}
	final, _ := StagedPath(dir, asset.Sha256, "lavray-setup.exe")
	if fileSize(final+PartSuffix) != 40_000 {
		t.Fatalf(".part после обрыва: %d байт, ожидалось 40000", fileSize(final+PartSuffix))
	}
	flaky.breakAt = 0
	p, err := d.Fetch(context.Background(), asset)
	if err != nil || flaky.offsets[len(flaky.offsets)-1] != 40_000 {
		t.Fatalf("докачка после обрыва: %v offsets=%v", err, flaky.offsets)
	}
	if got, _ := os.ReadFile(p); !bytes.Equal(got, data) {
		t.Fatal("содержимое после докачки")
	}
}

func TestShaMismatchDeletesPart(t *testing.T) {
	k := newMinisignKey(t)
	data := testData(50_000)
	asset, verify := signedAsset(t, k, data)
	evil := append([]byte(nil), data...)
	evil[1234] ^= 0xff
	d, dir := newDL(t, verify, &memArt{data: evil})
	if _, err := d.Fetch(context.Background(), asset); !errors.Is(err, ErrHashMismatch) {
		t.Fatalf("подменённый байт: %v", err)
	}
	if names := listDir(t, dir); len(names) != 0 {
		t.Fatalf("после несовпадения sha256 .part удаляется: %v", names)
	}
}

func TestMinisigFailDeletesFile(t *testing.T) {
	k, other := newMinisignKey(t), newMinisignKey(t)
	data := testData(50_000)
	asset, _ := signedAsset(t, k, data)
	// подпись файла выпущена ключом k, а роль artifact знает только ключ other
	sig, _ := ParseMinisig([]byte(asset.Minisig))
	verifyOther := func(d [DigestSize]byte) error { return other.key.VerifyDigest(sig, d) }
	d, dir := newDL(t, verifyOther, &memArt{data: data})
	_, err := d.Fetch(context.Background(), asset)
	if !errors.Is(err, ErrMinisigKeyID) && !errors.Is(err, ErrBadSignature) {
		t.Fatalf("неверная подпись minisign: %v", err)
	}
	if names := listDir(t, dir); len(names) != 0 {
		t.Fatalf("файл без валидной подписи не остаётся в staging: %v", names)
	}
	// без проверяющей функции загрузка вообще не стартует (fail-closed)
	d.Verify = nil
	if _, err := d.Fetch(context.Background(), asset); !errors.Is(err, ErrArtifactRoleMissing) {
		t.Fatalf("без Verify: %v", err)
	}
}

func TestFallbackToNextSource(t *testing.T) {
	k := newMinisignKey(t)
	data := testData(60_000)
	asset, verify := signedAsset(t, k, data)
	bad := &memArt{openErr: errors.New("connection refused")}
	evil := &memArt{data: append([]byte("EVIL"), data[4:]...)}
	good := &memArt{data: data}
	d, _ := newDL(t, verify, bad, evil, good)
	p, err := d.Fetch(context.Background(), asset)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(p); !bytes.Equal(got, data) || bad.calls != 1 || evil.calls != 1 || good.calls != 1 {
		t.Fatalf("переход к следующему источнику: calls %d/%d/%d", bad.calls, evil.calls, good.calls)
	}
	// все источники плохи: возвращается ошибка целостности, а не сетевая
	d2, _ := newDL(t, verify, bad, evil)
	if _, err := d2.Fetch(context.Background(), asset); !errors.Is(err, ErrHashMismatch) {
		t.Fatalf("приоритет ошибки целостности: %v", err)
	}
	d3, _ := newDL(t, verify)
	if _, err := d3.Fetch(context.Background(), asset); !errors.Is(err, ErrNetwork) {
		t.Fatalf("нет источников: %v", err)
	}
}

func TestCancelKeepsPart(t *testing.T) {
	k := newMinisignKey(t)
	data := testData(90_000)
	asset, verify := signedAsset(t, k, data)
	hit := make(chan struct{})
	src := &memArt{data: data, ranges: true, stallAt: 30_000, progress: hit}
	d, dir := newDL(t, verify, src)
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { _, err := d.Fetch(ctx, asset); errc <- err }()
	<-hit
	cancel()
	select {
	case err := <-errc:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("отмена: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Fetch не вернулся после отмены")
	}
	final, _ := StagedPath(dir, asset.Sha256, "lavray-setup.exe")
	if fileSize(final+PartSuffix) != 30_000 {
		t.Fatalf("отмена должна сохранять .part (30000), а там %d", fileSize(final+PartSuffix))
	}
}

func TestProgressMonotonic(t *testing.T) {
	k := newMinisignKey(t)
	data := testData(400_000)
	asset, verify := signedAsset(t, k, data)
	d, _ := newDL(t, verify, &memArt{data: data})
	var seen []int64
	d.Progress = func(done, total int64) {
		if total != asset.Length {
			t.Errorf("total=%d", total)
		}
		seen = append(seen, done)
	}
	if _, err := d.Fetch(context.Background(), asset); err != nil {
		t.Fatal(err)
	}
	for i := 1; i < len(seen); i++ {
		if seen[i] <= seen[i-1] {
			t.Fatalf("прогресс не монотонен: %v", seen)
		}
	}
	if len(seen) == 0 || seen[len(seen)-1] != asset.Length {
		t.Fatalf("прогресс должен закончиться на длине файла: %v", seen)
	}
}

func TestDownloadRejectsOversizedAsset(t *testing.T) {
	k := newMinisignKey(t)
	_, verify := signedAsset(t, k, []byte("x"))
	d, _ := newDL(t, verify, &memArt{})
	for _, n := range []int64{0, -1, MaxArtifactBytes + 1} {
		a := Asset{Path: "a.exe", Sha256: strings.Repeat("a", 64), Length: n}
		if _, err := d.Fetch(context.Background(), a); !errors.Is(err, ErrTooLarge) {
			t.Errorf("длина %d: %v", n, err)
		}
	}
}

func TestStagingRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	sha := strings.Repeat("a", 64)
	for _, name := range []string{"../evil.exe", "a/b.exe", `a\b.exe`, "..", "x..y.exe", "", "a:b.exe", "a b.exe", "-x.exe"} {
		if _, err := StagedName(sha, name); !errors.Is(err, ErrUnsafePath) {
			t.Errorf("имя %q должно отвергаться: %v", name, err)
		}
	}
	for _, bad := range []string{"", "xyz", strings.Repeat("A", 64), strings.Repeat("a", 63)} {
		if _, err := StagedName(bad, "ok.exe"); !errors.Is(err, ErrUnsafePath) {
			t.Errorf("sha %q должен отвергаться: %v", bad, err)
		}
	}
	good, _ := StagedPath(dir, sha, "ok.exe")
	if err := os.WriteFile(good, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := InsideStaging(dir, good); err != nil {
		t.Fatalf("корректный путь: %v", err)
	}
	outside := t.TempDir()
	evil := filepath.Join(outside, sha+"-ok.exe")
	_ = os.WriteFile(evil, []byte("x"), 0o600)
	for name, p := range map[string]string{
		"вне каталога":  evil,
		"через ..":      filepath.Join(dir, "..", filepath.Base(outside), sha+"-ok.exe"),
		"вложенный":     filepath.Join(dir, "sub", sha+"-ok.exe"),
		"чужое имя":     filepath.Join(dir, "evil.exe"),
		"сам каталог":   dir,
		"пустой":        "",
		"нет файла":     filepath.Join(dir, sha+"-none.exe"),
		"каталог-имя":   mkdir(t, filepath.Join(dir, sha+"-d.exe")),
		"подпись чужая": filepath.Join(dir, "x.minisig"),
	} {
		if _, err := InsideStaging(dir, p); err == nil {
			t.Errorf("%s: путь %q принят", name, p)
		}
	}
	link := filepath.Join(dir, sha+"-link.exe")
	if err := os.Symlink(evil, link); err == nil {
		if _, err := InsideStaging(dir, link); err == nil {
			t.Error("симлинк в staging принят")
		}
	} else {
		t.Logf("симлинки недоступны (%v): проверка пропущена", err)
	}
	if _, err := InsideStaging(dir, good+MinisigSuffix); err == nil {
		t.Error("несуществующая подпись принята")
	}
}

func mkdir(t *testing.T, p string) string {
	t.Helper()
	if err := os.Mkdir(p, 0o700); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCleanStaging(t *testing.T) {
	dir := t.TempDir()
	sha := strings.Repeat("b", 64)
	keep := filepath.Join(dir, sha+"-keep.exe")
	old := filepath.Join(dir, sha+"-old.exe")
	part := filepath.Join(dir, sha+"-old2.exe"+PartSuffix)
	fresh := filepath.Join(dir, sha+"-fresh.exe")
	foreign := filepath.Join(dir, "notes.txt")
	for _, p := range []string{keep, old, part, fresh, foreign} {
		_ = os.WriteFile(p, []byte("x"), 0o600)
	}
	past := time.Now().Add(-48 * time.Hour)
	for _, p := range []string{keep, old, part, foreign} {
		_ = os.Chtimes(p, past, past)
	}
	n, err := CleanStaging(dir, keep, 24*time.Hour, time.Now())
	if err != nil || n != 2 {
		t.Fatalf("удалено %d (%v), ожидалось 2 (old и .part)", n, err)
	}
	for _, p := range []string{keep, fresh, foreign} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("не должен был удаляться: %s", p)
		}
	}
	if n, err := CleanStaging(filepath.Join(dir, "нет"), "", 0, time.Now()); err != nil || n != 0 {
		t.Errorf("нет каталога: %d %v", n, err)
	}
}
