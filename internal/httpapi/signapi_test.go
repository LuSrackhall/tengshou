package httpapi_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LuSrackhall/tengshou/internal/model"
	"github.com/LuSrackhall/tengshou/internal/sign"
)

func signRaw(t *testing.T, f *fixture, method, path string, body []byte, remote string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	if remote != "" {
		req.RemoteAddr = remote
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	return rec
}

func createSignSession(t *testing.T, f *fixture, body map[string]any) map[string]any {
	t.Helper()
	resp, raw := f.appRequest(t, http.MethodPost, "/app/api/v1/signature-sessions", body, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create session: %d %s", resp.StatusCode, raw)
	}
	return decodeAppItem(t, raw)
}

func tokenFromURL(t *testing.T, item map[string]any) string {
	t.Helper()
	url, _ := item["url"].(string)
	idx := strings.LastIndex(url, "/sign/")
	if idx < 0 {
		t.Fatalf("url has no /sign/ path: %q", url)
	}
	token := url[idx+len("/sign/"):]
	if !sign.ValidToken(token) {
		t.Fatalf("token %q is not valid", token)
	}
	return token
}

func drawTestSignature(img *image.NRGBA) {
	ink := color.NRGBA{0, 0, 0, 255}
	for x := 100; x < 760; x++ {
		y := 240 + int(70*math.Sin(float64(x)/38))
		for dy := -3; dy <= 3; dy++ {
			for dx := -3; dx <= 3; dx++ {
				if dx*dx+dy*dy > 9 {
					continue
				}
				img.SetNRGBA(x+dx, y+dy, ink)
			}
		}
	}
	for x := 90; x < 820; x++ {
		for dy := -1; dy <= 1; dy++ {
			img.SetNRGBA(x, 420+dy, ink)
		}
	}
}

func signatureDataURL(t *testing.T, faint bool) string {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 1200, 480))
	if faint {
		for i := 0; i < 20; i++ {
			img.SetNRGBA(200+i, 200, color.NRGBA{0, 0, 0, 20})
		}
	} else {
		drawTestSignature(img)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

func submitSignature(t *testing.T, f *fixture, token, dataURL, remote string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]any{"image": dataURL})
	if err != nil {
		t.Fatal(err)
	}
	return signRaw(t, f, http.MethodPost, "/sign/"+token+"/submit", body, remote, map[string]string{
		"Content-Type":    "application/json",
		"X-Tengshou-Sign": "1",
	})
}

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var payload struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode error payload %q: %v", rec.Body.String(), err)
	}
	return payload.Error.Code
}

func TestSignatureSessionFlow(t *testing.T) {
	f := newFixtureWithOptions(t, fixtureOptions{
		allowLAN: true,
		lanHosts: []string{"192.168.1.50"},
	})
	item := createSignSession(t, f, map[string]any{
		"contract_id": f.contractID,
		"party_id":    f.partyBID,
	})
	if item["state"] != "pending" || item["signer_name"] != "张三" {
		t.Fatalf("session item = %+v", item)
	}
	if item["contract_id"] != f.contractID || item["content_sha256"] == "" {
		t.Fatalf("session link = %+v", item)
	}
	if url, _ := item["url"].(string); !strings.HasPrefix(url, "http://192.168.1.50:8765/sign/") {
		t.Fatalf("url = %q", url)
	}
	token := tokenFromURL(t, item)
	sessionID, _ := item["id"].(string)

	// The QR image is a real PNG.
	qrPath, _ := item["qr_url"].(string)
	qr := signRaw(t, f, http.MethodGet, qrPath, nil, "127.0.0.1:5000", nil)
	if qr.Code != http.StatusOK || qr.Header().Get("Content-Type") != "image/png" ||
		!bytes.HasPrefix(qr.Body.Bytes(), []byte("\x89PNG")) {
		t.Fatalf("qr: %d %s", qr.Code, qr.Header().Get("Content-Type"))
	}

	// LAN devices see the signing page; public addresses do not.
	page := signRaw(t, f, http.MethodGet, "/sign/"+token, nil, "192.168.1.23:5000", nil)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "请在此处手写签名") ||
		!strings.Contains(page.Body.String(), "张三") {
		t.Fatalf("sign page: %d %s", page.Code, page.Body.String())
	}
	if csp := page.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "script-src 'unsafe-inline'") {
		t.Fatalf("sign page CSP = %q", csp)
	}
	if page.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("sign page cache = %q", page.Header().Get("Cache-Control"))
	}
	public := signRaw(t, f, http.MethodGet, "/sign/"+token, nil, "203.0.113.9:4444", nil)
	if public.Code != http.StatusForbidden || errorCode(t, public) != "lan_only" {
		t.Fatalf("public page: %d %s", public.Code, public.Body.String())
	}

	status := signRaw(t, f, http.MethodGet, "/sign/"+token+"/status", nil, "192.168.1.23:5000", nil)
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"pending"`) {
		t.Fatalf("status: %d %s", status.Code, status.Body.String())
	}

	// A too-light stroke is rejected but does not consume the token.
	light := submitSignature(t, f, token, signatureDataURL(t, true), "192.168.1.23:5000")
	if light.Code != http.StatusUnprocessableEntity || errorCode(t, light) != "sign_too_light" {
		t.Fatalf("too light: %d %s", light.Code, light.Body.String())
	}
	if got := signRaw(t, f, http.MethodGet, "/sign/"+token+"/status", nil, "192.168.1.23:5000", nil); !strings.Contains(got.Body.String(), `"pending"`) {
		t.Fatalf("token consumed after too-light: %s", got.Body.String())
	}

	ok := submitSignature(t, f, token, signatureDataURL(t, false), "192.168.1.23:5000")
	if ok.Code != http.StatusOK || !strings.Contains(ok.Body.String(), `"completed"`) {
		t.Fatalf("submit: %d %s", ok.Code, ok.Body.String())
	}
	// The token is one-time: a retry is refused.
	reuse := submitSignature(t, f, token, signatureDataURL(t, false), "192.168.1.23:5000")
	if reuse.Code != http.StatusConflict || errorCode(t, reuse) != "sign_consumed" {
		t.Fatalf("reuse: %d %s", reuse.Code, reuse.Body.String())
	}
	status = signRaw(t, f, http.MethodGet, "/sign/"+token+"/status", nil, "192.168.1.23:5000", nil)
	if !strings.Contains(status.Body.String(), `"completed"`) {
		t.Fatalf("completed status: %s", status.Body.String())
	}
	page = signRaw(t, f, http.MethodGet, "/sign/"+token, nil, "192.168.1.23:5000", nil)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "签名已提交") {
		t.Fatalf("completed page: %d %s", page.Code, page.Body.String())
	}

	// Poll from the PC sees the stored signature.
	resp, raw := f.appRequest(t, http.MethodGet, "/app/api/v1/signature-sessions/"+sessionID, nil, nil)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(raw), `"signature_id"`) {
		t.Fatalf("session detail: %d %s", resp.StatusCode, raw)
	}

	// The asset is linked to the contract and content version and is on disk.
	resp, raw = f.appRequest(t, http.MethodGet, "/app/api/v1/contracts/"+f.contractID, nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("contract detail: %d %s", resp.StatusCode, raw)
	}
	var detail model.ContractDetail
	if err := json.Unmarshal(raw, &detail); err != nil {
		t.Fatal(err)
	}
	var captured *model.Signature
	for i := range detail.Signatures {
		if detail.Signatures[i].Source == model.SignatureSourceLANQR {
			captured = &detail.Signatures[i]
		}
	}
	if captured == nil {
		t.Fatalf("captured signature missing from detail: %+v", detail.Signatures)
	}
	if captured.ContractID != f.contractID || captured.ContentSHA256 == "" ||
		captured.WidthPx == 0 || captured.HeightPx == 0 {
		t.Fatalf("captured signature links = %+v", captured)
	}
	if _, err := os.Stat(filepath.Join(f.dataDir, filepath.FromSlash(captured.AssetPath))); err != nil {
		t.Fatalf("signature file missing: %v", err)
	}

	resp, raw = f.get(t, "/api/v1/signature-assets?contract_id="+f.contractID, true)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("signature list: %d %s", resp.StatusCode, raw)
	}
	var list model.List[model.Signature]
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatal(err)
	}
	if list.Total != 1 || list.Items[0].ID != captured.ID {
		t.Fatalf("signature list = %+v", list)
	}

	// A later contract edit is reported as content_changed so the PC can warn
	// that the stored signature belongs to an older content version.
	resp, raw = f.appRequest(t, http.MethodPut, "/app/api/v1/contracts/"+f.contractID, map[string]any{
		"year": 2026, "title": "幸福村房屋租赁合同（修订）", "party_b_id": f.partyBID,
	}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update contract: %d %s", resp.StatusCode, raw)
	}
	resp, raw = f.appRequest(t, http.MethodGet, "/app/api/v1/signature-sessions/"+sessionID, nil, nil)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(raw), `"content_changed":true`) {
		t.Fatalf("content change not detected: %d %s", resp.StatusCode, raw)
	}
}

func TestSignatureSessionExpiryAndCancel(t *testing.T) {
	now := time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)
	mgr := sign.NewManager(time.Minute, func() time.Time { return now })
	f := newFixtureWithOptions(t, fixtureOptions{
		allowLAN: true,
		lanHosts: []string{"192.168.1.50"},
		sign:     mgr,
	})

	item := createSignSession(t, f, map[string]any{"contract_id": f.contractID})
	token := tokenFromURL(t, item)
	now = now.Add(2 * time.Minute)
	page := signRaw(t, f, http.MethodGet, "/sign/"+token, nil, "192.168.1.23:5000", nil)
	if page.Code != http.StatusGone || !strings.Contains(page.Body.String(), "链接已失效") {
		t.Fatalf("expired page: %d %s", page.Code, page.Body.String())
	}
	expired := submitSignature(t, f, token, signatureDataURL(t, false), "192.168.1.23:5000")
	if expired.Code != http.StatusGone || errorCode(t, expired) != "sign_expired" {
		t.Fatalf("expired submit: %d %s", expired.Code, expired.Body.String())
	}

	// Cancelling from the PC invalidates the link immediately.
	item = createSignSession(t, f, map[string]any{"contract_id": f.contractID})
	token = tokenFromURL(t, item)
	id, _ := item["id"].(string)
	resp, raw := f.appRequest(t, http.MethodDelete, "/app/api/v1/signature-sessions/"+id, nil, nil)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(raw), `"cancelled":true`) {
		t.Fatalf("cancel: %d %s", resp.StatusCode, raw)
	}
	page = signRaw(t, f, http.MethodGet, "/sign/"+token, nil, "192.168.1.23:5000", nil)
	if page.Code != http.StatusGone {
		t.Fatalf("cancelled page: %d", page.Code)
	}
}

func TestSignatureSessionValidationAndLANRequirement(t *testing.T) {
	// Loopback-only mode cannot sign: phones would not be able to reach it.
	f := newFixture(t)
	resp, raw := f.appRequest(t, http.MethodPost, "/app/api/v1/signature-sessions",
		map[string]any{"contract_id": f.contractID}, nil)
	if resp.StatusCode != http.StatusConflict || !strings.Contains(string(raw), "lan_required") {
		t.Fatalf("loopback mode: %d %s", resp.StatusCode, raw)
	}

	lan := newFixtureWithOptions(t, fixtureOptions{allowLAN: true, lanHosts: []string{"192.168.1.50"}})
	resp, raw = lan.appRequest(t, http.MethodPost, "/app/api/v1/signature-sessions",
		map[string]any{"contract_id": "ctr_missing"}, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("missing contract: %d %s", resp.StatusCode, raw)
	}
	resp, raw = lan.appRequest(t, http.MethodPost, "/app/api/v1/signature-sessions",
		map[string]any{"contract_id": lan.contractID, "party_id": "pty_other"}, nil)
	if resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(string(raw), "invalid_signer") {
		t.Fatalf("foreign party: %d %s", resp.StatusCode, raw)
	}
	// Public submissions never reach the token check.
	public := signRaw(t, lan, http.MethodPost, "/sign/"+strings.Repeat("a", 64)+"/submit",
		[]byte(`{"image":""}`), "203.0.113.9:4444", map[string]string{"Content-Type": "application/json"})
	if public.Code != http.StatusForbidden || errorCode(t, public) != "lan_only" {
		t.Fatalf("public submit: %d %s", public.Code, public.Body.String())
	}
}

// v0.2: signature and enrollment sessions share the one-time token model but
// each endpoint validates its own session kind, so a token from one flow can
// neither render, submit, nor cancel in the other.
func TestSessionKindSeparation(t *testing.T) {
	f := newFixtureWithOptions(t, fixtureOptions{allowLAN: true, lanHosts: []string{"192.168.1.50"}})

	resp, body := f.appRequest(t, http.MethodPost, "/app/api/v1/enroll-sessions", nil, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create enroll session: %d %s", resp.StatusCode, body)
	}
	enrollSession := decodeAppItem(t, body)
	enrollID, _ := enrollSession["id"].(string)
	enrollToken := tokenForPath(t, enrollSession, "/enroll/")

	// The signature phone surface refuses an enrollment token.
	page := signRaw(t, f, http.MethodGet, "/sign/"+enrollToken, nil, "192.168.1.23:5000", nil)
	if page.Code != http.StatusGone {
		t.Fatalf("enroll token on sign page = %d %s", page.Code, page.Body.String())
	}
	status := signRaw(t, f, http.MethodGet, "/sign/"+enrollToken+"/status", nil, "192.168.1.23:5000", nil)
	if status.Code != http.StatusNotFound || errorCode(t, status) != "not_found" {
		t.Fatalf("enroll token on sign status = %d %s", status.Code, status.Body.String())
	}
	submit := submitSignature(t, f, enrollToken, signatureDataURL(t, false), "192.168.1.23:5000")
	if submit.Code != http.StatusNotFound || errorCode(t, submit) != "not_found" {
		t.Fatalf("enroll token on sign submit = %d %s", submit.Code, submit.Body.String())
	}
	// The refused submit did not claim the enrollment session.
	enrollStatus := signRaw(t, f, http.MethodGet, "/enroll/"+enrollToken+"/status", nil, "192.168.1.23:5000", nil)
	if enrollStatus.Code != http.StatusOK || !strings.Contains(enrollStatus.Body.String(), `"pending"`) {
		t.Fatalf("enroll session after cross-kind submit = %d %s", enrollStatus.Code, enrollStatus.Body.String())
	}
	// The signature cancel endpoint refuses the enrollment session.
	resp, body = f.appRequest(t, http.MethodDelete, "/app/api/v1/signature-sessions/"+enrollID, nil, nil)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), `"cancelled":false`) {
		t.Fatalf("signature cancel on enroll session = %d %s", resp.StatusCode, body)
	}

	// Symmetrically, the enrollment surface refuses a signature token.
	signSession := createSignSession(t, f, map[string]any{"contract_id": f.contractID})
	signID, _ := signSession["id"].(string)
	signToken := tokenFromURL(t, signSession)
	enrollPage := signRaw(t, f, http.MethodGet, "/enroll/"+signToken, nil, "192.168.1.23:5000", nil)
	if enrollPage.Code != http.StatusGone {
		t.Fatalf("sign token on enroll page = %d %s", enrollPage.Code, enrollPage.Body.String())
	}
	enrollStatus = signRaw(t, f, http.MethodGet, "/enroll/"+signToken+"/status", nil, "192.168.1.23:5000", nil)
	if enrollStatus.Code != http.StatusNotFound {
		t.Fatalf("sign token on enroll status = %d %s", enrollStatus.Code, enrollStatus.Body.String())
	}
	resp, body = f.appRequest(t, http.MethodDelete, "/app/api/v1/enroll-sessions/"+signID, nil, nil)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), `"cancelled":false`) {
		t.Fatalf("enroll cancel on signature session = %d %s", resp.StatusCode, body)
	}

	// Both sessions stayed usable: their own endpoints can cancel them.
	resp, body = f.appRequest(t, http.MethodDelete, "/app/api/v1/enroll-sessions/"+enrollID, nil, nil)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), `"cancelled":true`) {
		t.Fatalf("enroll cancel = %d %s", resp.StatusCode, body)
	}
	resp, body = f.appRequest(t, http.MethodDelete, "/app/api/v1/signature-sessions/"+signID, nil, nil)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), `"cancelled":true`) {
		t.Fatalf("signature cancel = %d %s", resp.StatusCode, body)
	}
}
