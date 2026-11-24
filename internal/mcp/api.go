// Package mcp implements the B6 stdio MCP connector.
//
// The connector is a local agent bridge, not a second data store: it never
// opens SQLite. Reads go through the token-protected /api/v1 surface and
// writes go through the loopback-only /app/api/v1 surface, so validation,
// contract numbering and change logging stay in internal/service. Only
// loopback HTTP targets are accepted; nothing is exposed to the network.
package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// MCPMarkerHeader identifies connector requests on the app write surface.
// The server uses it for the write switch and the agent audit actor.
const MCPMarkerHeader = "X-Tengshou-MCP"

// APIOptions configures the local HTTP client.
type APIOptions struct {
	// BaseURLs are candidate loopback endpoints. They are probed in order on
	// first use; the first healthy one is kept. The desktop app falls back to
	// the next few ports when 8765 is already taken, so callers may pass a
	// small range.
	BaseURLs []string
	// Token authenticates /api/v1 reads. It may be empty when the local
	// service runs loopback-only without a token.
	Token string
	// Client overrides the HTTP client (tests use a shorter timeout).
	Client *http.Client
	// ProbeTimeout bounds each health probe. Default 800ms.
	ProbeTimeout time.Duration
}

// API is the loopback HTTP client used by every tool.
type API struct {
	candidates   []string
	token        string
	client       *http.Client
	probeTimeout time.Duration

	mu   sync.Mutex
	base string
}

// NewAPI validates the candidate endpoints and constructs the client.
func NewAPI(opts APIOptions) (*API, error) {
	if len(opts.BaseURLs) == 0 {
		return nil, errors.New("缺少腾手本地服务地址")
	}
	seen := map[string]bool{}
	candidates := make([]string, 0, len(opts.BaseURLs))
	for _, raw := range opts.BaseURLs {
		base, err := validateLoopbackURL(raw)
		if err != nil {
			return nil, err
		}
		if !seen[base] {
			seen[base] = true
			candidates = append(candidates, base)
		}
	}
	client := opts.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	probe := opts.ProbeTimeout
	if probe <= 0 {
		probe = 800 * time.Millisecond
	}
	return &API{
		candidates:   candidates,
		token:        strings.TrimSpace(opts.Token),
		client:       client,
		probeTimeout: probe,
	}, nil
}

// validateLoopbackURL accepts only plain http URLs on a loopback host with an
// explicit port. This is the hard boundary that keeps the connector local.
func validateLoopbackURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("本地服务地址为空")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("本地服务地址必须是 http://127.0.0.1:<端口>（收到 %q）", raw)
	}
	if !strings.EqualFold(u.Scheme, "http") {
		return "", fmt.Errorf("MCP 只接受本机 http 回环地址，拒绝 %q", raw)
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", fmt.Errorf("本地服务地址不能带路径、查询或账号信息（收到 %q）", raw)
	}
	if u.Port() == "" {
		return "", fmt.Errorf("本地服务地址必须包含端口（收到 %q）", raw)
	}
	host := u.Hostname()
	if strings.EqualFold(host, "localhost") {
		return "http://localhost:" + u.Port(), nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return "", fmt.Errorf("MCP 只连接本机回环地址，拒绝 %q", raw)
	}
	return "http://" + net.JoinHostPort(host, u.Port()), nil
}

// BaseURL returns the first healthy local endpoint, caching the result.
func (a *API) BaseURL(ctx context.Context) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.base != "" {
		return a.base, nil
	}
	for _, base := range a.candidates {
		if a.healthy(ctx, base) {
			a.base = base
			return base, nil
		}
	}
	return "", fmt.Errorf(
		"无法连接腾手本地服务（已尝试 %s）。请先启动腾手桌面应用（或 tengshou-cli serve），并确认数据目录与端口一致",
		strings.Join(a.candidates, "、"))
}

func (a *API) healthy(ctx context.Context, base string) bool {
	probeCtx, cancel := context.WithTimeout(ctx, a.probeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, base+"/api/v1/health", nil)
	if err != nil {
		return false
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	var payload struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&payload); err != nil {
		return false
	}
	return payload.Status == "ok"
}

// Forget drops the cached endpoint so the next call probes again (the app may
// have restarted on another fallback port).
func (a *API) Forget() {
	a.mu.Lock()
	a.base = ""
	a.mu.Unlock()
}

// Get performs a read against /api/v1.
func (a *API) Get(ctx context.Context, path string, q url.Values) (json.RawMessage, error) {
	return a.do(ctx, http.MethodGet, path, q, nil, false)
}

// Post performs a write against /app/api/v1. The marker headers make the
// server enforce the MCP write switch and audit the agent actor.
func (a *API) Post(ctx context.Context, path string, body any) (json.RawMessage, error) {
	return a.do(ctx, http.MethodPost, path, nil, body, true)
}

// PostRead performs a read-only action that the local app surface exposes as a
// POST (generation preflight). It keeps the X-Tengshou-App header but omits the
// MCP marker, so the read tool keeps working when the MCP write switch is off
// and no agent actor is recorded for an operation that changes nothing.
func (a *API) PostRead(ctx context.Context, path string, body any) (json.RawMessage, error) {
	return a.do(ctx, http.MethodPost, path, nil, body, false)
}

func (a *API) do(ctx context.Context, method, path string, q url.Values, body any, mcpMarker bool) (json.RawMessage, error) {
	base, err := a.BaseURL(ctx)
	if err != nil {
		return nil, err
	}
	raw, err := a.send(ctx, base, method, path, q, body, mcpMarker)
	if err != nil && method == http.MethodGet && isConnectError(err) {
		// Read retries are safe; never retry a write, since the server may
		// have committed it before the connection timed out.
		a.Forget()
		if base, derr := a.BaseURL(ctx); derr == nil {
			return a.send(ctx, base, method, path, q, body, mcpMarker)
		}
	}
	return raw, err
}

func (a *API) send(ctx context.Context, base, method, path string, q url.Values, body any, mcpMarker bool) (json.RawMessage, error) {
	target := base + path
	if len(q) > 0 {
		target += "?" + q.Encode()
	}
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("编码请求失败：%w", err)
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if a.token != "" {
		req.Header.Set("Authorization", "Bearer "+a.token)
	}
	if method != http.MethodGet && method != http.MethodHead {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Tengshou-App", "1")
		if mcpMarker {
			req.Header.Set(MCPMarkerHeader, "1")
		}
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, fmt.Errorf("读取本地接口响应失败：%w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, newAPIError(resp.StatusCode, raw)
	}
	return json.RawMessage(raw), nil
}

func isConnectError(err error) bool {
	var opErr *net.OpError
	return errors.As(err, &opErr)
}

// APIError is a non-2xx answer from the local service.
type APIError struct {
	Status  int
	Code    string
	Message string
	// Detail is the raw JSON error envelope, shown to the agent for recovery.
	Detail json.RawMessage
}

func (e *APIError) Error() string {
	if e.Code != "" || e.Message != "" {
		return fmt.Sprintf("本地接口拒绝请求（HTTP %d，%s）：%s", e.Status, e.Code, e.Message)
	}
	return fmt.Sprintf("本地接口返回 HTTP %d", e.Status)
}

func newAPIError(status int, raw []byte) error {
	var payload struct {
		Error struct {
			Code    string          `json:"code"`
			Message string          `json:"message"`
			Fields  json.RawMessage `json:"fields"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &payload) == nil &&
		(payload.Error.Code != "" || payload.Error.Message != "") {
		return &APIError{
			Status:  status,
			Code:    payload.Error.Code,
			Message: payload.Error.Message,
			Detail:  append(json.RawMessage(nil), raw...),
		}
	}
	message := strings.TrimSpace(string(raw))
	if len([]rune(message)) > 300 {
		message = string([]rune(message)[:300]) + "…"
	}
	return &APIError{Status: status, Message: message, Detail: append(json.RawMessage(nil), raw...)}
}
