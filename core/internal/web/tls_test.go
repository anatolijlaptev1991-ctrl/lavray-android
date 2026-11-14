// SPDX-License-Identifier: GPL-3.0-only

package web

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"testing"
	"time"
)

var fingerprintRE = regexp.MustCompile(`^([0-9A-F]{2}:){31}[0-9A-F]{2}$`)

// TestTLSFingerprintStable — отпечаток один и тот же между запусками (файл
// переиспользуется), ключ ECDSA P-256, права файла 0600.
func TestTLSFingerprintStable(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC)
	cert1, fp1, err := LoadOrCreateCert(dir, []string{"192.168.1.1", "router.lan"}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if !fingerprintRE.MatchString(fp1) {
		t.Fatalf("формат отпечатка: %q", fp1)
	}
	cert2, fp2, err := LoadOrCreateCert(dir, []string{"192.168.1.1"}, func() time.Time { return now.Add(24 * time.Hour) })
	if err != nil {
		t.Fatal(err)
	}
	if fp1 != fp2 {
		t.Fatalf("отпечаток сменился при повторной загрузке: %s != %s", fp1, fp2)
	}
	if fp1 != Fingerprint(cert2.Certificate[0]) || fp1 != Fingerprint(cert1.Certificate[0]) {
		t.Fatal("отпечаток не совпадает с DER сертификата")
	}

	leaf, err := x509.ParseCertificate(cert1.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	pub, ok := leaf.PublicKey.(*ecdsa.PublicKey)
	if !ok || pub.Curve != elliptic.P256() {
		t.Fatalf("ключ не ECDSA P-256: %T", leaf.PublicKey)
	}
	if err := leaf.VerifyHostname("192.168.1.1"); err != nil {
		t.Errorf("IP роутера нет в SAN: %v", err)
	}
	if err := leaf.VerifyHostname("127.0.0.1"); err != nil {
		t.Errorf("loopback нет в SAN: %v", err)
	}
	if err := leaf.VerifyHostname("router.lan"); err != nil {
		t.Errorf("имя хоста нет в SAN: %v", err)
	}
	if runtime.GOOS != "windows" {
		st, err := os.Stat(filepath.Join(dir, TLSFileName))
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode().Perm() != 0o600 {
			t.Fatalf("права файла сертификата %v, ждали 0600", st.Mode().Perm())
		}
	}
}

func TestTLSRegeneratesExpiringAndCorrupt(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC)
	_, fp1, err := LoadOrCreateCert(dir, nil, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	// За 10 дней до конца срока сертификат пересоздаётся.
	_, fp2, err := LoadOrCreateCert(dir, nil, func() time.Time { return now.Add(certValidity - 10*24*time.Hour) })
	if err != nil || fp2 == fp1 {
		t.Fatalf("истекающий сертификат не обновлён: err=%v same=%v", err, fp1 == fp2)
	}
	if err := os.WriteFile(filepath.Join(dir, TLSFileName), []byte("garbage"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, fp3, err := LoadOrCreateCert(dir, nil, func() time.Time { return now })
	if err != nil || fp3 == "" {
		t.Fatalf("испорченный файл не пересоздан: %v", err)
	}
}

// TestTLSServesWithCertificate — сертификат пригоден для рукопожатия.
func TestTLSServesWithCertificate(t *testing.T) {
	cert, fp, err := LoadOrCreateCert(t.TempDir(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) }))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
	srv.StartTLS()
	defer srv.Close()
	var seen string
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		InsecureSkipVerify: true, //nolint:gosec // проверяем отпечаток вручную, как это делает пользователь
		VerifyPeerCertificate: func(raw [][]byte, _ [][]*x509.Certificate) error {
			seen = Fingerprint(raw[0])
			return nil
		},
	}}}
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if seen != fp {
		t.Fatalf("клиент увидел отпечаток %s, ждали %s", seen, fp)
	}
}
