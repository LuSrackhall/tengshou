package httpapi_test

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"

	"github.com/LuSrackhall/tengshou/internal/model"
	"github.com/LuSrackhall/tengshou/internal/service"
	"github.com/LuSrackhall/tengshou/internal/store"
)

func (f *fixture) postMultipart(t *testing.T, path, field, filename string,
	data []byte, appHeader bool) (*http.Response, []byte) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := mw.CreateFormFile(field, filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, f.server.URL+path, &buf)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if appHeader {
		req.Header.Set("X-Tengshou-App", "1")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := readAll(resp)
	if err != nil {
		t.Fatal(err)
	}
	return resp, raw
}

func readAll(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()
	var buf bytes.Buffer
	_, err := buf.ReadFrom(resp.Body)
	return buf.Bytes(), err
}

func TestBackupExportAndRestoreEndpoints(t *testing.T) {
	f := newFixture(t)
	resp, body := f.appRequest(t, http.MethodGet, "/app/api/v1/backup/export", nil, nil)
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "application/zip" {
		t.Fatalf("export status=%d ct=%q body=%s", resp.StatusCode,
			resp.Header.Get("Content-Type"), body)
	}
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("export is not a zip: %v", err)
	}
	foundManifest := false
	for _, zf := range zr.File {
		if zf.Name == "manifest.json" {
			foundManifest = true
		}
	}
	if !foundManifest {
		t.Fatal("export has no manifest.json")
	}

	// The exported package is valid input for a staged restore.
	resp, raw := f.postMultipart(t, "/app/api/v1/backup/restore", "file", "backup.zip", body, true)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("restore status=%d body=%s", resp.StatusCode, raw)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["restart_required"] != true {
		t.Fatalf("restore payload = %s", raw)
	}
	resp, raw = f.appRequest(t, http.MethodGet, "/app/api/v1/backup/status", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.StatusCode, raw)
	}
	var status map[string]any
	if err := json.Unmarshal(raw, &status); err != nil {
		t.Fatal(err)
	}
	if status["pending"] == nil {
		t.Fatalf("staged restore not reported: %s", raw)
	}
}

func TestRestoreGuardIsNarrow(t *testing.T) {
	f := newFixture(t)
	// The multipart exception still requires the app header and loopback.
	resp, raw := f.postMultipart(t, "/app/api/v1/backup/restore", "file", "backup.zip",
		[]byte("not-a-zip"), false)
	if resp.StatusCode != http.StatusForbidden || !strings.Contains(string(raw), "app_header_required") {
		t.Fatalf("missing app header status=%d body=%s", resp.StatusCode, raw)
	}
	// Multipart is not accepted by any other write endpoint.
	resp, raw = f.postMultipart(t, "/app/api/v1/fields", "file", "x.zip", []byte("x"), true)
	if resp.StatusCode != http.StatusUnsupportedMediaType || !strings.Contains(string(raw), "json_required") {
		t.Fatalf("multipart on fields status=%d body=%s", resp.StatusCode, raw)
	}
}

func TestSignatureExportEndpoint(t *testing.T) {
	f := newFixture(t)
	png := []byte("signature-png-for-export")
	sum := sha256.Sum256(png)
	sig, err := f.service.SaveSignature(context.Background(), service.SignatureInput{
		PartyID:    f.partyBID,
		SignerName: "张三",
		ImagePNG:   png,
		SHA256:     hex.EncodeToString(sum[:]),
	})
	if err != nil {
		t.Fatalf("save signature: %v", err)
	}
	resp, raw := f.appRequest(t, http.MethodPost,
		"/app/api/v1/signatures/"+sig.ID+"/export", map[string]any{"confirm": false}, nil)
	if resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(string(raw), "confirm_required") {
		t.Fatalf("without confirmation status=%d body=%s", resp.StatusCode, raw)
	}
	resp, raw = f.appRequest(t, http.MethodPost,
		"/app/api/v1/signatures/"+sig.ID+"/export", map[string]any{"confirm": true}, nil)
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "image/png" {
		t.Fatalf("export status=%d ct=%q body=%s", resp.StatusCode,
			resp.Header.Get("Content-Type"), raw)
	}
	if !bytes.Equal(raw, png) {
		t.Fatal("exported bytes differ from the stored signature")
	}
	logs, err := f.reader.ListChangeLogs(context.Background(), store.ChangeFilter{
		Entity: model.ChangeEntitySignature, EntityID: sig.ID, Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range logs.Items {
		if entry.Action == model.ChangeActionExport {
			found = true
		}
	}
	if !found {
		t.Fatalf("signature export not audited: %+v", logs.Items)
	}
}

func TestSignatureAssetsAreNotStaticallyServed(t *testing.T) {
	f := newFixture(t)
	for _, path := range []string{
		"/assets/signatures/sig.png",
		"/assets/seals/seal.png",
		"/tengshou.db",
		"/config.json",
	} {
		resp, body := f.get(t, path, false)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("%s served with status %d: %s", path, resp.StatusCode, body)
		}
	}
}

func TestIntegrityEndpoint(t *testing.T) {
	f := newFixture(t)
	resp, raw := f.appRequest(t, http.MethodGet, "/app/api/v1/integrity", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("integrity status=%d body=%s", resp.StatusCode, raw)
	}
	var report service.IntegrityReport
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatal(err)
	}
	if !report.DatabaseOK {
		t.Fatalf("database integrity failed: %+v", report)
	}
}
