package httpapi_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LuSrackhall/tengshou/internal/config"
	"github.com/LuSrackhall/tengshou/internal/httpapi"
	"github.com/LuSrackhall/tengshou/internal/model"
	"github.com/LuSrackhall/tengshou/internal/service"
	"github.com/LuSrackhall/tengshou/internal/sign"
	"github.com/LuSrackhall/tengshou/internal/store"
)

const token = "test-token"

type fixture struct {
	server     *httptest.Server
	handler    http.Handler
	partyID    string
	partyBID   string
	contractID string
	dataDir    string
	service    *service.Service
}

type fixtureOptions struct {
	allowLAN bool
	lanHosts []string
	sign     *sign.Manager
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	return newFixtureWithOptions(t, fixtureOptions{})
}

func newFixtureWithOptions(t *testing.T, opts fixtureOptions) *fixture {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	reader, err := st.Reader()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reader.Close() })

	ctx := context.Background()
	partyA := model.Party{Kind: model.PartyOrganization, Name: "幸福村合作社"}
	partyB := model.Party{Kind: model.PartyPerson, Name: "张三", Phone: "13800000002"}
	if err := st.PutParty(ctx, &partyA); err != nil {
		t.Fatal(err)
	}
	if err := st.PutParty(ctx, &partyB); err != nil {
		t.Fatal(err)
	}
	tpl := model.Template{Name: "租赁模板", Version: 1, HTML: "<p>{{.Data.rent}}</p>", Variables: []string{"rent"}, IsActive: true}
	if err := st.PutTemplate(ctx, &tpl); err != nil {
		t.Fatal(err)
	}
	contract := model.Contract{Title: "幸福村房屋租赁合同", PartyAID: partyA.ID, PartyBID: partyB.ID,
		TemplateID: tpl.ID, Status: model.ContractActive, Data: map[string]any{"rent": 1200.0}}
	if err := st.PutContract(ctx, &contract); err != nil {
		t.Fatal(err)
	}
	sig := model.Signature{PartyID: partyB.ID, SignerName: "张三", AssetPath: "assets/sig.png", SHA256: "abc", WidthPx: 10, HeightPx: 5}
	if err := st.PutSignature(ctx, &sig); err != nil {
		t.Fatal(err)
	}
	doc := model.OutputDocument{ContractID: contract.ID, TemplateID: tpl.ID, TemplateVersion: 1,
		FilePath: "outputs/a.pdf", SHA256: "def", PageCount: 2}
	if err := st.PutOutputDocument(ctx, &doc); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	cfg.HTTP.Token = token
	cfg.HTTP.Port = 8765
	if opts.allowLAN {
		cfg.HTTP.AllowLAN = true
		cfg.HTTP.Bind = "0.0.0.0"
	} else {
		cfg.HTTP.AllowLAN = false
		cfg.HTTP.Bind = "127.0.0.1"
	}
	svc := service.New(st, reader, "tester")
	dataDir := t.TempDir()
	svc.SetDataDir(dataDir)
	server := httpapi.New(httpapi.Options{
		Reader:   reader,
		Service:  svc,
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		Config:   cfg,
		Version:  "test",
		Sign:     opts.sign,
		LANHosts: opts.lanHosts,
	})
	ts := httptest.NewServer(server.Handler())
	t.Cleanup(ts.Close)
	return &fixture{server: ts, handler: server.Handler(), partyID: partyA.ID,
		partyBID: partyB.ID, contractID: contract.ID, dataDir: dataDir, service: svc}
}

func (f *fixture) get(t *testing.T, path string, auth bool) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, f.server.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if auth {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	return resp, body
}

func TestHealthIsPublic(t *testing.T) {
	f := newFixture(t)
	resp, body := f.get(t, "/api/v1/health", false)
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["status"] != "ok" || payload["version"] != "test" {
		t.Fatalf("payload = %v", payload)
	}
}

func TestIndexPage(t *testing.T) {
	f := newFixture(t)
	resp, body := f.get(t, "/", false)
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if !strings.Contains(string(body), "腾手") || !strings.Contains(string(body), "本地服务运行中") {
		t.Fatalf("unexpected index: %s", body)
	}
	if resp.Header.Get("Content-Security-Policy") == "" {
		t.Fatal("missing CSP header")
	}
	f.get(t, "/missing", false)
}

func TestDataEndpointsRequireToken(t *testing.T) {
	f := newFixture(t)
	for _, path := range []string{"/api/v1/parties", "/api/v1/contracts", "/api/v1/templates", "/api/v1/stats", "/api/v1/schema", "/api/v1/fields"} {
		resp, body := f.get(t, path, false)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%s without token: status %d body=%s", path, resp.StatusCode, body)
		}
	}
}

func TestReadOnlyMethodsRejected(t *testing.T) {
	f := newFixture(t)
	req, _ := http.NewRequest(http.MethodPost, f.server.URL+"/api/v1/parties", strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed && resp.StatusCode != http.StatusNotFound {
		t.Fatalf("POST status = %d", resp.StatusCode)
	}
}

func TestPartiesAndContractDetail(t *testing.T) {
	f := newFixture(t)
	resp, body := f.get(t, "/api/v1/parties?q=幸福", true)
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}
	var parties model.List[model.Party]
	if err := json.Unmarshal(body, &parties); err != nil {
		t.Fatal(err)
	}
	if parties.Total != 1 || parties.Items[0].Name != "幸福村合作社" {
		t.Fatalf("parties = %+v", parties)
	}
	resp, body = f.get(t, "/api/v1/contracts?status=active", true)
	if resp.StatusCode != 200 {
		t.Fatalf("contracts status = %d body=%s", resp.StatusCode, body)
	}
	var contracts model.List[model.Contract]
	if err := json.Unmarshal(body, &contracts); err != nil {
		t.Fatal(err)
	}
	if contracts.Total != 1 {
		t.Fatalf("contracts = %+v", contracts)
	}
	id := contracts.Items[0].ID
	resp, body = f.get(t, "/api/v1/contracts/"+id, true)
	if resp.StatusCode != 200 {
		t.Fatalf("detail status = %d body=%s", resp.StatusCode, body)
	}
	var detail model.ContractDetail
	if err := json.Unmarshal(body, &detail); err != nil {
		t.Fatal(err)
	}
	if detail.PartyA == nil || detail.PartyB == nil || len(detail.Signatures) != 1 || len(detail.Documents) != 1 {
		t.Fatalf("detail = %+v", detail)
	}
	if detail.Template == nil || detail.Template.HTML != "" {
		t.Fatalf("detail template should omit html: %+v", detail.Template)
	}
}

func TestNotFoundAndDocuments(t *testing.T) {
	f := newFixture(t)
	resp, _ := f.get(t, "/api/v1/parties/nope", true)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	resp, body := f.get(t, "/api/v1/documents", true)
	if resp.StatusCode != 200 {
		t.Fatalf("documents status = %d body=%s", resp.StatusCode, body)
	}
	var docs model.List[model.OutputDocument]
	if err := json.Unmarshal(body, &docs); err != nil {
		t.Fatal(err)
	}
	if docs.Total != 1 || docs.Items[0].PageCount != 2 {
		t.Fatalf("docs = %+v", docs)
	}
}

func TestSchemaAndStats(t *testing.T) {
	f := newFixture(t)
	resp, body := f.get(t, "/api/v1/schema", true)
	if resp.StatusCode != 200 {
		t.Fatalf("schema status = %d", resp.StatusCode)
	}
	if !strings.Contains(string(body), httpapi.SchemaName) || !strings.Contains(string(body), "signature_asset") {
		t.Fatalf("schema missing content: %s", body)
	}
	resp, body = f.get(t, "/api/v1/stats", true)
	if resp.StatusCode != 200 {
		t.Fatalf("stats status = %d", resp.StatusCode)
	}
	var stats model.Stats
	if err := json.Unmarshal(body, &stats); err != nil {
		t.Fatal(err)
	}
	if stats.Parties != 2 || stats.Contracts != 1 || stats.Signatures != 1 {
		t.Fatalf("stats = %+v", stats)
	}
}

func TestTemplates(t *testing.T) {
	f := newFixture(t)
	resp, body := f.get(t, "/api/v1/templates", true)
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var list model.List[model.Template]
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatal(err)
	}
	if list.Total != 1 || list.Items[0].HTML != "" {
		t.Fatalf("templates = %+v", list)
	}
	resp, body = f.get(t, "/api/v1/templates/"+list.Items[0].ID, true)
	if resp.StatusCode != 200 || !strings.Contains(string(body), "<p>") {
		t.Fatalf("template detail status=%d body=%s", resp.StatusCode, body)
	}
}
