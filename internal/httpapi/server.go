// Package httpapi exposes 腾手's read-only local data API and the LAN test
// page. The data API is intentionally GET-only so external agents (B6 MCP)
// cannot mutate the local dataset.
package httpapi

import (
	"context"
	"crypto/subtle"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/LuSrackhall/tengshou/internal/config"
	"github.com/LuSrackhall/tengshou/internal/store"
	"github.com/LuSrackhall/tengshou/internal/version"
)

// Options wires the server to the application.
type Options struct {
	Reader  *store.Reader
	Logger  *slog.Logger
	Config  config.Config
	Version string
}

// Server owns the local HTTP service.
type Server struct {
	opts Options
	mux  *http.ServeMux
	srv  *http.Server

	mu   sync.RWMutex
	urls []string
}

// New constructs the server and its routes.
func New(opts Options) *Server {
	s := &Server{opts: opts, mux: http.NewServeMux()}
	s.routes()
	handler := s.withLogging(s.withRecover(s.withSecurityHeaders(s.mux)))
	s.srv = &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	return s
}

// Handler returns the full middleware-wrapped handler (useful for tests).
func (s *Server) Handler() http.Handler { return s.srv.Handler }

// Listen binds the configured address.
func (s *Server) Listen() (net.Listener, error) {
	return net.Listen("tcp", s.opts.Config.ListenAddr())
}

// Serve binds and blocks until ctx is cancelled or the server stops.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	if ln == nil {
		var err error
		ln, err = s.Listen()
		if err != nil {
			return err
		}
	}
	if logger := s.opts.Logger; logger != nil {
		logger.Info("local service listening", "addr", ln.Addr().String(), "lan", s.opts.Config.HTTP.AllowLAN)
	}
	urls := s.URLs(ln)
	s.mu.Lock()
	s.urls = urls
	s.mu.Unlock()
	if logger := s.opts.Logger; logger != nil {
		for _, u := range urls {
			logger.Info("local service url", "url", u)
		}
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.srv.Shutdown(shutdownCtx)
	}()
	err := s.srv.Serve(ln)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// Shutdown stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.srv.Shutdown(ctx)
}

// URLs returns human-readable URLs for this service.
func (s *Server) URLs(ln net.Listener) []string {
	port := s.opts.Config.HTTP.Port
	if ln != nil {
		if tcp, ok := ln.Addr().(*net.TCPAddr); ok {
			port = tcp.Port
		}
	}
	urls := []string{httpURL("127.0.0.1", port)}
	if s.opts.Config.HTTP.AllowLAN {
		for _, ip := range localIPv4s() {
			urls = append(urls, httpURL(ip, port))
		}
	}
	return urls
}

func httpURL(host string, port int) string {
	return "http://" + net.JoinHostPort(host, strconv.Itoa(port)) + "/"
}

func localIPv4s() []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	out := []string{}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.To4() == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
				continue
			}
			out = append(out, ip.String())
		}
	}
	sort.Strings(out)
	return out
}

func (s *Server) routes() {
	m := s.mux
	m.HandleFunc("GET /", s.handleIndex)
	m.HandleFunc("GET /api/v1/health", s.handleHealth)
	m.HandleFunc("GET /api/v1/schema", s.auth(s.handleSchema))
	m.HandleFunc("GET /api/v1/stats", s.auth(s.handleStats))
	m.HandleFunc("GET /api/v1/parties", s.auth(s.handleListParties))
	m.HandleFunc("GET /api/v1/parties/{id}", s.auth(s.handleGetParty))
	m.HandleFunc("GET /api/v1/contracts", s.auth(s.handleListContracts))
	m.HandleFunc("GET /api/v1/contracts/{id}", s.auth(s.handleGetContract))
	m.HandleFunc("GET /api/v1/templates", s.auth(s.handleListTemplates))
	m.HandleFunc("GET /api/v1/templates/{id}", s.auth(s.handleGetTemplate))
	m.HandleFunc("GET /api/v1/signature-assets", s.auth(s.handleListSignatures))
	m.HandleFunc("GET /api/v1/documents", s.auth(s.handleListDocuments))
	m.HandleFunc("GET /api/v1", s.auth(s.handleAPIRoot))
}

// auth enforces bearer-token access for data endpoints. The test page and
// health endpoint stay public so a phone can confirm LAN reachability.
func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			writeError(w, http.StatusMethodNotAllowed, "read_only", "this API is read-only; only GET is allowed")
			return
		}
		token := s.opts.Config.HTTP.Token
		if token == "" {
			next(w, r)
			return
		}
		provided := ""
		if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
			provided = strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
		}
		if provided == "" {
			provided = strings.TrimSpace(r.Header.Get("X-Api-Token"))
		}
		if subtle.ConstantTimeCompare([]byte(provided), []byte(token)) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="tengshou"`)
			writeError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid API token")
			return
		}
		next(w, r)
	}
}

func (s *Server) withRecover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				if s.opts.Logger != nil {
					s.opts.Logger.Error("http panic", "path", r.URL.Path, "panic", rec)
				}
				writeError(w, http.StatusInternalServerError, "internal", "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; img-src data:; base-uri 'none'; form-action 'none'")
		}
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (s *Server) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		if s.opts.Logger != nil {
			s.opts.Logger.Debug("http request", "method", r.Method, "path", r.URL.Path,
				"status", rec.status, "duration_ms", time.Since(start).Milliseconds())
		}
	})
}

func (s *Server) version() string {
	if s.opts.Version != "" {
		return s.opts.Version
	}
	return version.String()
}
