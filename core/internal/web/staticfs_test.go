// SPDX-License-Identifier: GPL-3.0-only

package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func testSite() fstest.MapFS {
	return fstest.MapFS{
		"index.html":       {Data: []byte("<!doctype html><title>LAVRay</title>")},
		"assets/app-1.js":  {Data: []byte("console.log(1)")},
		"assets/app-1.css": {Data: []byte("body{}")},
		"logo.svg":         {Data: []byte("<svg/>")},
		".hidden":          {Data: []byte("hidden")},
	}
}

func get(h http.Handler, method, target string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, target, nil))
	return rec
}

// TestServeWebRootReturnsHTML — корень и клиентские маршруты SPA отдают index.html.
func TestServeWebRootReturnsHTML(t *testing.T) {
	h := NewStaticHandler(testSite())
	for _, p := range []string{"/", "/index.html", "/login", "/servers/42"} {
		rec := get(h, http.MethodGet, p)
		if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") ||
			!strings.Contains(rec.Body.String(), "<title>LAVRay</title>") {
			t.Errorf("GET %s: code=%d ct=%q body=%q", p, rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
		}
		if rec.Header().Get("Cache-Control") != "no-cache" {
			t.Errorf("GET %s: Cache-Control=%q, ждали no-cache", p, rec.Header().Get("Cache-Control"))
		}
	}
}

func TestStaticAssets(t *testing.T) {
	h := NewStaticHandler(testSite())
	cases := []struct{ path, ct string }{
		{"/assets/app-1.js", "text/javascript"},
		{"/assets/app-1.css", "text/css"},
		{"/logo.svg", "image/svg+xml"},
	}
	for _, c := range cases {
		rec := get(h, http.MethodGet, c.path)
		if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), c.ct) {
			t.Errorf("GET %s: code=%d ct=%q", c.path, rec.Code, rec.Header().Get("Content-Type"))
		}
	}
	if cc := get(h, http.MethodGet, "/assets/app-1.js").Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("хэшированный ресурс без immutable: %q", cc)
	}
	if rec := get(h, http.MethodGet, "/assets/missing.js"); rec.Code != http.StatusNotFound {
		t.Errorf("несуществующий ресурс: %d, ждали 404 (а не index.html)", rec.Code)
	}
	head := get(h, http.MethodHead, "/assets/app-1.js")
	if head.Code != http.StatusOK || head.Body.Len() != 0 || head.Header().Get("Content-Length") == "" {
		t.Errorf("HEAD: code=%d len=%d cl=%q", head.Code, head.Body.Len(), head.Header().Get("Content-Length"))
	}
	post := get(h, http.MethodPost, "/")
	if post.Code != http.StatusMethodNotAllowed || post.Header().Get("Allow") == "" {
		t.Errorf("POST: %d allow=%q", post.Code, post.Header().Get("Allow"))
	}
}

// TestStaticPathTraversal — выйти за корень встроенного каталога нельзя, листинга
// каталогов и скрытых файлов нет (S).
func TestStaticPathTraversal(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "dist", "assets"), 0o700); err != nil {
		t.Fatal(err)
	}
	must := func(p, content string) {
		if err := os.WriteFile(filepath.Join(root, p), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	must("secret.txt", "TOP-SECRET")
	must("dist/index.html", "<title>ok</title>")
	must("dist/assets/app.js", "js")
	must("dist/.env", "TOP-SECRET")
	h := NewStaticHandler(os.DirFS(filepath.Join(root, "dist")))

	targets := []string{
		"/../secret.txt", "/%2e%2e/secret.txt", "/assets/../../secret.txt", "/assets/%2e%2e/%2e%2e/secret.txt",
		"//secret.txt", "/..%2fsecret.txt", "/assets%5c..%5c..%5csecret.txt", "/assets\\..\\..\\secret.txt",
		"/%00", "/assets/app.js%00.html", "/.env", "/assets/../.env", "/....//secret.txt",
	}
	for _, p := range targets {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.URL.Path = decodePath(p)
		req.URL.RawPath = p
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		body, _ := io.ReadAll(rec.Body)
		if strings.Contains(string(body), "TOP-SECRET") {
			t.Errorf("GET %q отдал файл вне корня", p)
		}
	}
	for _, dir := range []string{"/assets/", "/assets"} {
		rec := get(h, http.MethodGet, dir)
		if strings.Contains(rec.Body.String(), "app.js") && rec.Code == http.StatusOK {
			t.Errorf("GET %s показал содержимое каталога", dir)
		}
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s: код %d, ждали 404", dir, rec.Code)
		}
	}
}

// decodePath — как net/http: процентное кодирование снято, в Path остаются
// управляющие символы и обратные слэши.
func decodePath(raw string) string {
	if p, err := url.PathUnescape(raw); err == nil {
		return p
	}
	return raw
}

// TestStubPageWhenNotEmbedded — без встроенного ui/dist корень отвечает
// страницей «панель не встроена», а не 404.
func TestStubPageWhenNotEmbedded(t *testing.T) {
	rec := get(NotEmbeddedHandler(), http.MethodGet, "/")
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") ||
		!strings.Contains(rec.Body.String(), "with_webui") {
		t.Fatalf("code=%d ct=%q body=%q", rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
	}
}
