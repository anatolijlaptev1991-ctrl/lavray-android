// SPDX-License-Identifier: GPL-3.0-only

package updates

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type localFix struct {
	t     *testing.T
	def   rootDef
	trust *Trust
	dir   string
}

func newLocalFix(t *testing.T) *localFix {
	t.Helper()
	d := newRootDef(t, 1, farFuture, "k1")
	return &localFix{t: t, def: d, trust: mustTrust(t, d.selfSigned(t)), dir: t.TempDir()}
}

// file кладёт установщик и подпись (key — чем подписано; nil — подписи нет).
func (f *localFix) file(name string, data []byte, key *testMinisignKey, claims ArtifactClaims) (installer, sig string) {
	installer = filepath.Join(f.dir, name)
	if err := os.WriteFile(installer, data, 0o600); err != nil {
		f.t.Fatal(err)
	}
	sig = installer + MinisigSuffix
	if key != nil {
		if err := os.WriteFile(sig, key.sign(data, claims.String()), 0o600); err != nil {
			f.t.Fatal(err)
		}
	}
	return installer, sig
}

var localPolicy = LocalPolicy{Product: "lavray-windows", Platform: "windows-x64", AppVersion: "0.5.0"}

func localClaims(version string) ArtifactClaims {
	return ArtifactClaims{"lavray-windows", "windows-x64", "stable", version, 3}
}

func TestVerifyInstallerFile(t *testing.T) {
	f := newLocalFix(t)
	data := []byte("installer-0.6.0")
	inst, sig := f.file("a.exe", data, f.def.art, localClaims("0.6.0"))
	res, err := VerifyInstallerFile(f.trust, inst, sig, localPolicy)
	if err != nil || res.Claims.Version != "0.6.0" || len(res.Sha256) != 64 {
		t.Fatalf("валидный файл: %+v %v", res, err)
	}
}

// S: файл без подписи, с подписью чужого ключа, с подменённым содержимым,
// чужой версией в заявлениях не проходит.
func TestVerifyInstallerFileRejects(t *testing.T) {
	f := newLocalFix(t)
	data := []byte("installer-0.6.0")
	foreign := newMinisignKey(t)
	cases := map[string]func() (string, string){
		"без подписи": func() (string, string) { return f.file("u.exe", data, nil, ArtifactClaims{}) },
		"чужой ключ":  func() (string, string) { return f.file("w.exe", data, &foreign, localClaims("0.6.0")) },
		"другие байты": func() (string, string) {
			i, s := f.file("t.exe", data, f.def.art, localClaims("0.6.0"))
			_ = os.WriteFile(i, []byte("installer-0.6.X"), 0o600)
			return i, s
		},
		"подпись большого размера": func() (string, string) {
			i, s := f.file("b.exe", data, nil, ArtifactClaims{})
			_ = os.WriteFile(s, bytes.Repeat([]byte("a"), MaxSignatureBytes+1), 0o600)
			return i, s
		},
		"мусорная подпись": func() (string, string) {
			i, s := f.file("g.exe", data, nil, ArtifactClaims{})
			_ = os.WriteFile(s, []byte("не подпись"), 0o600)
			return i, s
		},
	}
	for name, mk := range cases {
		inst, sig := mk()
		if _, err := VerifyInstallerFile(f.trust, inst, sig, localPolicy); err == nil {
			t.Errorf("%s: файл принят", name)
		}
	}
	if _, err := VerifyInstallerFile(f.trust, filepath.Join(f.dir, "нет.exe"), "x.minisig", localPolicy); err == nil {
		t.Error("несуществующий файл принят")
	}
}

func TestVerifyInstallerPolicy(t *testing.T) {
	f := newLocalFix(t)
	data := []byte("installer")
	mk := func(name string, c ArtifactClaims) (string, string) { return f.file(name, data, f.def.art, c) }

	older, olderSig := mk("old.exe", localClaims("0.4.0"))
	if _, err := VerifyInstallerFile(f.trust, older, olderSig, localPolicy); err == nil {
		t.Fatal("старая версия без AllowOlder принята")
	}
	pol := localPolicy
	pol.AllowOlder = true
	if res, err := VerifyInstallerFile(f.trust, older, olderSig, pol); err != nil || res.Claims.Version != "0.4.0" {
		t.Fatalf("откат разрешает старую подписанную версию: %v", err)
	}
	same, sameSig := mk("same.exe", localClaims("0.5.0"))
	if _, err := VerifyInstallerFile(f.trust, same, sameSig, localPolicy); err == nil {
		t.Fatal("та же версия — не обновление")
	}
	other := localClaims("0.6.0")
	other.Product = "lavray-linux"
	wrongP, wrongPSig := mk("p.exe", other)
	if _, err := VerifyInstallerFile(f.trust, wrongP, wrongPSig, pol); !errors.Is(err, ErrWrongProduct) {
		t.Fatalf("чужой продукт: %v", err)
	}
	other = localClaims("0.6.0")
	other.Platform = "linux-x64"
	wrongPl, wrongPlSig := mk("pl.exe", other)
	if _, err := VerifyInstallerFile(f.trust, wrongPl, wrongPlSig, pol); err == nil {
		t.Fatal("чужая платформа принята")
	}
}

// Downloader кладёт рядом подпись: по ней оболочка перепроверит файл перед установкой.
func TestDownloadWritesSignatureNextToInstaller(t *testing.T) {
	k := newMinisignKey(t)
	data := testData(30_000)
	asset, verify := signedAsset(t, k, data)
	d, dir := newDL(t, verify, &memArt{data: data})
	d.Signature = asset.Minisig
	p, err := d.Fetch(context.Background(), asset)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(p + MinisigSuffix)
	if err != nil || string(got) != asset.Minisig {
		t.Fatalf("подпись рядом с установщиком: %v", err)
	}
	// потерянная подпись восстанавливается при повторном обращении, файл не качается заново
	_ = os.Remove(p + MinisigSuffix)
	src := d.Sources[0].(*memArt)
	calls := src.calls
	if _, err := d.Fetch(context.Background(), asset); err != nil || src.calls != calls {
		t.Fatalf("повтор: %v calls %d→%d", err, calls, src.calls)
	}
	if got, _ := os.ReadFile(p + MinisigSuffix); string(got) != asset.Minisig {
		t.Fatal("подпись не восстановлена")
	}
	// очистка staging не трогает подпись оставляемого установщика
	_ = os.Chtimes(p+MinisigSuffix, time.Now().Add(-48*time.Hour), time.Now().Add(-48*time.Hour))
	if _, err := CleanStaging(dir, p, 0, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p + MinisigSuffix); err != nil {
		t.Fatal("очистка удалила подпись установщика, который остаётся")
	}
}

// X: источник, который перестал слать байты, не держит загрузку вечно.
func TestHTTPSourceStallTimeout(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1000")
		_, _ = w.Write([]byte("x"))
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)
	s := &HTTPSource{Base: srv.URL, StallTimeout: 200 * time.Millisecond}
	start := time.Now()
	if _, err := s.Fetch(context.Background(), "doc"); err == nil || time.Since(start) > 3*time.Second {
		t.Fatalf("Fetch: зависшее тело должно обрываться по таймауту: %v за %v", err, time.Since(start))
	}
	st, err := s.Open(context.Background(), "a.exe", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Body.Close()
	start = time.Now()
	buf := make([]byte, 100)
	var rerr error
	for rerr == nil {
		_, rerr = st.Body.Read(buf)
	}
	if time.Since(start) > 3*time.Second || strings.Contains(rerr.Error(), "EOF") && time.Since(start) < 100*time.Millisecond {
		t.Fatalf("Read: %v за %v", rerr, time.Since(start))
	}
}

// Часы клиента могут отставать до суток: подписанный «сейчас + 7 суток» timestamp не отвергается.
func TestTimestampClockSkewTolerance(t *testing.T) {
	e, _ := publishedEnv(t)
	for name, behind := range map[string]time.Duration{"на 23 часа": 23 * time.Hour, "на 26 часов": 26 * time.Hour} {
		ctx := context.Background()
		pol := TimestampPolicy{Now: time.Now().Add(-behind)}
		_, err := FetchTimestamp(ctx, e.cl.Trust, []Source{e.src}, pol)
		wantErr := behind > 24*time.Hour
		if wantErr && !errors.Is(err, ErrTimestampHorizon) {
			t.Errorf("часы отстают %s: ожидалась ErrTimestampHorizon, получено %v", name, err)
		}
		if !wantErr && err != nil {
			t.Errorf("часы отстают %s: допуск суток, получено %v", name, err)
		}
	}
}
