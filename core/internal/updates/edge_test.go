// SPDX-License-Identifier: GPL-3.0-only

// edge_test.go — краевые случаи: манифест против пина timestamp, ключи и роли
// корня, загрузка с неожиданными ответами источников, HTTP-статусы.
package updates

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func publishedEnv(t *testing.T) (*env, relOpts) {
	t.Helper()
	e := newEnv(t, "0.5.0")
	o := relOpts{version: "0.6.0", serial: 3, tsVersion: 4}
	e.def.publish(t, e.src, o)
	return e, o
}

func TestFetchManifestPinAndClaims(t *testing.T) {
	e, _ := publishedEnv(t)
	ctx := context.Background()
	res, err := e.check(t)
	if err != nil {
		t.Fatal(err)
	}
	q := ManifestQuery{Channel: ChannelStable, Product: "lavray-windows", Now: time.Now()}
	srcs := []Source{e.src}
	// канал, которого нет в timestamp: ничего не опубликовано (не ошибка)
	if mf, s, err := FetchManifest(ctx, e.cl.Trust, srcs, res.Timestamp, ManifestQuery{Channel: ChannelBeta, Now: time.Now()}); mf != nil || s != nil || err != nil {
		t.Fatalf("нет записи канала: %v %v %v", mf, s, err)
	}
	pin := res.Timestamp.Meta["manifest-stable.json"]
	for name, mutate := range map[string]func(*Timestamp){
		"sha256": func(ts *Timestamp) {
			p := pin
			p.Sha256 = hex.EncodeToString(make([]byte, 32))
			ts.Meta["manifest-stable.json"] = p
		},
		"длина":   func(ts *Timestamp) { p := pin; p.Length++; ts.Meta["manifest-stable.json"] = p },
		"serial":  func(ts *Timestamp) { p := pin; p.Serial++; ts.Meta["manifest-stable.json"] = p },
		"подпись": nil,
	} {
		ts := &Timestamp{Meta: map[string]TimestampEntry{"manifest-stable.json": pin}}
		if mutate != nil {
			mutate(ts)
		} else {
			// манифест подписан чужим ключом
			e.src.put("manifest-stable.json", envelope(t, []byte(`{"type":"manifest"}`), map[string]signer{"r1": newSigner(t)}))
		}
		if _, _, err := FetchManifest(ctx, e.cl.Trust, srcs, ts, q); err == nil {
			t.Errorf("%s: расхождение с пином timestamp принято", name)
		}
	}
	// все источники молчат
	down := newMem()
	down.down = true
	ts := &Timestamp{Meta: map[string]TimestampEntry{"manifest-stable.json": pin}}
	if _, _, err := FetchManifest(ctx, e.cl.Trust, []Source{down}, ts, q); !errors.Is(err, ErrNetwork) {
		t.Fatalf("источник недоступен: %v", err)
	}
	// просроченный манифест
	late := q
	late.Now = time.Now().Add(60 * 24 * time.Hour)
	e2, _ := publishedEnv(t)
	res2, _ := e2.check(t)
	if _, _, err := FetchManifest(ctx, e2.cl.Trust, []Source{e2.src}, res2.Timestamp, late); !errors.Is(err, ErrExpired) {
		t.Fatalf("манифест после expires: %v", err)
	}
}

func TestRootKeyValidation(t *testing.T) {
	d := newRootDef(t, 1, farFuture, "k1")
	var doc map[string]any
	_ = json.Unmarshal(d.signed(t), &doc)
	clone := func() map[string]any {
		b, _ := json.Marshal(doc)
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		return m
	}
	cases := map[string]func(m map[string]any){
		"ed25519 короткий": func(m map[string]any) {
			m["keys"].(map[string]any)["t1"] = map[string]any{"alg": "ed25519", "pub": "AAAA"}
		},
		"minisign не base64": func(m map[string]any) {
			m["keys"].(map[string]any)["a1"] = map[string]any{"alg": "minisign", "pub": "%%%"}
		},
		"minisign без Ed": func(m map[string]any) {
			m["keys"].(map[string]any)["a1"] = map[string]any{"alg": "minisign", "pub": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"}
		},
		"алгоритм": func(m map[string]any) { m["keys"].(map[string]any)["t1"] = map[string]any{"alg": "rsa", "pub": "x"} },
		"порог 0": func(m map[string]any) {
			m["roles"].(map[string]any)["timestamp"] = map[string]any{"keyids": []string{"t1"}, "threshold": 0}
		},
		"порог больше ключей": func(m map[string]any) {
			m["roles"].(map[string]any)["timestamp"] = map[string]any{"keyids": []string{"t1"}, "threshold": 2}
		},
		"пустой keyid": func(m map[string]any) {
			m["roles"].(map[string]any)["timestamp"] = map[string]any{"keyids": []string{""}, "threshold": 1}
		},
		"дубликат": func(m map[string]any) {
			m["roles"].(map[string]any)["manifest"] = map[string]any{"keyids": []string{"r1", "r1"}, "threshold": 1}
		},
		"нет ключа": func(m map[string]any) {
			m["roles"].(map[string]any)["manifest"] = map[string]any{"keyids": []string{"zz"}, "threshold": 1}
		},
		"нет роли": func(m map[string]any) { delete(m["roles"].(map[string]any), "sources") },
	}
	for name, mutate := range cases {
		m := clone()
		mutate(m)
		raw, _ := json.Marshal(m)
		if _, err := ParseRoot(raw); err == nil {
			t.Errorf("%s: корень принят", name)
		}
	}
}

func TestArtifactSignatureAgainstRoot(t *testing.T) {
	d := newRootDef(t, 1, farFuture, "k1")
	root := mustTrust(t, d.selfSigned(t)).Root()
	data := []byte("setup")
	claims := ArtifactClaims{"p", "windows-x64", "stable", "1.0.0", 1}
	sig, _ := ParseMinisig(d.art.sign(data, claims.String()))
	digest := DigestOf(data)
	if err := root.VerifyArtifactSignature(sig, digest, claims); err != nil {
		t.Fatal(err)
	}
	other := newMinisignKey(t)
	foreign, _ := ParseMinisig(other.sign(data, claims.String()))
	if err := root.VerifyArtifactSignature(foreign, digest, claims); !errors.Is(err, ErrMinisigKeyID) {
		t.Fatalf("подпись неизвестного ключа: %v", err)
	}
	claims.Serial = 2
	if err := root.VerifyArtifactSignature(sig, digest, claims); !errors.Is(err, ErrMinisigTrustedComment) {
		t.Fatalf("чужие заявления: %v", err)
	}
	if err := d.art.key.VerifyDigest(sig, DigestOf([]byte("другое"))); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("чужой дайджест: %v", err)
	}
	if _, err := ParseClaims("a;b;c;d;x"); err == nil {
		t.Fatal("serial не число")
	}
	if _, err := ParseClaims("a;b;c"); err == nil {
		t.Fatal("мало полей")
	}
}

func TestResolveTrustEdges(t *testing.T) {
	d := newRootDef(t, 1, farFuture, "k1")
	if _, err := ResolveTrust([]byte("мусор"), nil); err == nil || errors.Is(err, ErrNotConfigured) {
		t.Fatalf("испорченный вшитый корень: %v", err)
	}
	if tr, err := ResolveTrust(d.selfSigned(t), nil); err != nil || tr.Root().Version != 1 {
		t.Fatalf("без хранилища: %v", err)
	}
	if err := PersistRoot(nil, mustTrust(t, d.selfSigned(t))); err != nil {
		t.Fatal(err)
	}
	if err := PersistRoot(&fakeRootStore{}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestDownloaderUnexpectedSources(t *testing.T) {
	k := newMinisignKey(t)
	data := testData(40_000)
	asset, verify := signedAsset(t, k, data)
	// существующий итоговый файл с верным sha, но неверной подписью: удаляется и качается заново
	d, dir := newDL(t, func([DigestSize]byte) error { return ErrBadSignature }, &memArt{data: data})
	final, _ := StagedPath(dir, asset.Sha256, "lavray-setup.exe")
	_ = os.WriteFile(final, data, 0o600)
	if _, err := d.Fetch(context.Background(), asset); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("плохая подпись у готового файла: %v", err)
	}
	if _, err := os.Stat(final); err == nil {
		t.Fatal("файл с плохой подписью остался в staging")
	}
	// .part длиннее актива удаляется, загрузка идёт с нуля
	d2, dir2 := newDL(t, verify, &memArt{data: data, ranges: true})
	final2, _ := StagedPath(dir2, asset.Sha256, "lavray-setup.exe")
	_ = os.WriteFile(final2+PartSuffix, append(append([]byte(nil), data...), 1, 2, 3), 0o600)
	if _, err := d2.Fetch(context.Background(), asset); err != nil {
		t.Fatalf("раздутый .part: %v", err)
	}
	// сервер отдал смещение, которого не просили
	d3, dir3 := newDL(t, verify, &offsetLiar{data: data})
	final3, _ := StagedPath(dir3, asset.Sha256, "lavray-setup.exe")
	_ = os.WriteFile(final3+PartSuffix, data[:10_000], 0o600)
	if _, err := d3.Fetch(context.Background(), asset); !errors.Is(err, ErrLengthMismatch) {
		t.Fatalf("неожиданное смещение: %v", err)
	}
}

type offsetLiar struct{ data []byte }

func (o *offsetLiar) Open(_ context.Context, _ string, off int64) (*ArtifactStream, error) {
	return &ArtifactStream{Body: io.NopCloser(bytes.NewReader(o.data[off+5:])), Start: off + 5, Total: int64(len(o.data))}, nil
}

func TestHTTPSourceStatuses(t *testing.T) {
	data := []byte("0123456789")
	var rangeHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/500":
			http.Error(w, "boom", http.StatusInternalServerError)
		case "/416":
			rangeHeader = r.Header.Get("Range")
			if rangeHeader != "" {
				w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
				return
			}
			_, _ = w.Write(data)
		case "/badrange":
			w.Header().Set("Content-Range", "garbage")
			w.WriteHeader(http.StatusPartialContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	s := &HTTPSource{Base: srv.URL}
	ctx := context.Background()
	if _, err := s.Fetch(ctx, "500"); err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("500: %v", err)
	}
	if _, err := s.Open(ctx, "404", 0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Open 404: %v", err)
	}
	if _, err := s.Open(ctx, "500", 0); err == nil {
		t.Fatal("Open 500")
	}
	if _, err := s.Open(ctx, "badrange", 3); err == nil {
		t.Fatal("кривой Content-Range")
	}
	st, err := s.Open(ctx, "416", 5) // 416 → повтор без Range
	if err != nil || st.Start != 0 {
		t.Fatalf("416: %v %+v", err, st)
	}
	st.Body.Close()
	if big := (&HTTPSource{Base: srv.URL, MaxBytes: 3}); true {
		if _, err := big.Fetch(ctx, "416"); !errors.Is(err, ErrTooLarge) {
			t.Fatalf("Content-Length больше лимита: %v", err)
		}
	}
}

func TestClientDownloadEdges(t *testing.T) {
	e, _ := publishedEnv(t)
	res, err := e.check(t)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (&Result{}).ArtifactVerifierFor(Asset{}); !errors.Is(err, ErrNoCheck) {
		t.Fatalf("результат без манифеста: %v", err)
	}
	if _, err := res.ArtifactVerifierFor(Asset{Minisig: "мусор"}); !errors.Is(err, ErrMinisigFormat) {
		t.Fatalf("мусорная подпись: %v", err)
	}
	res.AssetKey = "linux-x64"
	if _, err := e.cl.Download(context.Background(), res, t.TempDir(), nil); !errors.Is(err, ErrNoCheck) {
		t.Fatalf("нет актива платформы: %v", err)
	}
	if CurrentAssetKey() == "unknown" {
		t.Fatal("недостижимо")
	}
}

func TestTimestampFetchEdges(t *testing.T) {
	e, _ := publishedEnv(t)
	pol := TimestampPolicy{Now: time.Now()}
	if _, err := FetchTimestamp(context.Background(), e.cl.Trust, nil, pol); !errors.Is(err, ErrNetwork) {
		t.Fatalf("нет источников: %v", err)
	}
	// ошибка валидации важнее сетевой: один источник лежит, другой отдаёт чужую подпись
	down, forged := newMem(), newMem()
	down.down = true
	forged.put("timestamp.json", envelope(t, []byte(`{"type":"timestamp"}`), map[string]signer{"t1": newSigner(t)}))
	if _, err := FetchTimestamp(context.Background(), e.cl.Trust, []Source{down, forged}, pol); !errors.Is(err, ErrInsufficientSignatures) {
		t.Fatalf("приоритет ошибки валидации: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	slow := newMem()
	slow.delay = time.Minute
	if _, err := FetchTimestamp(ctx, e.cl.Trust, []Source{slow}, pol); !errors.Is(err, ErrNetwork) {
		t.Fatalf("отменённый контекст: %v", err)
	}
}

func TestCtxReaderAndPreferIntegrity(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	r := &ctxReader{ctx: ctx, r: bytes.NewReader([]byte("abc"))}
	buf := make([]byte, 2)
	if n, err := r.Read(buf); err != nil || n != 2 {
		t.Fatalf("чтение до отмены: %d %v", n, err)
	}
	cancel()
	if _, err := r.Read(buf); !errors.Is(err, context.Canceled) {
		t.Fatalf("чтение после отмены: %v", err)
	}
	if got := preferIntegrity(ErrHashMismatch, ErrNetwork); !errors.Is(got, ErrHashMismatch) {
		t.Fatal("ошибка целостности не должна затираться сетевой")
	}
	if got := preferIntegrity(ErrNetwork, ErrHashMismatch); !errors.Is(got, ErrHashMismatch) {
		t.Fatal("сетевая заменяется ошибкой целостности")
	}
	sum := sha256.Sum256(nil)
	if hex.EncodeToString(sum[:]) == "" {
		t.Fatal("недостижимо")
	}
}
