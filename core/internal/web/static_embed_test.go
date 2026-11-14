// SPDX-License-Identifier: GPL-3.0-only

//go:build with_webui

package web

import (
	"net/http"
	"strings"
	"testing"
)

// TestEmbeddedWebUIServesHTML — в сборке с with_webui корень отдаёт HTML: либо
// встроенный index.html (каталог dist заполнен скриптом сборки), либо страницу
// «панель не встроена», если dist пуст.
func TestEmbeddedWebUIServesHTML(t *testing.T) {
	rec := get(WebUIHandler(), http.MethodGet, "/")
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("code=%d ct=%q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if _, ok := embeddedFS(); ok && !strings.Contains(strings.ToLower(rec.Body.String()), "<div id=\"root\">") {
		t.Fatalf("встроенный index.html без #root: %q", rec.Body.String())
	}
}
