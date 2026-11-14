// SPDX-License-Identifier: GPL-3.0-only

package api

// test_probes.go — отдельные замеры URL- и полного теста (T-84) через
// временный инстанс (inst.TestDialer): задержка HTTP GET, UDP-задержка
// (DNS-запрос), скорость скачивания, IP выхода. Каждая функция ходит только
// через переданный dialer — у http.Transport нет ни Proxy, ни собственного
// набора номера, то есть напрямую запрос уйти не может.

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/inst"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/version"
)

const (
	// maxIPEchoBytes — сколько читаем ответа эхо-сервиса IP.
	maxIPEchoBytes = 64 << 10
	// udpProbeName — имя в DNS-запросе UDP-замера: любой резолвер ответит.
	udpProbeName = "example.com"
)

// httpViaDialer — клиент, чьи соединения идут только через d. Без Proxy
// (переменные окружения HTTP_PROXY не действуют), без повторного использования
// соединений, редиректы не выполняются: замер — первый ответ.
func httpViaDialer(d inst.TestDialer) *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DialContext:        d.DialContext,
			DisableKeepAlives:  true,
			DisableCompression: true, // скорость считается по байтам на проводе
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// getVia выполняет GET через d и возвращает ответ (тело закрывает вызывающий).
func getVia(ctx context.Context, d inst.TestDialer, rawURL string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "LAVRay/"+version.Version)
	return httpViaDialer(d).Do(req)
}

// probeURL — задержка URL-теста: от начала запроса (дозвон, TLS, отправка) до
// заголовков ответа, как sing-box urltest. Статус ответа не важен: сервер
// ответил — туннель работает (captive-портал и 4xx тоже ответы).
func probeURL(ctx context.Context, d inst.TestDialer, rawURL string, timeout time.Duration) (int32, error) {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	start := time.Now()
	resp, err := getVia(cctx, d, rawURL)
	elapsed := time.Since(start)
	if err != nil {
		return 0, probeError(ctx, cctx, err, timeout)
	}
	_ = resp.Body.Close()
	return atLeastMs(elapsed), nil
}

// probeSpeed — скорость скачивания в бит/с: байты тела за время от заголовков
// ответа до конца тела или до таймаута (по таймауту замер завершается штатно,
// если что-то уже скачано).
func probeSpeed(ctx context.Context, d inst.TestDialer, rawURL string, timeout time.Duration) (float64, error) {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	resp, err := getVia(cctx, d, rawURL)
	if err != nil {
		return 0, probeError(ctx, cctx, err, timeout)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return 0, fmt.Errorf("сервер скорости ответил %s", resp.Status)
	}
	start := time.Now()
	n, err := io.Copy(io.Discard, resp.Body)
	elapsed := time.Since(start)
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}
	timedOut := cctx.Err() != nil
	if err != nil && !timedOut {
		return 0, err // обрыв до таймаута — не измерение
	}
	if n == 0 {
		if timedOut {
			return 0, probeError(ctx, cctx, err, timeout)
		}
		return 0, errors.New("пустой ответ сервера скорости")
	}
	if elapsed < time.Millisecond {
		elapsed = time.Millisecond
	}
	return float64(n) * 8 / elapsed.Seconds(), nil
}

// probeEgressIP — IP, под которым профиль выходит в интернет, из ответа
// эхо-сервиса: строка ip=… (cloudflare.com/cdn-cgi/trace) либо голый адрес.
func probeEgressIP(ctx context.Context, d inst.TestDialer, rawURL string, timeout time.Duration) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	resp, err := getVia(cctx, d, rawURL)
	if err != nil {
		return "", probeError(ctx, cctx, err, timeout)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", fmt.Errorf("сервис IP ответил %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxIPEchoBytes))
	if err != nil {
		return "", probeError(ctx, cctx, err, timeout)
	}
	ip, ok := parseEgressIP(string(body))
	if !ok {
		return "", errors.New("в ответе сервиса IP нет адреса")
	}
	return ip, nil
}

// parseEgressIP достаёт адрес из ответа эхо-сервиса.
func parseEgressIP(body string) (string, bool) {
	for _, line := range strings.Split(body, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "ip="); ok {
			if a, err := netip.ParseAddr(strings.TrimSpace(v)); err == nil {
				return a.String(), true
			}
		}
	}
	if a, err := netip.ParseAddr(strings.TrimSpace(body)); err == nil {
		return a.String(), true
	}
	return "", false
}

// hostPortAddr — цель UDP как net.Addr: пакетное соединение sing-box разбирает
// String() ("host:port"), так что работают и IP, и имя хоста (имя уйдёт серверу
// профиля, локального DNS-запроса не будет).
type hostPortAddr string

func (hostPortAddr) Network() string  { return "udp" }
func (a hostPortAddr) String() string { return string(a) }

// probeUDP — UDP-задержка: один DNS-запрос к target ("host:port") через
// исходящее профиля и время до ответа с тем же идентификатором. Содержимое
// ответа не проверяется: годится и обычное UDP-эхо.
func probeUDP(ctx context.Context, d inst.TestDialer, target string, timeout time.Duration) (int32, error) {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	pc, err := d.ListenPacket(cctx, target)
	if err != nil {
		return 0, probeError(ctx, cctx, err, timeout)
	}
	defer pc.Close()
	// Отмена StopTests должна рвать ожидание ответа: ReadFrom ctx не знает.
	stop := context.AfterFunc(cctx, func() { _ = pc.Close() })
	defer stop()
	if dl, ok := cctx.Deadline(); ok {
		_ = pc.SetDeadline(dl)
	}

	var idb [2]byte
	if _, err := rand.Read(idb[:]); err != nil {
		return 0, err
	}
	query := dnsQuery(binary.BigEndian.Uint16(idb[:]), udpProbeName)
	start := time.Now()
	if _, err := pc.WriteTo(query, hostPortAddr(target)); err != nil {
		return 0, probeError(ctx, cctx, err, timeout)
	}
	buf := make([]byte, 1500)
	for {
		n, _, err := pc.ReadFrom(buf)
		if err != nil {
			return 0, probeError(ctx, cctx, err, timeout)
		}
		if n >= 12 && buf[0] == idb[0] && buf[1] == idb[1] {
			return atLeastMs(time.Since(start)), nil
		}
	}
}

// dnsQuery — DNS-запрос A-записи name (рекурсия желательна).
func dnsQuery(id uint16, name string) []byte {
	b := binary.BigEndian.AppendUint16(nil, id)
	b = append(b, 0x01, 0x00, 0, 1, 0, 0, 0, 0, 0, 0) // RD, QDCOUNT=1
	for _, label := range strings.Split(name, ".") {
		b = append(b, byte(len(label)))
		b = append(b, label...)
	}
	return append(b, 0, 0, 1, 0, 1) // корень, QTYPE=A, QCLASS=IN
}

// probeError переводит ошибку замера в понятный текст. Отмену родителя
// (StopTests) возвращает как есть — вызывающий отличает её по ctx.Err().
func probeError(parent, probe context.Context, err error, timeout time.Duration) error {
	if parent.Err() != nil {
		return parent.Err()
	}
	if probe.Err() != nil || errors.Is(err, context.DeadlineExceeded) || isTimeout(err) {
		return &probeTimeoutError{msg: "таймаут " + strconv.Itoa(int(timeout/time.Millisecond)) + " мс"}
	}
	return err
}

// probeTimeoutError — замер не уложился во время; отличается от прочих сбоев
// типом (проверка связи после подключения не повторяет запрос по таймауту).
type probeTimeoutError struct{ msg string }

func (e *probeTimeoutError) Error() string { return e.msg }

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

// atLeastMs — миллисекунды, не меньше 1: ноль означает «не тестировали».
func atLeastMs(d time.Duration) int32 {
	ms := int32(d / time.Millisecond)
	if ms <= 0 {
		return 1
	}
	return ms
}
