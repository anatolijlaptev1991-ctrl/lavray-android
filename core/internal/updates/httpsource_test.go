// SPDX-License-Identifier: GPL-3.0-only

package updates

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHTTPSourceFetchAndNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/meta/timestamp.json" {
			_, _ = w.Write([]byte("ok"))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	s := &HTTPSource{Base: srv.URL + "/meta/"}
	if b, err := s.Fetch(context.Background(), "timestamp.json"); err != nil || string(b) != "ok" {
		t.Fatalf("Fetch: %q %v", b, err)
	}
	if _, err := s.Fetch(context.Background(), "nope.json"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("404 должен давать ErrNotFound: %v", err)
	}
}

func TestHTTPSourceLimitIsConfigurable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(bytes.Repeat([]byte("a"), 100))
	}))
	defer srv.Close()
	small := &HTTPSource{Base: srv.URL, MaxBytes: 50}
	if _, err := small.Fetch(context.Background(), "x"); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("лимит 50 при теле 100: %v", err)
	}
	exact := &HTTPSource{Base: srv.URL, MaxBytes: 100}
	if b, err := exact.Fetch(context.Background(), "x"); err != nil || len(b) != 100 {
		t.Fatalf("тело ровно по лимиту должно проходить: %d %v", len(b), err)
	}
	if b, err := (&HTTPSource{Base: srv.URL}).Fetch(context.Background(), "x"); err != nil || len(b) != 100 {
		t.Fatalf("лимит по умолчанию: %d %v", len(b), err)
	}
}

func TestOverLimitErrorsNotTruncates(t *testing.T) {
	big := bytes.Repeat([]byte("b"), DefaultMetaMaxBytes+10)
	for _, chunked := range []bool{false, true} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if chunked {
				w.(http.Flusher).Flush() // без Content-Length
			}
			_, _ = w.Write(big)
		}))
		body, err := (&HTTPSource{Base: srv.URL}).Fetch(context.Background(), "x")
		srv.Close()
		if !errors.Is(err, ErrTooLarge) || body != nil {
			t.Fatalf("chunked=%v: ожидалась ErrTooLarge без данных, получено %d байт, %v", chunked, len(body), err)
		}
	}
}

func TestHttpsToHttpRedirectRejected(t *testing.T) {
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("plain"))
	}))
	defer plain.Close()
	tls := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plain.URL+"/x", http.StatusFound)
	}))
	defer tls.Close()
	s := &HTTPSource{Base: tls.URL, Client: tls.Client()}
	if _, err := s.Fetch(context.Background(), "x"); err == nil || !strings.Contains(err.Error(), ErrBadRedirect.Error()) {
		t.Fatalf("редирект https→http должен отвергаться: %v", err)
	}
}

func TestRedirectCap(t *testing.T) {
	// Цепочка из N переходов на один и тот же TLS-сервер.
	run := func(n int) error {
		count := 0
		var inner *httptest.Server
		inner = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if count >= n {
				_, _ = w.Write([]byte("end"))
				return
			}
			count++
			http.Redirect(w, r, inner.URL+"/next", http.StatusFound)
		}))
		defer inner.Close()
		_, err := (&HTTPSource{Base: inner.URL, Client: inner.Client()}).Fetch(context.Background(), "x")
		return err
	}
	if err := run(MaxRedirects); err != nil {
		t.Fatalf("ровно %d переходов допустимы: %v", MaxRedirects, err)
	}
	if err := run(MaxRedirects + 1); err == nil || !strings.Contains(err.Error(), ErrBadRedirect.Error()) {
		t.Fatalf("больше %d переходов должны отвергаться: %v", MaxRedirects, err)
	}
}

func TestHTTPSourceFirstByteTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(2 * time.Second):
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	s := &HTTPSource{Base: srv.URL, FirstByteTimeout: 100 * time.Millisecond}
	start := time.Now()
	if _, err := s.Fetch(context.Background(), "x"); err == nil || time.Since(start) > time.Second {
		t.Fatalf("медленный источник должен обрываться по таймауту первого байта: %v за %v", err, time.Since(start))
	}
}

func TestHTTPSourceOpenRange(t *testing.T) {
	data := []byte("0123456789")
	rangeSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "a.bin", time.Time{}, bytes.NewReader(data))
	}))
	defer rangeSrv.Close()
	st, err := (&HTTPSource{Base: rangeSrv.URL}).Open(context.Background(), "a.bin", 4)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(st.Body)
	st.Body.Close()
	if st.Start != 4 || st.Total != 10 || string(body) != "456789" {
		t.Fatalf("Range: start=%d total=%d body=%q", st.Start, st.Total, body)
	}
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(data) // Range игнорируется
	}))
	defer plain.Close()
	st, err = (&HTTPSource{Base: plain.URL}).Open(context.Background(), "a.bin", 4)
	if err != nil {
		t.Fatal(err)
	}
	st.Body.Close()
	if st.Start != 0 || st.Total != 10 {
		t.Fatalf("сервер без Range: start=%d total=%d", st.Start, st.Total)
	}
}

func TestParseContentRange(t *testing.T) {
	cases := []struct {
		in           string
		start, total int64
		ok           bool
	}{
		{"bytes 4-9/10", 4, 10, true}, {"bytes 0-9/*", 0, -1, true},
		{"", 0, 0, false}, {"items 1-2/3", 0, 0, false}, {"bytes x-9/10", 0, 0, false}, {"bytes 4-9/zz", 0, 0, false},
	}
	for _, c := range cases {
		s, tot, ok := parseContentRange(c.in)
		if ok != c.ok || (ok && (s != c.start || tot != c.total)) {
			t.Errorf("%q: got %d %d %v", c.in, s, tot, ok)
		}
	}
}
