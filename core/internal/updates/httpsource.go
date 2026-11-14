// SPDX-License-Identifier: GPL-3.0-only

// httpsource.go — HTTP(S)-источник метаданных и артефактов. Правила:
// ответ больше лимита — ErrTooLarge (не обрезается молча); 404 — ErrNotFound;
// редиректы только на https и не больше пяти; соединение 3 с, первый байт 5 с
// (для клиента по умолчанию; свой клиент задаёт таймауты сам).
package updates

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Умолчания источника.
const (
	DefaultMetaMaxBytes     = 8 << 20 // метаданные — килобайты; 8 МиБ с запасом
	DefaultConnectTimeout   = 3 * time.Second
	DefaultFirstByteTimeout = 5 * time.Second
	DefaultStallTimeout     = 30 * time.Second // тело не двигается дольше — обрыв
	MaxRedirects            = 5
)

// HTTPSource — источник с базой URL (…/releases/download/…).
type HTTPSource struct {
	Base     string
	Client   *http.Client // nil → клиент с таймаутами по умолчанию
	MaxBytes int64        // лимит тела метаданных (0 → DefaultMetaMaxBytes)
	// ConnectTimeout/FirstByteTimeout действуют только для клиента по умолчанию.
	ConnectTimeout   time.Duration
	FirstByteTimeout time.Duration
	// StallTimeout — сколько тело ответа может не отдавать ни байта (0 → 30 с);
	// действует для любых клиентов, включая заданный снаружи.
	StallTimeout time.Duration
}

// Fetch скачивает небольшой документ целиком (с лимитом).
func (s *HTTPSource) Fetch(ctx context.Context, name string) ([]byte, error) {
	resp, err := s.get(ctx, name, 0)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, statusError(name, resp.StatusCode)
	}
	limit := s.MaxBytes
	if limit <= 0 {
		limit = DefaultMetaMaxBytes
	}
	if resp.ContentLength > limit {
		return nil, fmt.Errorf("%w: %s (%d > %d)", ErrTooLarge, name, resp.ContentLength, limit)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("%w: %s (> %d)", ErrTooLarge, name, limit)
	}
	return body, nil
}

// Open открывает артефакт с докачкой: при 206 Start — запрошенное смещение,
// при 200 (сервер проигнорировал Range) — 0, и вызывающий начинает заново.
func (s *HTTPSource) Open(ctx context.Context, name string, offset int64) (*ArtifactStream, error) {
	resp, err := s.get(ctx, name, offset)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusRequestedRangeNotSatisfiable && offset > 0 {
		resp.Body.Close()
		return s.Open(ctx, name, 0)
	}
	switch resp.StatusCode {
	case http.StatusOK:
		return &ArtifactStream{Body: resp.Body, Start: 0, Total: resp.ContentLength}, nil
	case http.StatusPartialContent:
		start, total, ok := parseContentRange(resp.Header.Get("Content-Range"))
		if !ok {
			resp.Body.Close()
			return nil, fmt.Errorf("updates: %s: некорректный Content-Range", name)
		}
		return &ArtifactStream{Body: resp.Body, Start: start, Total: total}, nil
	}
	resp.Body.Close()
	return nil, statusError(name, resp.StatusCode)
}

func (s *HTTPSource) get(ctx context.Context, name string, offset int64) (*http.Response, error) {
	rctx, cancel := context.WithCancel(ctx)
	req, err := http.NewRequestWithContext(rctx, http.MethodGet, strings.TrimRight(s.Base, "/")+"/"+name, nil)
	if err != nil {
		cancel()
		return nil, err
	}
	if offset > 0 {
		req.Header.Set("Range", "bytes="+strconv.FormatInt(offset, 10)+"-")
	}
	resp, err := s.client().Do(req)
	if err != nil {
		cancel()
		return nil, err
	}
	stall := s.StallTimeout
	if stall <= 0 {
		stall = DefaultStallTimeout
	}
	resp.Body = newIdleBody(resp.Body, stall, cancel)
	return resp, nil
}

// client — копия клиента с политикой редиректов (только https, не больше пяти).
func (s *HTTPSource) client() *http.Client {
	cl := s.Client
	if cl == nil {
		cl = s.defaultClient()
	}
	local := *cl
	local.CheckRedirect = checkRedirect
	return &local
}

func (s *HTTPSource) defaultClient() *http.Client {
	connect, first := s.ConnectTimeout, s.FirstByteTimeout
	if connect <= 0 {
		connect = DefaultConnectTimeout
	}
	if first <= 0 {
		first = DefaultFirstByteTimeout
	}
	return &http.Client{Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: connect}).DialContext,
		TLSHandshakeTimeout:   connect,
		ResponseHeaderTimeout: first,
		DisableKeepAlives:     true,
	}}
}

func checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) > MaxRedirects {
		return fmt.Errorf("%w: больше %d переходов", ErrBadRedirect, MaxRedirects)
	}
	if req.URL.Scheme != "https" {
		return fmt.Errorf("%w: переход на %s", ErrBadRedirect, req.URL.Scheme)
	}
	return nil
}

func statusError(name string, code int) error {
	if code == http.StatusNotFound {
		return fmt.Errorf("%w: %s", ErrNotFound, name)
	}
	return fmt.Errorf("updates: %s: HTTP %d", name, code)
}

// parseContentRange разбирает «bytes 100-199/200».
func parseContentRange(h string) (start, total int64, ok bool) {
	rest, found := strings.CutPrefix(strings.TrimSpace(h), "bytes ")
	if !found {
		return 0, 0, false
	}
	span, size, found := strings.Cut(rest, "/")
	from, _, found2 := strings.Cut(span, "-")
	if !found || !found2 {
		return 0, 0, false
	}
	start, err1 := strconv.ParseInt(from, 10, 64)
	total = -1
	if size != "*" {
		var err2 error
		if total, err2 = strconv.ParseInt(size, 10, 64); err2 != nil {
			return 0, 0, false
		}
	}
	return start, total, err1 == nil && start >= 0
}
