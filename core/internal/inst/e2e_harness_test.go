// SPDX-License-Identifier: GPL-3.0-only

//go:build singbox_run

// e2e_harness_test.go — оснастка сквозных тестов протоколов (0.2.1): в одном
// процессе поднимаются серверы sing-box из того же форка (inbound) и клиент
// LAVRay по конфигу cfgbuild.Build из share-ссылки; HTTP-запрос идёт через
// mixed-вход клиента к httptest-серверу. Только loopback, внешней сети нет.
//
// Что доказывает прохождение запроса через туннель: адрес цели — имя
// proof.test, которое резолвит только сервер (DNS-сервер hosts в его конфиге).
// Клиент напрямую это имя не откроет, так что ответ с токеном получен именно
// через исходящее профиля.
package inst

import (
	"context"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/cfgbuild"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/links"
)

const (
	// e2eProofHost — имя цели: его знает только DNS-сервер hosts на стороне сервера.
	e2eProofHost = "proof.test"
	// e2eUUID — идентификатор пользователя vmess/vless/tuic (синтетический).
	e2eUUID = "b831381d-6324-4d53-ad4f-8cda48b30818"
)

// e2eLog копит журнал инстансов; при сбое теста печатает его целиком.
type e2eLog struct {
	mu    sync.Mutex
	lines []string
}

func (l *e2eLog) sink(name string) LogSink {
	return func(level, msg string) {
		l.mu.Lock()
		defer l.mu.Unlock()
		l.lines = append(l.lines, fmt.Sprintf("[%s %s] %s", name, level, msg))
	}
}

func (l *e2eLog) dump(t *testing.T) {
	t.Helper()
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, line := range l.lines {
		t.Log(line)
	}
}

// e2eEnv — стенд одного теста: цель, журнал, остановка инстансов.
type e2eEnv struct {
	t          *testing.T
	log        *e2eLog
	token      string
	targetPort int
}

// newE2EEnv поднимает httptest-цель, отвечающую уникальным токеном. На сбое
// теста выводит журнал серверной и клиентской сторон.
func newE2EEnv(t *testing.T) *e2eEnv {
	t.Helper()
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		t.Fatal(err)
	}
	env := &e2eEnv{t: t, log: &e2eLog{}, token: fmt.Sprintf("token-%x", raw)}
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, env.token)
	}))
	t.Cleanup(target.Close)
	_, port, err := net.SplitHostPort(target.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Sscan(port, &env.targetPort); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if t.Failed() {
			env.log.dump(t)
		}
	})
	return env
}

// run запускает инстанс sing-box из конфига и останавливает его в конце теста.
func (e *e2eEnv) run(name string, cfg map[string]any) {
	e.t.Helper()
	// С PlatformLogWriter (T-79) форк открывает cache_file в любом конфиге; два
	// инстанса теста не должны делить один файл «cache.db» (блокировка bbolt),
	// поэтому у каждого свой путь во временном каталоге.
	if _, has := cfg["experimental"]; !has {
		cfg["experimental"] = map[string]any{"cache_file": map[string]any{
			"enabled": true, "path": filepath.ToSlash(filepath.Join(e.t.TempDir(), name+"-cache.db")),
		}}
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		e.t.Fatal(err)
	}
	runner := NewSingBoxRunner()
	runner.SetLogSink(e.log.sink(name))
	if err := runner.Start(context.Background(), raw); err != nil {
		e.log.dump(e.t)
		e.t.Fatalf("%s: старт: %v\nконфиг: %s", name, err, raw)
	}
	e.t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = runner.Stop(ctx)
	})
}

// serve поднимает серверную сторону: inbound протокола и direct-выход, где имя
// proof.test указывает на loopback. Возвращает порт сервера.
func (e *e2eEnv) serve(inbound map[string]any, network string) int {
	e.t.Helper()
	var port int
	if network == "udp" {
		port = freeUDPPort(e.t)
	} else {
		port = freeTCPPort(e.t)
	}
	in := map[string]any{"tag": "in", "listen": "127.0.0.1", "listen_port": port}
	for k, v := range inbound {
		in[k] = v
	}
	e.run("server", map[string]any{
		"log": map[string]any{"level": "warn"},
		"dns": map[string]any{"servers": []any{map[string]any{
			"type": "hosts", "tag": "hosts",
			"predefined": map[string]any{e2eProofHost: "127.0.0.1"},
		}}},
		"inbounds":  []any{in},
		"outbounds": []any{map[string]any{"type": "direct", "tag": "direct"}},
		"route": map[string]any{
			"final":                   "direct",
			"default_domain_resolver": map[string]any{"server": "hosts"},
		},
	})
	return port
}

// clientOptions — отклонения клиента от настроек по умолчанию.
type clientOptions struct {
	allowInsecure bool // security.allow_insecure_default: самоподписанный сертификат сервера
}

// connect строит конфиг клиента из share-ссылки (cfgbuild.Build, как при
// подключении), запускает его и возвращает порт mixed-входа.
func (e *e2eEnv) connect(link string, opts clientOptions) int {
	e.t.Helper()
	cfg, port := e.clientConfig(link, opts)
	e.run("client", cfg)
	return port
}

// clientConfig — конфиг клиента и порт его mixed-входа: Build по ссылке, без
// experimental (run даёт каждому инстансу свой cache_file во временном каталоге).
func (e *e2eEnv) clientConfig(link string, opts clientOptions) (map[string]any, int) {
	e.t.Helper()
	p, err := links.ParseLink(link)
	if err != nil {
		e.t.Fatalf("ссылка %q: %v", link, err)
	}
	st := cfgbuild.DefaultSettings()
	st.LogLevel = "warn"
	st.Inbound.Port = freeTCPPort(e.t)
	st.AllowInsecureDefault = opts.allowInsecure
	res, err := cfgbuild.Build(cfgbuild.BuildRequest{Profile: p, Settings: st})
	if err != nil {
		e.t.Fatalf("Build: %v", err)
	}
	// Build отдаёт []map[string]any; JSON-круг приводит к виду, который читает sing-box.
	raw, err := json.Marshal(res.Config)
	if err != nil {
		e.t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		e.t.Fatal(err)
	}
	delete(cfg, "experimental")
	resolveLocalhostViaHosts(cfg)
	return cfg, st.Inbound.Port
}

// resolveLocalhostViaHosts подменяет обработчик DNS-сервера dns-direct: в
// Windows локальный резолвер sing-box спрашивает DNS напрямую и на имя
// localhost отвечает NXDOMAIN. Адрес сервера в ссылках остаётся именем
// (разрешение идёт через route.default_domain_resolver → dns-direct, как у
// пользователя), но имя localhost в тесте указывает на loopback.
func resolveLocalhostViaHosts(cfg map[string]any) {
	dns, _ := cfg["dns"].(map[string]any)
	servers, _ := dns["servers"].([]any)
	for i, raw := range servers {
		if srv, _ := raw.(map[string]any); srv["tag"] == "dns-direct" {
			servers[i] = map[string]any{
				"type": "hosts", "tag": "dns-direct",
				"predefined": map[string]any{"localhost": "127.0.0.1"},
			}
		}
	}
}

// fetch выполняет GET proof.test через mixed-вход клиента и сверяет токен.
func (e *e2eEnv) fetch(proxyPort int) error {
	proxy, err := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", proxyPort))
	if err != nil {
		return err
	}
	client := &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyURL(proxy), DisableKeepAlives: true},
		Timeout:   15 * time.Second,
	}
	resp, err := client.Get(fmt.Sprintf("http://%s:%d/", e2eProofHost, e.targetPort))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK || string(body) != e.token {
		return fmt.Errorf("ответ через туннель: %d %q, ожидался 200 %q", resp.StatusCode, body, e.token)
	}
	return nil
}

// mustFetch — fetch с остановкой теста при ошибке.
func (e *e2eEnv) mustFetch(proxyPort int) {
	e.t.Helper()
	if err := e.fetch(proxyPort); err != nil {
		e.log.dump(e.t)
		e.t.Fatalf("запрос через туннель не прошёл: %v", err)
	}
}

// mustFetchRetry — fetch с повторами для QUIC-протоколов (hysteria2, tuic). Сразу
// после старта sing-box получает первое событие смены сетевого интерфейса и
// закрывает клиент QUIC с ошибкой «network changed»: запрос, попавший в это окно,
// получает 502. Повтор через четверть секунды проходит; для остальных
// протоколов повторов нет, чтобы сбой не маскировался.
func (e *e2eEnv) mustFetchRetry(proxyPort int) {
	e.t.Helper()
	var err error
	for attempt := 0; attempt < 8; attempt++ {
		if err = e.fetch(proxyPort); err == nil {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	e.log.dump(e.t)
	e.t.Fatalf("запрос через туннель не прошёл после повторов: %v", err)
}

// freeUDPPort — свободный UDP-порт loopback.
func freeUDPPort(t *testing.T) int {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	return pc.LocalAddr().(*net.UDPAddr).Port
}

// e2eCertPEM — самоподписанный сертификат для localhost/127.0.0.1 и ключ (PEM).
func e2eCertPEM(t *testing.T) (certPEM, keyPEM string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}))
}

// pemLines — PEM как список строк (так sing-box читает certificate/key inline).
func pemLines(s string) []any {
	var out []any
	for _, line := range strings.Split(strings.TrimSpace(s), "\n") {
		out = append(out, line)
	}
	return out
}

// serverTLS — inbound-блок tls: сертификат localhost и список ALPN.
func serverTLS(t *testing.T, alpn ...string) map[string]any {
	t.Helper()
	cert, key := e2eCertPEM(t)
	tls := map[string]any{
		"enabled":     true,
		"server_name": "localhost",
		"certificate": pemLines(cert),
		"key":         pemLines(key),
	}
	if len(alpn) > 0 {
		list := make([]any, len(alpn))
		for i, a := range alpn {
			list[i] = a
		}
		tls["alpn"] = list
	}
	return tls
}

// vmessLink — v2rayN-ссылка vmess (base64 JSON).
func vmessLink(fields map[string]any) string {
	obj := map[string]any{"v": "2", "ps": "e2e", "aid": "0", "scy": "auto", "type": "none", "id": e2eUUID}
	for k, v := range fields {
		obj[k] = v
	}
	raw, _ := json.Marshal(obj)
	return "vmess://" + base64.StdEncoding.EncodeToString(raw)
}

// realityKeys — пара X25519 для REALITY (base64 без паддинга, как в xray/sing-box).
func realityKeys(t *testing.T) (private, public string) {
	t.Helper()
	k, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	enc := base64.RawURLEncoding
	return enc.EncodeToString(k.Bytes()), enc.EncodeToString(k.PublicKey().Bytes())
}

// h2Preface — начало любого соединения HTTP/2 (RFC 9113, 3.4).
const h2Preface = "PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n"

// startALPNFront поднимает TLS-фронт «как у провайдера» (nginx и подобные):
// сервер выбирает ALPN из списка клиента по своему порядку, а после выбора h2
// ждёт на соединении именно HTTP/2. Запрос HTTP/1.1 (апгрейд WebSocket) на
// соединении, где согласован h2, он обрывает. Соединения с http/1.1 или без
// ALPN пересылаются как есть на backendPort (внутренний сервер без TLS).
// Возвращает порт фронта.
func startALPNFront(t *testing.T, backendPort int, alpn ...string) int {
	t.Helper()
	certPEM, keyPEM := e2eCertPEM(t)
	pair, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
	if err != nil {
		t.Fatal(err)
	}
	cfg := &tls.Config{Certificates: []tls.Certificate{pair}, NextProtos: alpn}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			raw, err := ln.Accept()
			if err != nil {
				return
			}
			go serveALPNFront(raw, cfg, backendPort)
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port
}

func serveALPNFront(raw net.Conn, cfg *tls.Config, backendPort int) {
	defer raw.Close()
	conn := tls.Server(raw, cfg)
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	if err := conn.Handshake(); err != nil {
		return
	}
	_ = conn.SetDeadline(time.Time{})
	if conn.ConnectionState().NegotiatedProtocol == "h2" {
		// Согласован h2: сервер ждёт преамбулу HTTP/2, а клиент WebSocket шлёт
		// запрос HTTP/1.1 — соединение обрывается. Настоящего h2-клиента
		// в этих тестах нет, пересылать на внутренний сервер нечего.
		buf := make([]byte, len(h2Preface))
		_, _ = io.ReadFull(conn, buf)
		return
	}
	backend, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", backendPort))
	if err != nil {
		return
	}
	defer backend.Close()
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(backend, conn); done <- struct{}{} }()
	go func() { _, _ = io.Copy(conn, backend); done <- struct{}{} }()
	<-done
}

// listenerPort — порт httptest-сервера на loopback.
func listenerPort(t *testing.T, s *httptest.Server) int {
	t.Helper()
	return s.Listener.Addr().(*net.TCPAddr).Port
}
