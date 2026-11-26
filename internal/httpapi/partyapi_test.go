package httpapi_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LuSrackhall/tengshou/internal/model"
	"github.com/LuSrackhall/tengshou/internal/sign"
)

func photoDataURL(t *testing.T, w, h int) string {
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
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
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
