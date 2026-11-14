// SPDX-License-Identifier: GPL-3.0-only

package web

// tls.go — самоподписанный сертификат веб-слушателя (--web-tls): ECDSA P-256,
// пара ключ+сертификат в одном PEM-файле с правами 0600 в каталоге данных.
// Сертификат живёт между запусками, поэтому отпечаток sha256 стабилен и
// пользователь сверяет его один раз (LuCI показывает его при установке
// соединения). Пересоздаётся только при порче файла или за 30 дней до конца срока.

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// TLSFileName — файл с сертификатом и закрытым ключом.
const TLSFileName = "web-tls.pem"

const (
	certValidity   = 5 * 365 * 24 * time.Hour
	certRenewAhead = 30 * 24 * time.Hour
)

// Fingerprint — отпечаток sha256 сертификата (DER) в виде AA:BB:…, как его
// показывают браузеры.
func Fingerprint(der []byte) string {
	sum := sha256.Sum256(der)
	h := strings.ToUpper(hex.EncodeToString(sum[:]))
	parts := make([]string, 0, len(sum))
	for i := 0; i < len(h); i += 2 {
		parts = append(parts, h[i:i+2])
	}
	return strings.Join(parts, ":")
}

// LoadOrCreateCert читает сертификат из dir или создаёт новый. hosts —
// дополнительные адреса и имена для SAN (адрес привязки, имя роутера); loopback
// и localhost добавляются всегда. now == nil — time.Now.
func LoadOrCreateCert(dir string, hosts []string, now func() time.Time) (tls.Certificate, string, error) {
	if now == nil {
		now = time.Now
	}
	path := filepath.Join(dir, TLSFileName)
	if raw, err := os.ReadFile(path); err == nil {
		if cert, ok := parseUsable(raw, now()); ok {
			return cert, Fingerprint(cert.Certificate[0]), nil
		}
	}
	pemBytes, err := generateCert(hosts, now())
	if err != nil {
		return tls.Certificate{}, "", err
	}
	if err := writeFileAtomic(path, pemBytes); err != nil {
		return tls.Certificate{}, "", fmt.Errorf("web: сохранение сертификата: %w", err)
	}
	cert, err := tls.X509KeyPair(pemBytes, pemBytes)
	if err != nil {
		return tls.Certificate{}, "", err
	}
	return cert, Fingerprint(cert.Certificate[0]), nil
}

// parseUsable разбирает PEM и проверяет, что сертификат ещё долго действует.
func parseUsable(raw []byte, now time.Time) (tls.Certificate, bool) {
	cert, err := tls.X509KeyPair(raw, raw)
	if err != nil {
		return tls.Certificate{}, false
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil || now.Add(certRenewAhead).After(leaf.NotAfter) || now.Before(leaf.NotBefore) {
		return tls.Certificate{}, false
	}
	if pub, ok := leaf.PublicKey.(*ecdsa.PublicKey); !ok || pub.Curve != elliptic.P256() {
		return tls.Certificate{}, false
	}
	return cert, true
}

func generateCert(hosts []string, now time.Time) ([]byte, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		return nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "LAVRay web panel", Organization: []string{"LAVRay"}},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(certValidity),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}
	for _, h := range hosts {
		if ip := net.ParseIP(h); ip != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
		} else if h != "" {
			tmpl.DNSNames = append(tmpl.DNSNames, h)
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, err
	}
	out := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	return append(out, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})...), nil
}

// writeFileAtomic пишет файл через временный файл и rename: оборванная запись не
// оставляет полупустой PEM. os.CreateTemp создаёт файл с правами 0600.
func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".web-tls-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
