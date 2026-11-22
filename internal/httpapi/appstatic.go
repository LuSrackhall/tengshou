package httpapi

import (
	"embed"
	"net/http"
)

//go:embed web/app/index.html web/app/app.js web/app/app.css
var appFS embed.FS

func (s *Server) handleAppIndex(w http.ResponseWriter, r *http.Request) {
	s.serveAppFile(w, r, "web/app/index.html", "text/html; charset=utf-8")
}

func (s *Server) handleAppAsset(name, contentType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.serveAppFile(w, r, "web/app/"+name, contentType)
	}
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
