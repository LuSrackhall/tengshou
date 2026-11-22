// Package httpapi exposes 腾手's read-only local data API, the loopback-only
// application write surface and the LAN signature page. External agents (B6
// MCP) read /api/v1; their write tools use /app/api with the X-Tengshou-MCP
// marker, so mutating requests stay on loopback and are audited separately.
package httpapi

import (
	"context"
	"crypto/subtle"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/LuSrackhall/tengshou/internal/config"
	"github.com/LuSrackhall/tengshou/internal/service"
	"github.com/LuSrackhall/tengshou/internal/sign"
	"github.com/LuSrackhall/tengshou/internal/store"
	"github.com/LuSrackhall/tengshou/internal/version"
)

// Options wires the server to the application.
type Options struct {
	Reader  *store.Reader
	Service *service.Service
	Logger  *slog.Logger
	Config  config.Config
	// Settings is the live runtime configuration. When set, switches such as
	// mcp.write_enabled are read from it so a change applies immediately; nil
	// falls back to the Config snapshot (tests, read-only embeds).
	Settings *config.Settings
	Version  string
	// Sign overrides the signature session manager (tests inject a fake
	// clock). Nil uses the config TTL and the system clock.
	Sign *sign.Manager
	// LANHosts overrides the LAN IPv4 addresses used in signing URLs. Nil
	// detects the machine's interfaces.
	LANHosts []string
	// EventPollInterval overrides how often the SSE endpoint checks for new
	// change-log rows. Zero uses one second.
	EventPollInterval time.Duration
	// EventHeartbeatInterval overrides the SSE keep-alive comment interval.
	// Zero uses fifteen seconds.
	EventHeartbeatInterval time.Duration
}

// Server owns the local HTTP service.
type Server struct {
	opts Options
	mux  *http.ServeMux
	srv  *http.Server
	sign *sign.Manager
	// eventsCtx is cancelled on shutdown so long-lived SSE handlers return
	// promptly instead of holding Shutdown open until its timeout.
	eventsCtx    context.Context
	eventsCancel context.CancelFunc

	mu   sync.RWMutex
	urls []string
}

// New constructs the server and its routes.
func New(opts Options) *Server {
	eventsCtx, eventsCancel := context.WithCancel(context.Background())
	s := &Server{opts: opts, mux: http.NewServeMux(), sign: opts.Sign,
		eventsCtx: eventsCtx, eventsCancel: eventsCancel}
	if s.sign == nil {
		ttl := time.Duration(opts.Config.Signature.TTLMinutes) * time.Minute
		s.sign = sign.NewManager(ttl, nil)
	}
	s.routes()
	handler := s.withLogging(s.withRecover(s.withSecurityHeaders(s.withSignGuard(s.withAppGuard(s.mux)))))
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
		_ = s.Shutdown(shutdownCtx)
	}()
	err := s.srv.Serve(ln)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// Shutdown stops the server. Open SSE streams are ended first so Shutdown
// does not wait for them to time out.
func (s *Server) Shutdown(ctx context.Context) error {
	s.eventsCancel()
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
	m.HandleFunc("GET /app/", s.handleAppIndex)
	m.HandleFunc("GET /app/app.js", s.handleAppAsset("app.js", "application/javascript; charset=utf-8"))
	m.HandleFunc("GET /app/app.css", s.handleAppAsset("app.css", "text/css; charset=utf-8"))
	m.HandleFunc("GET /app/vendor/pdf.min.js",
		s.handleAppAsset("vendor/pdfjs/pdf.min.js", "application/javascript; charset=utf-8"))
	m.HandleFunc("GET /app/vendor/pdf.worker.min.js",
		s.handleAppAsset("vendor/pdfjs/pdf.worker.min.js", "application/javascript; charset=utf-8"))
	m.HandleFunc("GET /app/dist/", s.handleAppDist)
	m.HandleFunc("GET /api/v1/health", s.handleHealth)
	m.HandleFunc("GET /api/v1/schema", s.auth(s.handleSchema))
	m.HandleFunc("GET /api/v1/stats", s.auth(s.handleStats))
	m.HandleFunc("GET /api/v1/fields", s.auth(s.handleListFields))
	m.HandleFunc("GET /api/v1/parties", s.auth(s.handleListParties))
	m.HandleFunc("GET /api/v1/parties/{id}", s.auth(s.handleGetParty))
	m.HandleFunc("GET /api/v1/contracts", s.auth(s.handleListContracts))
	m.HandleFunc("GET /api/v1/contracts/{id}", s.auth(s.handleGetContract))
	m.HandleFunc("GET /api/v1/templates", s.auth(s.handleListTemplates))
	m.HandleFunc("GET /api/v1/templates/{id}", s.auth(s.handleGetTemplate))
	m.HandleFunc("GET /api/v1/signature-assets", s.auth(s.handleListSignatures))
	m.HandleFunc("GET /api/v1/documents", s.auth(s.handleListDocuments))
	m.HandleFunc("GET /api/v1", s.auth(s.handleAPIRoot))

	// Application write surface. It is loopback-only and requires the
	// X-Tengshou-App header on mutating requests (see withAppGuard).
	m.HandleFunc("GET /app/api/v1/bootstrap", s.handleAppBootstrap)
	m.HandleFunc("GET /app/api/v1/onboarding", s.handleAppOnboarding)
	m.HandleFunc("GET /app/api/v1/onboarding/check", s.handleAppOnboardingCheck)
	m.HandleFunc("POST /app/api/v1/onboarding/state", s.handleAppOnboardingState)
	m.HandleFunc("GET /app/api/v1/events", s.handleAppEvents)
	m.HandleFunc("GET /app/api/v1/mcp/status", s.handleAppMCPStatus)
	m.HandleFunc("PUT /app/api/v1/settings/mcp", s.handleAppUpdateMCPSettings)
	m.HandleFunc("POST /app/api/v1/settings/mcp", s.handleAppUpdateMCPSettings)
	m.HandleFunc("GET /app/api/v1/fields", s.handleAppListFields)
	m.HandleFunc("POST /app/api/v1/fields", s.handleAppCreateField)
	m.HandleFunc("PUT /app/api/v1/fields/{id}", s.handleAppUpdateField)
	m.HandleFunc("POST /app/api/v1/fields/{id}/archive", s.handleAppArchiveField)
	m.HandleFunc("GET /app/api/v1/parties", s.handleAppListParties)
	m.HandleFunc("POST /app/api/v1/parties", s.handleAppSaveParty)
	m.HandleFunc("GET /app/api/v1/parties/{id}", s.handleAppGetParty)
	m.HandleFunc("PUT /app/api/v1/parties/{id}", s.handleAppSaveParty)
	m.HandleFunc("DELETE /app/api/v1/parties/{id}", s.handleAppDeleteParty)
	m.HandleFunc("GET /app/api/v1/contracts", s.handleAppListContracts)
	m.HandleFunc("POST /app/api/v1/contracts", s.handleAppSaveContract)
	m.HandleFunc("GET /app/api/v1/contracts/years", s.handleAppContractYears)
	m.HandleFunc("GET /app/api/v1/contracts/{id}/renewal-draft", s.handleAppContractRenewalDraft)
	m.HandleFunc("GET /app/api/v1/contracts/{id}", s.handleAppGetContract)
	m.HandleFunc("PUT /app/api/v1/contracts/{id}", s.handleAppSaveContract)
	m.HandleFunc("DELETE /app/api/v1/contracts/{id}", s.handleAppDeleteContract)
	m.HandleFunc("GET /app/api/v1/changelog", s.handleAppChangeLog)
	m.HandleFunc("GET /app/api/v1/seals", s.handleAppListSeals)
	m.HandleFunc("POST /app/api/v1/seals", s.handleAppImportSeal)
	m.HandleFunc("GET /app/api/v1/seals/{id}", s.handleAppGetSeal)
	m.HandleFunc("GET /app/api/v1/seals/{id}/image", s.handleAppSealImage)
	m.HandleFunc("GET /app/api/v1/seals/{id}/contracts", s.handleAppSealContracts)
	m.HandleFunc("PUT /app/api/v1/seals/{id}", s.handleAppUpdateSeal)
	m.HandleFunc("POST /app/api/v1/seals/{id}/replace", s.handleAppReplaceSealImage)
	m.HandleFunc("DELETE /app/api/v1/seals/{id}", s.handleAppDeleteSeal)
	m.HandleFunc("POST /app/api/v1/seals/{id}/attach", s.handleAppAttachSeal)
	m.HandleFunc("POST /app/api/v1/seals/{id}/detach", s.handleAppDetachSeal)
	m.HandleFunc("GET /app/api/v1/excel/template", s.handleAppExcelTemplate)
	m.HandleFunc("GET /app/api/v1/excel/ledger", s.handleAppExcelLedger)
	m.HandleFunc("POST /app/api/v1/excel/preview", s.handleAppExcelPreview)
	m.HandleFunc("POST /app/api/v1/excel/commit", s.handleAppExcelCommit)

	// B3 PDF template management: import a Word file (converted to PDF) or a
	// PDF, calibrate positions, preview and generate stamped PDFs.
	m.HandleFunc("GET /app/api/v1/templates", s.handleAppListTemplates)
	m.HandleFunc("POST /app/api/v1/templates/import", s.handleAppImportTemplate)
	m.HandleFunc("GET /app/api/v1/templates/{id}", s.handleAppGetTemplate)
	m.HandleFunc("DELETE /app/api/v1/templates/{id}", s.handleAppDeleteTemplate)
	m.HandleFunc("PUT /app/api/v1/templates/{id}/mapping", s.handleAppSaveTemplateMapping)
	m.HandleFunc("GET /app/api/v1/templates/{id}/usage", s.handleAppTemplateUsage)
	m.HandleFunc("GET /app/api/v1/templates/{id}/check", s.handleAppTemplateCheck)
	m.HandleFunc("GET /app/api/v1/templates/{id}/pdf", s.handleAppTemplatePDF)
	m.HandleFunc("GET /app/api/v1/templates/{id}/source", s.handleAppTemplateSource)
	m.HandleFunc("POST /app/api/v1/templates/{id}/preview", s.handleAppPreviewTemplate)
	m.HandleFunc("POST /app/api/v1/templates/{id}/preflight", s.handleAppPreflightGeneration)
	m.HandleFunc("POST /app/api/v1/templates/{id}/generate", s.handleAppGenerateContract)
	m.HandleFunc("POST /app/api/v1/templates/{id}/batch/preview", s.handleAppBatchPreviewGeneration)
	m.HandleFunc("POST /app/api/v1/templates/{id}/batch/generate", s.handleAppBatchGenerate)
	m.HandleFunc("GET /app/api/v1/converters", s.handleAppConverters)
	m.HandleFunc("GET /app/api/v1/documents", s.handleAppListDocuments)
	m.HandleFunc("GET /app/api/v1/documents/{id}/file", s.handleAppDocumentFile)

	// B9 data security: complete backup export, staged restore, consistency
	// report and audited signature export.
	m.HandleFunc("GET /app/api/v1/backup/export", s.handleAppBackupExport)
	m.HandleFunc("GET /app/api/v1/backup/status", s.handleAppBackupStatus)
	m.HandleFunc("POST /app/api/v1/backup/restore", s.handleAppBackupRestore)
	m.HandleFunc("GET /app/api/v1/integrity", s.handleAppIntegrity)
	m.HandleFunc("POST /app/api/v1/signatures/{id}/export", s.handleAppExportSignature)

	m.HandleFunc("GET /app/api/v1", s.handleAppAPIRoot)

	// B5 LAN signature capture. Signing sessions are created from the
	// loopback-only app surface; the phone page itself is LAN-only and
	// token-gated (see withSignGuard and signapi.go).
	m.HandleFunc("POST /app/api/v1/signature-sessions", s.handleAppCreateSignatureSession)
	m.HandleFunc("GET /app/api/v1/signature-sessions/{id}", s.handleAppGetSignatureSession)
	m.HandleFunc("DELETE /app/api/v1/signature-sessions/{id}", s.handleAppCancelSignatureSession)
	m.HandleFunc("GET /app/api/v1/signature-sessions/{id}/qr.png", s.handleAppSignatureQR)
	m.HandleFunc("GET /sign/{token}", s.handleSignPage)
	m.HandleFunc("GET /sign/{token}/status", s.handleSignStatus)
	m.HandleFunc("POST /sign/{token}/submit", s.handleSignSubmit)
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

// HeaderMCP marks a request as coming from the B6 MCP connector. Mutating
// requests with this header are gated by the live mcp.write_enabled setting and
// their change-log entries are recorded with actor=tengshou-mcp.
const HeaderMCP = "X-Tengshou-MCP"

// withAppGuard protects the application write surface: it must be reached
// from loopback, mutating requests must carry X-Tengshou-App: 1 (forcing a
// CORS preflight for foreign pages) and cross-site origins are rejected.
// Requests from the MCP connector additionally respect the write switch and
// carry the agent audit identity.
func (s *Server) withAppGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/app/api/") {
			if !isLoopbackRemote(r.RemoteAddr) {
				writeError(w, http.StatusForbidden, "loopback_required",
					"应用写入接口只允许在本机访问")
				return
			}
			if origin := r.Header.Get("Origin"); origin != "" && !originAllowed(origin, r.Host) {
				writeError(w, http.StatusForbidden, "origin_rejected", "跨站请求被拒绝")
				return
			}
			mutating := r.Method != http.MethodGet && r.Method != http.MethodHead
			if mutating {
				if r.Header.Get("X-Tengshou-App") != "1" {
					writeError(w, http.StatusForbidden, "app_header_required",
						"缺少 X-Tengshou-App 请求头")
					return
				}
				// Narrow B9 exception: the restore endpoint accepts a streamed
				// multipart zip instead of a base64 JSON body. Loopback, Origin
				// and X-Tengshou-App checks above still apply.
				multipartRestore := r.Method == http.MethodPost &&
					r.URL.Path == "/app/api/v1/backup/restore" &&
					strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "multipart/form-data")
				if ct := r.Header.Get("Content-Type"); !multipartRestore && ct != "" &&
					!strings.HasPrefix(strings.ToLower(ct), "application/json") {
					writeError(w, http.StatusUnsupportedMediaType, "json_required",
						"请求体必须是 application/json")
					return
				}
				if r.Header.Get(HeaderMCP) == "1" && !s.mcpWriteEnabled() {
					writeError(w, http.StatusForbidden, "mcp_write_disabled",
						"MCP 写工具已关闭（mcp.write_enabled=false）；可在腾手「设置」页重新开启")
					return
				}
			}
			if r.Header.Get(HeaderMCP) == "1" {
				r = r.WithContext(service.WithActor(r.Context(), service.MCPActor))
			}
		}
		next.ServeHTTP(w, r)
	})
}

// mcpWriteEnabled resolves the live MCP write switch. The Settings holder is
// authoritative when present; the startup Config snapshot is only a fallback.
func (s *Server) mcpWriteEnabled() bool {
	if s.opts.Settings != nil {
		return s.opts.Settings.MCPWriteEnabled()
	}
	return s.opts.Config.MCP.WriteEnabled
}

func isLoopbackRemote(remote string) bool {
	remote = strings.TrimSpace(remote)
	// In-process desktop webview handlers may not set RemoteAddr at all.
	if remote == "" {
		return true
	}
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	host = strings.TrimSpace(host)
	if host == "" || host == "@" || strings.EqualFold(host, "wails") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// originAllowed accepts same-origin requests, loopback origins and the custom
// schemes used by desktop webviews. Everything else is cross-site.
func originAllowed(origin, host string) bool {
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	if strings.EqualFold(u.Host, host) {
		return true
	}
	switch strings.ToLower(u.Scheme) {
	case "wails", "wails3", "file", "tauri":
		return true
	}
	hostname := strings.ToLower(u.Hostname())
	return hostname == "localhost" || hostname == "127.0.0.1" ||
		hostname == "::1" || hostname == "wails.localhost"
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

// withSignGuard keeps the phone signing surface on the local network. Even if
// the service port were forwarded by mistake, requests from public addresses
// are rejected before the token is inspected.
func (s *Server) withSignGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/sign/") {
			if !isLANRemote(r.RemoteAddr) {
				writeError(w, http.StatusForbidden, "lan_only", "签名页只允许在同一局域网内访问")
				return
			}
			w.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

// isLANRemote accepts loopback, RFC1918/ULA private and link-local addresses.
// Requests without a socket address (in-process desktop webview) are allowed,
// but the phone page is never loaded that way.
func isLANRemote(remote string) bool {
	remote = strings.TrimSpace(remote)
	if remote == "" {
		return true
	}
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	ip := net.ParseIP(strings.TrimSpace(host))
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
}

func (s *Server) withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		if strings.HasPrefix(r.URL.Path, "/sign/") {
			// The signing page is self-contained: inline styles/script, no
			// external resources, no form posts, no framing.
			w.Header().Set("Content-Security-Policy",
				"default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; "+
					"img-src data:; connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
		} else if strings.HasPrefix(r.URL.Path, "/app/") && !strings.HasPrefix(r.URL.Path, "/app/api/") {
			// PDF.js renders template pages to canvas in a same-origin worker;
			// blob: is allowed for its worker/font fallbacks only.
			w.Header().Set("Content-Security-Policy",
				"default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; "+
					"img-src 'self' data: blob:; font-src 'self' data: blob:; "+
					"connect-src 'self'; worker-src 'self' blob:; "+
					"base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
		} else if !strings.HasPrefix(r.URL.Path, "/api/") && !strings.HasPrefix(r.URL.Path, "/app/api/") {
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

// Unwrap lets http.NewResponseController reach the underlying writer. The SSE
// endpoint uses it to clear the server-wide WriteTimeout for its long-lived
// response (the deadline is absolute, so heartbeats alone cannot keep it open).
func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

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
