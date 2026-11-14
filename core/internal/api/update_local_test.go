// SPDX-License-Identifier: GPL-3.0-only

package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"connectrpc.com/connect"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/updates"
)

// stageLocal кладёт установщик (и подпись рядом) в staging под именем <sha256>-<имя>.
func (f *updFix) stageLocal(t *testing.T, data []byte, claims updates.ArtifactClaims, withSig bool) string {
	t.Helper()
	if err := f.srv.ensureStaging(); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	p := filepath.Join(f.srv.stagingDir(), hex.EncodeToString(sum[:])+"-lavray-setup.exe")
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if withSig {
		sig := updates.SignMinisigPrehashed(f.env.Art, f.env.ArtID, updates.DigestOf(data), claims.String())
		if err := os.WriteFile(p+updates.MinisigSuffix, sig, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

func (f *updFix) verifyLocal(path string) (*lavrayv1.VerifyLocalUpdateResponse, error) {
	r, err := f.cl.VerifyLocalUpdate(context.Background(), connect.NewRequest(&lavrayv1.VerifyLocalUpdateRequest{InstallerPath: path}))
	if err != nil {
		return nil, err
	}
	return r.Msg, nil
}

func claimsFor(f *updFix, version string) updates.ArtifactClaims {
	return updates.ArtifactClaims{Product: f.env.Product, Platform: "windows-x64", Channel: "stable", Version: version, Serial: 4}
}

func TestVerifyLocalAcceptsNewerSignedInstaller(t *testing.T) {
	f := newUpdFix(t, "0.5.0", nil)
	p := f.stageLocal(t, []byte("installer-0.6.0"), claimsFor(f, "0.6.0"), true)
	got, err := f.verifyLocal(p)
	if err != nil || !got.Valid || got.Manifest.Version != "0.6.0" || got.Manifest.Serial != 4 {
		t.Fatalf("валидный установщик из файла: %+v %v", got, err)
	}
}

func TestVerifyLocalOutsideStagingRejected(t *testing.T) {
	f := newUpdFix(t, "0.5.0", nil)
	outside := filepath.Join(t.TempDir(), "evil.exe")
	_ = os.WriteFile(outside, []byte("x"), 0o600)
	for name, p := range map[string]string{
		"вне staging":  outside,
		"через ..":     filepath.Join(f.srv.stagingDir(), "..", "..", "x.exe"),
		"чужое имя":    filepath.Join(f.st.Dir(), "settings.db"),
		"каталог":      f.srv.stagingDir(),
		"относительно": "evil.exe",
	} {
		if _, err := f.verifyLocal(p); connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("%s: ожидался INVALID_ARGUMENT, получено %v", name, err)
		}
	}
	if _, err := f.verifyLocal(""); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("пустой путь: %v", err)
	}
}

func TestVerifyLocalDowngradeRejected(t *testing.T) {
	f := newUpdFix(t, "0.5.0", nil)
	for _, v := range []string{"0.4.0", "0.5.0"} { // старее и равная версия
		p := f.stageLocal(t, []byte("installer-"+v), claimsFor(f, v), true)
		got, err := f.verifyLocal(p)
		if err != nil || got.Valid || len(got.Errors) == 0 {
			t.Fatalf("версия %s не новее установленной: %+v %v", v, got, err)
		}
	}
}

func TestVerifyLocalRejectsBadSignatures(t *testing.T) {
	f := newUpdFix(t, "0.5.0", nil)
	good := []byte("installer-0.6.0")
	cases := map[string]func() string{
		"подменён байт": func() string {
			p := f.stageLocal(t, good, claimsFor(f, "0.6.0"), true)
			// подпись выпущена для good, а в файле другие байты с тем же sha-именем
			_ = os.WriteFile(p, []byte("installer-0.6.X"), 0o600)
			return p
		},
		"чужой продукт": func() string {
			c := claimsFor(f, "0.6.1")
			c.Product = "lavray-linux"
			return f.stageLocal(t, []byte("installer-0.6.1"), c, true)
		},
		"чужая платформа": func() string {
			c := claimsFor(f, "0.6.2")
			c.Platform = "linux-x64"
			return f.stageLocal(t, []byte("installer-0.6.2"), c, true)
		},
		"нет подписи и манифеста": func() string {
			return f.stageLocal(t, []byte("installer-0.6.3"), claimsFor(f, "0.6.3"), false)
		},
	}
	for name, mk := range cases {
		got, err := f.verifyLocal(mk())
		if err != nil || got.Valid || len(got.Errors) == 0 {
			t.Errorf("%s: файл должен быть отвергнут (valid=false): %+v %v", name, got, err)
		}
	}
}

func TestVerifyLocalUsesManifestWhenNoSigFile(t *testing.T) {
	f := newUpdFix(t, "0.5.0", nil)
	data := []byte("installer-0.7.0")
	f.release(updatesRelease("0.7.0", data))
	p := f.stageLocal(t, data, claimsFor(f, "0.7.0"), false)
	got, err := f.verifyLocal(p)
	if err != nil || !got.Valid || got.Manifest.Version != "0.7.0" {
		t.Fatalf("подпись из манифеста онлайн-проверки: %+v %v", got, err)
	}
}

// Файл подписи читается с пределом: огромный .minisig не тянется в память.
func TestVerifyLocalOversizedSignatureRejected(t *testing.T) {
	f := newUpdFix(t, "0.5.0", nil)
	p := f.stageLocal(t, []byte("installer-0.6.0"), claimsFor(f, "0.6.0"), false)
	if err := os.WriteFile(p+updates.MinisigSuffix, bytes.Repeat([]byte("a"), 8192), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := f.verifyLocal(p)
	if err != nil || got.Valid || len(got.Errors) == 0 {
		t.Fatalf("подпись больше 4 КиБ: %+v %v", got, err)
	}
}

// Подпись взята из манифеста онлайн-проверки: после успешной проверки она кладётся
// рядом с установщиком как .minisig, иначе оболочка (verify-artifact) отвергла бы
// файл, который ядро только что приняло.
func TestVerifyLocalPersistsManifestSignatureNextToInstaller(t *testing.T) {
	f := newUpdFix(t, "0.5.0", nil)
	data := []byte("installer-0.7.0")
	f.release(updatesRelease("0.7.0", data))
	p := f.stageLocal(t, data, claimsFor(f, "0.7.0"), false)
	got, err := f.verifyLocal(p)
	if err != nil || !got.Valid {
		t.Fatalf("проверка по манифесту: %+v %v", got, err)
	}
	sig, err := os.ReadFile(p + updates.MinisigSuffix)
	if err != nil {
		t.Fatalf("подпись рядом с установщиком не сохранена: %v", err)
	}
	// ту же проверку выполнит оболочка через verify-artifact
	trust, _ := updates.ResolveTrust(f.env.RootJSON, nil)
	pol := updates.LocalPolicy{Product: f.env.Product, Platform: "windows-x64", AppVersion: "0.5.0"}
	if _, err := updates.VerifyInstallerFile(trust, p, p+updates.MinisigSuffix, pol); err != nil {
		t.Fatalf("сохранённая подпись не проходит проверку оболочки: %v (%q)", err, sig)
	}
}

func TestVerifyLocalDoesNotPersistASignatureForARejectedFile(t *testing.T) {
	f := newUpdFix(t, "0.5.0", nil)
	f.release(updatesRelease("0.7.0", []byte("installer-0.7.0")))
	p := f.stageLocal(t, []byte("другие байты"), claimsFor(f, "0.7.0"), false)
	got, err := f.verifyLocal(p)
	if err != nil || got.Valid {
		t.Fatalf("файл, которого нет в манифесте: %+v %v", got, err)
	}
	if _, err := os.Stat(p + updates.MinisigSuffix); err == nil {
		t.Fatal("для отвергнутого файла подпись не сохраняется")
	}
}
