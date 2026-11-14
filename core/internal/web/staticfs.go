// SPDX-License-Identifier: GPL-3.0-only

package web

// staticfs.go — раздача собранного ui/dist из fs.FS: корень и клиентские
// маршруты SPA (путь без расширения) отдают index.html; каталоги не
// перечисляются; скрытые файлы и пути с "..", обратным слэшем и NUL не
// отдаются. fs.FS сам не выпускает путь за свой корень.

import (
	"io/fs"
	"net/http"
	"path"
	"strconv"
	"strings"
)

// contentTypes — типы по расширению. Свои, а не mime.TypeByExtension: на Windows
// тот читает реестр, и ответ зависит от установленных программ.
var contentTypes = map[string]string{
	".html":  "text/html; charset=utf-8",
	".js":    "text/javascript; charset=utf-8",
	".mjs":   "text/javascript; charset=utf-8",
	".css":   "text/css; charset=utf-8",
	".json":  "application/json",
	".svg":   "image/svg+xml",
	".png":   "image/png",
	".jpg":   "image/jpeg",
	".jpeg":  "image/jpeg",
	".webp":  "image/webp",
	".ico":   "image/x-icon",
	".woff":  "font/woff",
	".woff2": "font/woff2",
	".txt":   "text/plain; charset=utf-8",
	".map":   "application/json",
}

type staticHandler struct{ fsys fs.FS }

// NewStaticHandler раздаёт файлы из fsys (корень — каталог ui/dist).
func NewStaticHandler(fsys fs.FS) http.Handler { return &staticHandler{fsys: fsys} }

func (s *staticHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name, ok := cleanName(r.URL.Path)
	if !ok {
		http.Error(w, "bad path", http.StatusBadRequest)
		return
	}
	data, name, found := s.read(name)
	if !found {
		http.NotFound(w, r)
		return
	}
	h := w.Header()
	ct := contentTypes[strings.ToLower(path.Ext(name))]
	if ct == "" {
		ct = "application/octet-stream"
	}
	h.Set("Content-Type", ct)
	h.Set("Content-Length", strconv.Itoa(len(data)))
	if strings.HasPrefix(name, "assets/") {
		h.Set("Cache-Control", "public, max-age=31536000, immutable") // имена с хэшем содержимого
	} else {
		h.Set("Cache-Control", "no-cache")
	}
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodGet {
		_, _ = w.Write(data)
	}
}

// read возвращает содержимое файла. Нет файла и у пути нет расширения —
// клиентский маршрут SPA, отдаётся index.html. Каталог — не найден.
func (s *staticHandler) read(name string) ([]byte, string, bool) {
	info, err := fs.Stat(s.fsys, name)
	switch {
	case err == nil && info.IsDir():
		return nil, "", false
	case err != nil && path.Ext(name) == "":
		name = "index.html"
	case err != nil:
		return nil, "", false
	}
	data, err := fs.ReadFile(s.fsys, name)
	if err != nil {
		return nil, "", false
	}
	return data, name, true
}

// cleanName превращает URL-путь в имя внутри fs.FS. ok=false — путь недопустим:
// "..", обратный слэш, NUL, скрытый сегмент.
func cleanName(p string) (string, bool) {
	if strings.ContainsAny(p, "\x00\\") {
		return "", false
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." || (strings.HasPrefix(seg, ".") && seg != "" && seg != ".") {
			return "", false
		}
	}
	name := strings.TrimPrefix(path.Clean("/"+p), "/")
	if name == "" {
		name = "index.html"
	}
	return name, fs.ValidPath(name)
}
