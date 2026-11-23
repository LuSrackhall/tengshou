package httpapi

import (
	"embed"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
)

//go:embed web/app/index.html web/app/app.css web/app/vendor/pdfjs/pdf.min.js web/app/vendor/pdfjs/pdf.worker.min.js all:web/app/dist
var appFS embed.FS

// appDistFS 是 Vite 构建产物目录（frontend/ → web/app/dist）。入库的
// dist/.keep 让 go:embed 在未执行前端构建的干净 clone 里也成立；正式产物
// 由 CI/发布流程在 go build 前执行 npm ci && npm run build 生成。
var appDistFS = func() fs.FS {
	sub, err := fs.Sub(appFS, "web/app/dist")
	if err != nil {
		panic("httpapi: embedded web/app/dist is missing: " + err.Error())
	}
	return sub
}()

func (s *Server) handleAppIndex(w http.ResponseWriter, r *http.Request) {
	// GET /app/ is a subtree pattern: only the app entry itself may fall back
	// to index.html. Retired or unknown assets must 404 instead of becoming
	// the page (app.js is retired as of phase 1).
	if r.URL.Path != "/app/" && r.URL.Path != "/app/index.html" {
		http.NotFound(w, r)
		return
	}
	s.serveAppFile(w, r, "web/app/index.html", "text/html; charset=utf-8")
}

func (s *Server) handleAppAsset(name, contentType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.serveAppFile(w, r, "web/app/"+name, contentType)
	}
}

// handleAppDist serves the Vue app bundle built by Vite. File names are
// fixed (tengshou-ui.js / tengshou-ui.css), so responses are always
// revalidated instead of cache-busted.
func (s *Server) handleAppDist(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/app/dist/")
	if name == "" {
		http.NotFound(w, r)
		return
	}
	data, err := fs.ReadFile(appDistFS, name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	contentType := mime.TypeByExtension(path.Ext(name))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (s *Server) serveAppFile(w http.ResponseWriter, r *http.Request, name, contentType string) {
	data, err := appFS.ReadFile(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
