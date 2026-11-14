// SPDX-License-Identifier: GPL-3.0-only

package web

import "net/http"

// notEmbeddedPage — страница вместо интерфейса в сборке без with_webui.
const notEmbeddedPage = `<!doctype html>
<html lang="ru"><head><meta charset="utf-8"><title>LAVRay</title>
<meta name="viewport" content="width=device-width, initial-scale=1"></head>
<body style="font-family:system-ui,sans-serif;max-width:40rem;margin:3rem auto;padding:0 1rem">
<h1>LAVRay: веб-панель не встроена</h1>
<p>Это ядро собрано без интерфейса. Нужна сборка с тегом <code>with_webui</code>
(<code>lavray-core-linux-*</code> из релиза) либо каталог <code>ui/dist</code> в составе пакета.</p>
<p>The web panel is not embedded in this build. Use a release build with the
<code>with_webui</code> tag.</p>
</body></html>`

// NotEmbeddedHandler — ответ «панель не встроена» (HTML, а не 404).
func NotEmbeddedHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(notEmbeddedPage))
		}
	})
}

// WebUIHandler — раздача интерфейса: встроенный ui/dist (тег with_webui) либо
// страница «панель не встроена».
func WebUIHandler() http.Handler {
	if fsys, ok := embeddedFS(); ok {
		return NewStaticHandler(fsys)
	}
	return NotEmbeddedHandler()
}
