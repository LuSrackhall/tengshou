package httpapi_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LuSrackhall/tengshou/internal/model"
	"github.com/LuSrackhall/tengshou/internal/service"
	"github.com/LuSrackhall/tengshou/internal/sign"
)

func photoPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, color.NRGBA{200, uint8(x % 255), uint8(y % 255), 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func photoDataURL(t *testing.T, w, h int) string {
	t.Helper()
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(photoPNG(t, w, h))
}

func tokenForPath(t *testing.T, item map[string]any, pathPrefix string) string {
	t.Helper()
	url, _ := item["url"].(string)
	idx := strings.LastIndex(url, pathPrefix)
	if idx < 0 {
		t.Fatalf("url has no %s path: %q", pathPrefix, url)
	}
	token := url[idx+len(pathPrefix):]
	if !sign.ValidToken(token) {
		t.Fatalf("token %q is not valid", token)
	}
	return token
}

func TestAppPartyIDPhotoUploadAndServe(t *testing.T) {
	f := newFixture(t)
	resp, body := f.appRequest(t, http.MethodPost, "/app/api/v1/parties", map[string]any{
		"name": "证件照测试", "kind": "person", "id_number": "110101199001011234",
	}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create party: %d %s", resp.StatusCode, body)
	}
	party := decodeAppItem(t, body)
	id, _ := party["id"].(string)

	resp, body = f.appRequest(t, http.MethodPost, "/app/api/v1/parties/"+id+"/id-photos/front", map[string]any{
		"image": photoDataURL(t, 60, 40),
	}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("upload photo: %d %s", resp.StatusCode, body)
	}
	updated := decodeAppItem(t, body)
	if updated["id_front_path"] == "" || updated["id_front_sha256"] == "" {
		t.Fatalf("photo metadata missing: %v", updated)
	}

	resp, imageBody := f.appRequest(t, http.MethodGet, "/app/api/v1/parties/"+id+"/id-photos/front", nil, nil)
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "image/jpeg" {
		t.Fatalf("serve photo: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(imageBody))
	if err != nil || format != "jpeg" || cfg.Width != 60 || cfg.Height != 40 {
		t.Fatalf("stored photo = %dx%d %q err=%v", cfg.Width, cfg.Height, format, err)
	}

	resp, body = f.appRequest(t, http.MethodPost, "/app/api/v1/parties/"+id+"/id-photos/sideways", map[string]any{
		"image": photoDataURL(t, 60, 40),
	}, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid side should be 400: %d %s", resp.StatusCode, body)
	}
}

func TestAppPartyArchiveSignatureSession(t *testing.T) {
	f := newFixtureWithOptions(t, fixtureOptions{allowLAN: true, lanHosts: []string{"192.168.1.50"}})
	resp, body := f.appRequest(t, http.MethodPost, "/app/api/v1/parties", map[string]any{
		"name": "档案签名测试", "kind": "person", "id_number": "110101199001015678",
	}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create party: %d %s", resp.StatusCode, body)
	}
	party := decodeAppItem(t, body)
	id, _ := party["id"].(string)

	resp, body = f.appRequest(t, http.MethodPost, "/app/api/v1/parties/"+id+"/signature-sessions", nil, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create archive signature session: %d %s", resp.StatusCode, body)
	}
	session := decodeAppItem(t, body)
	if session["kind"] != "signature" || session["party_id"] != id || session["contract_id"] != "" {
		t.Fatalf("session view = %v", session)
	}
	token := tokenForPath(t, session, "/sign/")
	rec := submitSignature(t, f, token, signatureDataURL(t, false), "192.168.1.23:5000")
	if rec.Code != http.StatusOK {
		t.Fatalf("submit archive signature: %d %s", rec.Code, rec.Body.String())
	}
	stored, err := f.reader.GetParty(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if stored.SignatureID == "" {
		t.Fatalf("archive signature not attached: %+v", stored)
	}
	resp, imageBody := f.appRequest(t, http.MethodGet, "/app/api/v1/parties/"+id+"/signature", nil, nil)
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "image/png" || len(imageBody) == 0 {
		t.Fatalf("serve archive signature: %d %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
}

func TestAppEnrollSessionRequiresLAN(t *testing.T) {
	f := newFixture(t) // AllowLAN defaults to false
	resp, body := f.appRequest(t, http.MethodPost, "/app/api/v1/enroll-sessions", nil, nil)
	if resp.StatusCode != http.StatusConflict || errorCodeRecorder(t, resp, body) != "lan_required" {
		t.Fatalf("enroll without LAN: %d %s", resp.StatusCode, body)
	}
}

func errorCodeRecorder(t *testing.T, resp *http.Response, body []byte) string {
	t.Helper()
	var payload struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode error payload %q: %v", body, err)
	}
	return payload.Error.Code
}

func TestAppEnrollSelfServiceFlow(t *testing.T) {
	f := newFixtureWithOptions(t, fixtureOptions{allowLAN: true, lanHosts: []string{"192.168.1.50"}})
	resp, body := f.appRequest(t, http.MethodPost, "/app/api/v1/enroll-sessions", nil, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create enroll session: %d %s", resp.StatusCode, body)
	}
	session := decodeAppItem(t, body)
	if session["kind"] != "enroll" {
		t.Fatalf("session = %v", session)
	}
	token := tokenForPath(t, session, "/enroll/")

	// The phone page is LAN-only and token-gated.
	public := signRaw(t, f, http.MethodGet, "/enroll/"+token, nil, "203.0.113.9:4444", nil)
	if public.Code != http.StatusForbidden {
		t.Fatalf("public enroll page = %d", public.Code)
	}
	page := signRaw(t, f, http.MethodGet, "/enroll/"+token, nil, "192.168.1.23:5000", nil)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "租户自助建档") {
		t.Fatalf("enroll page = %d %s", page.Code, page.Body.String())
	}
	// The enrollment page must be as locked down as the signing page: inline
	// script/style only, images from data URLs, no form posts, no framing.
	csp := page.Header().Get("Content-Security-Policy")
	for _, want := range []string{"default-src 'none'", "script-src 'unsafe-inline'", "img-src data:", "form-action 'none'", "frame-ancestors 'none'"} {
		if !strings.Contains(csp, want) {
			t.Fatalf("enroll page CSP = %q, missing %q", csp, want)
		}
	}
	if got := page.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("enroll page cache = %q", got)
	}

	submit := func(name, idNumber string) *httptest.ResponseRecorder {
		payload, err := json.Marshal(map[string]any{
			"name":      name,
			"id_number": idNumber,
			"phone":     "13800001111",
			"id_front":  photoDataURL(t, 50, 30),
			"id_back":   photoDataURL(t, 50, 30),
			"signature": signatureDataURL(t, false),
		})
		if err != nil {
			t.Fatal(err)
		}
		return signRaw(t, f, http.MethodPost, "/enroll/"+token+"/submit", payload, "192.168.1.23:5000",
			map[string]string{"Content-Type": "application/json", "X-Tengshou-Sign": "1"})
	}

	rec := submit("自助租户", "330102198001010011")
	if rec.Code != http.StatusOK {
		t.Fatalf("enroll submit: %d %s", rec.Code, rec.Body.String())
	}
	matches, err := f.reader.FindPartiesByName(context.Background(), "自助租户")
	if err != nil || len(matches) != 1 {
		t.Fatalf("enrolled party = %v err=%v", matches, err)
	}
	if matches[0].IDFrontPath == "" || matches[0].IDBackPath == "" || matches[0].SignatureID == "" {
		t.Fatalf("enrolled assets missing: %+v", matches[0])
	}
	if matches[0].Kind != model.PartyPerson {
		t.Fatalf("default enrolled kind = %q, want person", matches[0].Kind)
	}

	// The one-time token cannot be replayed.
	if replay := submit("自助租户", "330102198001010011"); replay.Code != http.StatusConflict {
		t.Fatalf("replay = %d %s", replay.Code, replay.Body.String())
	}
}

func TestAppEnrollRejectsDuplicateIDThenAcceptsCorrection(t *testing.T) {
	f := newFixtureWithOptions(t, fixtureOptions{allowLAN: true, lanHosts: []string{"192.168.1.50"}})
	resp, body := f.appRequest(t, http.MethodPost, "/app/api/v1/parties", map[string]any{
		"name": "已有档案", "kind": "person", "id_number": "330102198001010022",
	}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("seed party: %d %s", resp.StatusCode, body)
	}
	resp, body = f.appRequest(t, http.MethodPost, "/app/api/v1/enroll-sessions", nil, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create enroll session: %d %s", resp.StatusCode, body)
	}
	session := decodeAppItem(t, body)
	token := tokenForPath(t, session, "/enroll/")

	submit := func(idNumber string) *httptest.ResponseRecorder {
		payload, err := json.Marshal(map[string]any{
			"name":      "重复证件号",
			"id_number": idNumber,
			"id_front":  photoDataURL(t, 30, 20),
			"id_back":   photoDataURL(t, 30, 20),
			"signature": signatureDataURL(t, false),
		})
		if err != nil {
			t.Fatal(err)
		}
		return signRaw(t, f, http.MethodPost, "/enroll/"+token+"/submit", payload, "192.168.1.23:5000",
			map[string]string{"Content-Type": "application/json", "X-Tengshou-Sign": "1"})
	}

	rec := submit("330102198001010022")
	if rec.Code != http.StatusConflict || errorCode(t, rec) != "duplicate_id_number" {
		t.Fatalf("duplicate submit = %d %s", rec.Code, rec.Body.String())
	}
	// Duplicate ID is a recoverable error: the same one-time link stays usable.
	status := signRaw(t, f, http.MethodGet, "/enroll/"+token+"/status", nil, "192.168.1.23:5000", nil)
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"pending"`) {
		t.Fatalf("status after duplicate = %d %s", status.Code, status.Body.String())
	}
	if rec := submit("330102198001010033"); rec.Code != http.StatusOK {
		t.Fatalf("corrected submit = %d %s", rec.Code, rec.Body.String())
	}
	stored, err := f.reader.FindPartiesByIDNumber(context.Background(), "330102198001010033")
	if err != nil || len(stored) != 1 {
		t.Fatalf("corrected party = %v err=%v", stored, err)
	}
	if stored[0].Kind != model.PartyPerson {
		t.Fatalf("enrolled kind = %q", stored[0].Kind)
	}
}

// te-i11we: the phone page lets the tenant choose person/organization. A unit
// tenant must be stored as an organization (previously every self-enrolled
// archive showed “个人”), Chinese labels are accepted, and an unknown value is
// a field validation error that leaves the one-time link usable.
func TestAppEnrollSubmitKindOrganization(t *testing.T) {
	f := newFixtureWithOptions(t, fixtureOptions{allowLAN: true, lanHosts: []string{"192.168.1.50"}})
	resp, body := f.appRequest(t, http.MethodPost, "/app/api/v1/enroll-sessions", nil, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create enroll session: %d %s", resp.StatusCode, body)
	}
	session := decodeAppItem(t, body)
	token := tokenForPath(t, session, "/enroll/")

	submit := func(kind, name, idNumber string) *httptest.ResponseRecorder {
		payload, err := json.Marshal(map[string]any{
			"kind":      kind,
			"name":      name,
			"id_number": idNumber,
			"id_front":  photoDataURL(t, 30, 20),
			"id_back":   photoDataURL(t, 30, 20),
			"signature": signatureDataURL(t, false),
		})
		if err != nil {
			t.Fatal(err)
		}
		return signRaw(t, f, http.MethodPost, "/enroll/"+token+"/submit", payload, "192.168.1.23:5000",
			map[string]string{"Content-Type": "application/json", "X-Tengshou-Sign": "1"})
	}

	invalid := submit("company", "单位租户", "91330100MA2ENROLL02")
	if invalid.Code != http.StatusUnprocessableEntity || errorCode(t, invalid) != "validation" {
		t.Fatalf("invalid kind = %d %s", invalid.Code, invalid.Body.String())
	}
	// The one-time link stays usable after a field validation error.
	rec := submit("单位", "单位租户", "91330100MA2ENROLL02")
	if rec.Code != http.StatusOK {
		t.Fatalf("organization submit = %d %s", rec.Code, rec.Body.String())
	}
	stored, err := f.reader.FindPartiesByIDNumber(context.Background(), "91330100MA2ENROLL02")
	if err != nil || len(stored) != 1 {
		t.Fatalf("enrolled party = %v err=%v", stored, err)
	}
	if stored[0].Kind != model.PartyOrganization {
		t.Fatalf("enrolled kind = %q, want organization", stored[0].Kind)
	}
}

// v0.2 D3=B: the "use archive signature" endpoint stamps the archive
// signature as-is (no contract content version), is idempotent across content
// edits, and refuses parties without an archive signature or outside the
// contract.
func TestAppReusePartySignature(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	raw := photoPNG(t, 12, 6)
	sum := sha256.Sum256(raw)
	party, _, err := f.service.SaveParty(ctx, service.PartyInput{
		Kind: model.PartyPerson, Name: "复用签名租户", IDNumber: "110101199001018888", RequireIDNumber: true,
	})
	if err != nil {
		t.Fatalf("seed party: %v", err)
	}
	archive, err := f.service.SaveSignature(ctx, service.SignatureInput{
		PartyID: party.ID, SignerName: party.Name, ImagePNG: raw,
		SHA256: hex.EncodeToString(sum[:]), WidthPx: 12, HeightPx: 6,
	})
	if err != nil {
		t.Fatalf("seed archive signature: %v", err)
	}
	if err := f.service.AttachPartySignature(ctx, party.ID, archive.ID); err != nil {
		t.Fatalf("attach archive signature: %v", err)
	}
	contract, _, err := f.service.SaveContract(ctx, service.ContractInput{Title: "复用签名合同", PartyBID: party.ID})
	if err != nil {
		t.Fatalf("seed contract: %v", err)
	}

	resp, body := f.appRequest(t, http.MethodPost,
		"/app/api/v1/contracts/"+contract.ID+"/signature-reuse",
		map[string]any{"party_id": party.ID}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("reuse signature: %d %s", resp.StatusCode, body)
	}
	item := decodeAppItem(t, body)
	if item["source"] != model.SignatureSourceArchive {
		t.Fatalf("reused source = %v", item["source"])
	}
	// Archive reuse is deliberately not bound to a contract content version.
	if item["content_sha256"] != nil && item["content_sha256"] != "" {
		t.Fatalf("reused signature should not bind a content version: %v", item)
	}
	if item["contract_id"] != contract.ID || item["party_id"] != party.ID {
		t.Fatalf("reused signature links = %v", item)
	}
	firstID, _ := item["id"].(string)
	if firstID == "" {
		t.Fatalf("reused signature has no id: %v", item)
	}

	// Reusing the same archive signature for the same content version is
	// idempotent: the same row comes back instead of a duplicate.
	resp, body = f.appRequest(t, http.MethodPost,
		"/app/api/v1/contracts/"+contract.ID+"/signature-reuse",
		map[string]any{"party_id": party.ID}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("reuse signature again: %d %s", resp.StatusCode, body)
	}
	if again := decodeAppItem(t, body); again["id"] != firstID {
		t.Fatalf("reuse should be idempotent: %v vs %s", again["id"], firstID)
	}

	// Editing the contract content later must not invalidate or duplicate the
	// reuse row (v0.2 D3=B).
	if _, _, err := f.service.SaveContract(ctx, service.ContractInput{
		ID: contract.ID, ContractNo: contract.ContractNo, Year: contract.Year,
		Title: "复用签名合同（改名）", PartyBID: party.ID, Amount: contract.Amount,
	}); err != nil {
		t.Fatalf("update contract: %v", err)
	}
	resp, body = f.appRequest(t, http.MethodPost,
		"/app/api/v1/contracts/"+contract.ID+"/signature-reuse",
		map[string]any{"party_id": party.ID}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("reuse after content change: %d %s", resp.StatusCode, body)
	}
	if afterChange := decodeAppItem(t, body); afterChange["id"] != firstID {
		t.Fatalf("reuse should stay idempotent across content changes: %v vs %s", afterChange["id"], firstID)
	}

	// A party without an archive signature is refused with a business code.
	other, _, err := f.service.SaveParty(ctx, service.PartyInput{
		Kind: model.PartyPerson, Name: "无签名租户", IDNumber: "110101199001019999", RequireIDNumber: true,
	})
	if err != nil {
		t.Fatalf("seed second party: %v", err)
	}
	otherContract, _, err := f.service.SaveContract(ctx, service.ContractInput{Title: "无签名合同", PartyBID: other.ID})
	if err != nil {
		t.Fatalf("seed second contract: %v", err)
	}
	resp, body = f.appRequest(t, http.MethodPost,
		"/app/api/v1/contracts/"+otherContract.ID+"/signature-reuse",
		map[string]any{"party_id": other.ID}, nil)
	if resp.StatusCode != http.StatusUnprocessableEntity || errorCodeRecorder(t, resp, body) != "missing_signature" {
		t.Fatalf("party without archive signature: %d %s", resp.StatusCode, body)
	}

	// A party outside the contract cannot have its signature reused.
	resp, body = f.appRequest(t, http.MethodPost,
		"/app/api/v1/contracts/"+contract.ID+"/signature-reuse",
		map[string]any{"party_id": other.ID}, nil)
	if resp.StatusCode != http.StatusBadRequest || errorCodeRecorder(t, resp, body) != "invalid_party" {
		t.Fatalf("foreign party reuse: %d %s", resp.StatusCode, body)
	}
}

// endlessReader yields filler bytes forever; io.LimitReader bounds it so an
// oversized request body can be tested without allocating it in memory.
type endlessReader struct{ filler byte }

func (r endlessReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = r.filler
	}
	return len(p), nil
}

// signRawBody is signRaw with a streaming body, for size-limit tests.
func signRawBody(t *testing.T, f *fixture, method, path string, body io.Reader, remote string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, body)
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

// v0.2: the enrollment submission body is bounded (96 MiB); an oversized body
// is rejected before the one-time token is claimed, so the link stays usable.
func TestAppEnrollRejectsOversizedSubmit(t *testing.T) {
	const enrollBodyLimit = 96 << 20 // 与 partyapi.go 的 maxEnrollBody 一致
	f := newFixtureWithOptions(t, fixtureOptions{allowLAN: true, lanHosts: []string{"192.168.1.50"}})
	resp, body := f.appRequest(t, http.MethodPost, "/app/api/v1/enroll-sessions", nil, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create enroll session: %d %s", resp.StatusCode, body)
	}
	token := tokenForPath(t, decodeAppItem(t, body), "/enroll/")

	oversized := io.LimitReader(endlessReader{filler: 'a'}, enrollBodyLimit+1)
	rec := signRawBody(t, f, http.MethodPost, "/enroll/"+token+"/submit", oversized, "192.168.1.23:5000",
		map[string]string{"Content-Type": "application/json", "X-Tengshou-Sign": "1"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("oversized enroll submit = %d %s", rec.Code, rec.Body.String())
	}

	// Rejection happens before Claim: the one-time link still works.
	status := signRaw(t, f, http.MethodGet, "/enroll/"+token+"/status", nil, "192.168.1.23:5000", nil)
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"pending"`) {
		t.Fatalf("enroll session after oversized submit = %d %s", status.Code, status.Body.String())
	}
}

// v0.2: one ID-card photo upload is bounded (24 MiB); an oversized body is
// rejected without touching the archive.
func TestAppPartyPhotoRejectsOversizedUpload(t *testing.T) {
	const photoBodyLimit = 24 << 20 // 与 partyapi.go 的 maxPartyPhotoBody 一致
	f := newFixture(t)
	resp, body := f.appRequest(t, http.MethodPost, "/app/api/v1/parties", map[string]any{
		"name": "超大照片测试", "kind": "person", "id_number": "110101199001017777",
	}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create party: %d %s", resp.StatusCode, body)
	}
	party := decodeAppItem(t, body)
	id, _ := party["id"].(string)

	oversized := io.LimitReader(endlessReader{filler: 'a'}, photoBodyLimit+1)
	rec := signRawBody(t, f, http.MethodPost, "/app/api/v1/parties/"+id+"/id-photos/front", oversized, "127.0.0.1:5000",
		map[string]string{"Content-Type": "application/json", "X-Tengshou-App": "1"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("oversized photo upload = %d %s", rec.Code, rec.Body.String())
	}
	stored, err := f.reader.GetParty(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if stored.IDFrontPath != "" {
		t.Fatalf("oversized upload left a photo behind: %q", stored.IDFrontPath)
	}
}
