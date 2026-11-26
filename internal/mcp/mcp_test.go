package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LuSrackhall/tengshou/internal/config"
	"github.com/LuSrackhall/tengshou/internal/httpapi"
	"github.com/LuSrackhall/tengshou/internal/mcpspec"
	"github.com/LuSrackhall/tengshou/internal/model"
	"github.com/LuSrackhall/tengshou/internal/service"
	"github.com/LuSrackhall/tengshou/internal/store"
)

// testEnv runs the real HTTP service (with its middleware, validation and
// change log) and points the MCP client at it. This keeps the connector tests
// honest: tools exercise the same surface a desktop UI or external agent uses.
type testEnv struct {
	api        *API
	server     *httptest.Server
	store      *store.Store
	reader     *store.Reader
	svc        *service.Service
	partyAID   string
	partyBID   string
	contractID string
	templateID string
}

func newTestEnv(t *testing.T, writeEnabled bool) *testEnv {
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
	partyA := &model.Party{Kind: model.PartyOrganization, Name: "幸福村合作社"}
	partyB := &model.Party{Kind: model.PartyPerson, Name: "张三", Phone: "13800000002"}
	if err := st.PutParty(ctx, partyA); err != nil {
		t.Fatal(err)
	}
	if err := st.PutParty(ctx, partyB); err != nil {
		t.Fatal(err)
	}
	tpl := &model.Template{
		Name: "租赁模板", Version: 1, HTML: "<p>{{.Data.rent}}</p>",
		Variables: []string{"rent"}, IsActive: true,
	}
	if err := st.PutTemplate(ctx, tpl); err != nil {
		t.Fatal(err)
	}
	contract := &model.Contract{
		Year: 2026, Title: "幸福村房屋租赁合同", ContractNo: "HT-2026-0001",
		PartyAID: partyA.ID, PartyBID: partyB.ID, TemplateID: tpl.ID,
		Status: model.ContractActive, Data: map[string]any{"rent": 1200.0},
	}
	if err := st.PutContract(ctx, contract); err != nil {
		t.Fatal(err)
	}
	sig := &model.Signature{
		PartyID: partyB.ID, ContractID: contract.ID, SignerName: "张三",
		AssetPath: "assets/sig.png", SHA256: "abc", ContentSHA256: "content-v1",
		Source: model.SignatureSourceLANQR, WidthPx: 10, HeightPx: 5,
	}
	if err := st.PutSignature(ctx, sig); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	cfg.HTTP.Token = "test-token"
	cfg.HTTP.AllowLAN = false
	cfg.HTTP.Bind = "127.0.0.1"
	cfg.MCP.WriteEnabled = writeEnabled
	svc := service.New(st, reader, "tester")
	svc.SetDataDir(t.TempDir())
	server := httpapi.New(httpapi.Options{
		Reader:  reader,
		Service: svc,
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		Config:  cfg,
		Version: "test",
	})
	ts := httptest.NewServer(server.Handler())
	t.Cleanup(ts.Close)

	api, err := NewAPI(APIOptions{
		BaseURLs:     []string{ts.URL},
		Token:        "test-token",
		Client:       &http.Client{Timeout: 5 * time.Second},
		ProbeTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	return &testEnv{
		api:        api,
		server:     ts,
		store:      st,
		reader:     reader,
		svc:        svc,
		partyAID:   partyA.ID,
		partyBID:   partyB.ID,
		contractID: contract.ID,
		templateID: tpl.ID,
	}
}

func serverFor(t *testing.T, env *testEnv) *Server {
	t.Helper()
	return New(Options{
		API:     env.api,
		Version: "test",
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}

func runRPC(t *testing.T, srv *Server, lines ...string) []map[string]any {
	t.Helper()
	input := strings.Join(lines, "\n") + "\n"
	var out bytes.Buffer
	if err := srv.Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatalf("serve: %v", err)
	}
	text := strings.TrimSpace(out.String())
	if text == "" {
		return nil
	}
	responses := []map[string]any{}
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var response map[string]any
		if err := json.Unmarshal([]byte(line), &response); err != nil {
			t.Fatalf("invalid response %q: %v", line, err)
		}
		responses = append(responses, response)
	}
	return responses
}

func callTool(t *testing.T, srv *Server, name string, args any) (string, bool) {
	t.Helper()
	rawArgs, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	request := fmt.Sprintf(
		`{"jsonrpc":"2.0","id":"call-1","method":"tools/call","params":{"name":%q,"arguments":%s}}`,
		name, rawArgs)
	responses := runRPC(t, srv, request)
	if len(responses) != 1 {
		t.Fatalf("got %d responses for %s", len(responses), name)
	}
	if rpcErr, ok := responses[0]["error"]; ok {
		t.Fatalf("%s protocol error: %v", name, rpcErr)
	}
	result, ok := responses[0]["result"].(map[string]any)
	if !ok {
		t.Fatalf("%s missing result: %v", name, responses[0])
	}
	content, ok := result["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("%s missing content: %v", name, result)
	}
	first, _ := content[0].(map[string]any)
	text, _ := first["text"].(string)
	isError, _ := result["isError"].(bool)
	return text, isError
}

func callToolJSON(t *testing.T, srv *Server, name string, args any) map[string]any {
	t.Helper()
	text, isError := callTool(t, srv, name, args)
	if isError {
		t.Fatalf("%s returned tool error: %s", name, text)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("%s returned non-JSON text: %v\n%s", name, err, text)
	}
	return payload
}

func assertMCPActor(t *testing.T, reader *store.Reader, entity, entityID string) {
	t.Helper()
	logs, err := reader.ListChangeLogs(context.Background(), store.ChangeFilter{
		Entity: entity, EntityID: entityID, Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if logs.Total == 0 {
		t.Fatalf("no change log for %s/%s", entity, entityID)
	}
	if logs.Items[0].Actor != service.MCPActor {
		t.Fatalf("actor = %q, want %q", logs.Items[0].Actor, service.MCPActor)
	}
}

func TestProtocolInitializeListAndPing(t *testing.T) {
	env := newTestEnv(t, true)
	srv := serverFor(t, env)
	responses := runRPC(t, srv,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test-client","version":"1.0"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":4,"method":"unknown/method"}`,
	)
	if len(responses) != 4 {
		t.Fatalf("got %d responses, want 4 (notifications must stay silent)", len(responses))
	}
	initResult, ok := responses[0]["result"].(map[string]any)
	if !ok {
		t.Fatalf("initialize result: %v", responses[0])
	}
	if initResult["protocolVersion"] != "2025-06-18" {
		t.Fatalf("protocolVersion = %v", initResult["protocolVersion"])
	}
	serverInfo, _ := initResult["serverInfo"].(map[string]any)
	if serverInfo["name"] != "tengshou" || serverInfo["version"] != "test" {
		t.Fatalf("serverInfo = %v", serverInfo)
	}
	capabilities, _ := initResult["capabilities"].(map[string]any)
	if _, ok := capabilities["tools"]; !ok {
		t.Fatalf("missing tools capability: %v", capabilities)
	}
	if instructions, _ := initResult["instructions"].(string); !strings.Contains(instructions, "tengshou-mcp") {
		t.Fatalf("instructions = %q", instructions)
	}

	toolsResult, _ := responses[1]["result"].(map[string]any)
	tools, _ := toolsResult["tools"].([]any)
	expected := []string{
		toolStatus, toolGetDataSchema, toolListFields, toolListParties, toolGetParty,
		toolListContracts, toolGetContract, toolListSignatureAssets, toolListTemplates,
		toolGetTemplate, toolListDocuments, toolPreflightGeneration,
		toolCreateField, toolCreateParty, toolCreateContractDraft, toolGenerateContract,
	}
	if len(tools) != len(expected) {
		t.Fatalf("tools = %d, want %d", len(tools), len(expected))
	}
	names := map[string]bool{}
	for _, raw := range tools {
		tool, _ := raw.(map[string]any)
		name, _ := tool["name"].(string)
		names[name] = true
	}
	for _, want := range expected {
		if !names[want] {
			t.Fatalf("missing tool %q in %v", want, names)
		}
	}

	pingResult, ok := responses[2]["result"].(map[string]any)
	if !ok || len(pingResult) != 0 {
		t.Fatalf("ping result = %v", responses[2])
	}
	rpcErr, _ := responses[3]["error"].(map[string]any)
	if rpcErr["code"] != float64(codeMethodNotFound) {
		t.Fatalf("unknown method error = %v", responses[3])
	}

	// Unknown protocol revisions fall back to the default supported version.
	fallback := runRPC(t, srv,
		`{"jsonrpc":"2.0","id":9,"method":"initialize","params":{"protocolVersion":"1999-01-01"}}`)
	result, _ := fallback[0]["result"].(map[string]any)
	if result["protocolVersion"] != defaultProtocolVersion {
		t.Fatalf("fallback protocolVersion = %v", result["protocolVersion"])
	}
}

func TestToolCatalogHasNoDestructiveTools(t *testing.T) {
	env := newTestEnv(t, true)
	srv := serverFor(t, env)
	responses := runRPC(t, srv, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	result, _ := responses[0]["result"].(map[string]any)
	tools, _ := result["tools"].([]any)
	if len(tools) == 0 {
		t.Fatal("no tools")
	}
	banned := []string{"delete", "remove", "update", "replace", "archive", "import", "bulk"}
	for _, raw := range tools {
		tool, _ := raw.(map[string]any)
		name, _ := tool["name"].(string)
		for _, word := range banned {
			if strings.Contains(name, word) {
				t.Fatalf("destructive-sounding tool exposed: %s", name)
			}
		}
		schema, _ := tool["inputSchema"].(map[string]any)
		if schema["type"] != "object" || schema["additionalProperties"] != false {
			t.Fatalf("%s schema is not a closed object: %v", name, schema)
		}
		annotations, _ := tool["annotations"].(map[string]any)
		if isWriteTool(name) {
			if annotations["readOnlyHint"] != false || annotations["destructiveHint"] != false {
				t.Fatalf("%s write annotations = %v", name, annotations)
			}
		} else if annotations["readOnlyHint"] != true {
			t.Fatalf("%s read annotations = %v", name, annotations)
		}
	}
}

func TestReadTools(t *testing.T) {
	env := newTestEnv(t, true)
	srv := serverFor(t, env)

	status := callToolJSON(t, srv, toolStatus, map[string]any{})
	if status["mcp_write_enabled"] != true {
		t.Fatalf("mcp_write_enabled = %v", status["mcp_write_enabled"])
	}
	if base, _ := status["api_base_url"].(string); !strings.HasPrefix(base, "http://127.0.0.1:") {
		t.Fatalf("api_base_url = %q", base)
	}

	schema := callToolJSON(t, srv, toolGetDataSchema, map[string]any{})
	if schema["schema"] != "tengshou.data.v1" || schema["read_only"] != true {
		t.Fatalf("schema = %v", schema)
	}

	fields := callToolJSON(t, srv, toolListFields, map[string]any{"entity": "contract"})
	fieldItems, _ := fields["items"].([]any)
	if !hasFieldKey(fieldItems, "contract_no") {
		t.Fatalf("contract fields missing built-ins: %v", fields)
	}

	parties := callToolJSON(t, srv, toolListParties, map[string]any{"q": "张"})
	if parties["total"] != float64(1) {
		t.Fatalf("parties = %v", parties)
	}
	partyItem := parties["items"].([]any)[0].(map[string]any)
	if partyItem["name"] != "张三" {
		t.Fatalf("party = %v", partyItem)
	}

	party := callToolJSON(t, srv, toolGetParty, map[string]any{"id": env.partyBID})
	if party["id"] != env.partyBID || party["phone"] != "13800000002" {
		t.Fatalf("party = %v", party)
	}

	contracts := callToolJSON(t, srv, toolListContracts, map[string]any{"year": 2026})
	if contracts["total"] != float64(1) {
		t.Fatalf("contracts = %v", contracts)
	}

	detail := callToolJSON(t, srv, toolGetContract, map[string]any{"id": env.contractID})
	contract := detail["contract"].(map[string]any)
	if contract["title"] != "幸福村房屋租赁合同" {
		t.Fatalf("contract = %v", contract)
	}
	signatures := detail["signatures"].([]any)
	if len(signatures) != 1 {
		t.Fatalf("signatures = %v", signatures)
	}
	sig := signatures[0].(map[string]any)
	if sig["content_sha256"] != "content-v1" || sig["signer_name"] != "张三" {
		t.Fatalf("signature = %v", sig)
	}
	template := detail["template"].(map[string]any)
	if template["version"] != float64(1) {
		t.Fatalf("template = %v", template)
	}

	signatureList := callToolJSON(t, srv, toolListSignatureAssets, map[string]any{"contract_id": env.contractID})
	if signatureList["total"] != float64(1) {
		t.Fatalf("signature list = %v", signatureList)
	}

	templates := callToolJSON(t, srv, toolListTemplates, map[string]any{})
	if templates["total"] != float64(1) {
		t.Fatalf("templates = %v", templates)
	}
	tplItem := templates["items"].([]any)[0].(map[string]any)
	if tplItem["version"] != float64(1) || tplItem["html"] != nil {
		t.Fatalf("template summary = %v", tplItem)
	}

	templateDetail := callToolJSON(t, srv, toolGetTemplate, map[string]any{"id": env.templateID})
	if !strings.Contains(fmt.Sprint(templateDetail["html"]), "<p>") {
		t.Fatalf("template detail = %v", templateDetail)
	}
}

func hasFieldKey(items []any, key string) bool {
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		if item["key"] == key {
			return true
		}
	}
	return false
}

func TestWriteToolsAndActorAudit(t *testing.T) {
	env := newTestEnv(t, true)
	srv := serverFor(t, env)
	ctx := context.Background()

	created := callToolJSON(t, srv, toolCreateField, map[string]any{
		"entity": "contract", "key": "area_sqm", "label": "平方数", "type": "number",
		"in_ledger": true, "in_template": true,
	})
	field := created["item"].(map[string]any)
	if field["key"] != "area_sqm" || field["built_in"] != false {
		t.Fatalf("field = %v", field)
	}
	fieldID, _ := field["id"].(string)
	if _, err := env.reader.GetFieldByKey(ctx, "contract", "area_sqm"); err != nil {
		t.Fatalf("field not persisted: %v", err)
	}
	assertMCPActor(t, env.reader, "field", fieldID)

	party := callToolJSON(t, srv, toolCreateParty, map[string]any{
		"name": "李四", "kind": "person", "phone": "13900000000", "id_number": "110101199001011234",
	})
	partyItem := party["item"].(map[string]any)
	partyID, _ := partyItem["id"].(string)
	if partyID == "" || partyItem["name"] != "李四" {
		t.Fatalf("party = %v", partyItem)
	}
	if _, err := env.reader.GetParty(ctx, partyID); err != nil {
		t.Fatalf("party not persisted: %v", err)
	}
	assertMCPActor(t, env.reader, "party", partyID)

	draft := callToolJSON(t, srv, toolCreateContractDraft, map[string]any{
		"title": "MCP 起草合同", "year": 2027, "party_b": "王五", "amount": 9000,
	})
	contract := draft["item"].(map[string]any)
	if contract["status"] != model.ContractDraft {
		t.Fatalf("contract status = %v", contract["status"])
	}
	number, _ := contract["contract_no"].(string)
	if !strings.HasPrefix(number, "HT-2027-") {
		t.Fatalf("contract_no = %q", number)
	}
	contractID, _ := contract["id"].(string)
	assertMCPActor(t, env.reader, "contract", contractID)
	warnings, _ := draft["warnings"].([]any)
	if len(warnings) == 0 {
		t.Fatalf("expected auto-archive warning, got %v", draft)
	}
	matches, err := env.reader.FindPartiesByName(ctx, "王五")
	if err != nil || len(matches) != 1 {
		t.Fatalf("auto-created party = %v err=%v", matches, err)
	}
	assertMCPActor(t, env.reader, "party", matches[0].ID)
}

func TestToolValidationAndArgumentErrors(t *testing.T) {
	env := newTestEnv(t, true)
	srv := serverFor(t, env)

	text, isError := callTool(t, srv, toolCreateField, map[string]any{
		"entity": "contract", "key": "Bad Key", "label": "坏键", "type": "text",
	})
	if !isError || !strings.Contains(text, "invalid_key") {
		t.Fatalf("invalid key: isError=%v text=%s", isError, text)
	}

	callToolJSON(t, srv, toolCreateField, map[string]any{
		"entity": "contract", "key": "ok_key", "label": "正常", "type": "text",
	})
	text, isError = callTool(t, srv, toolCreateField, map[string]any{
		"entity": "contract", "key": "ok_key", "label": "重复", "type": "text",
	})
	if !isError || !strings.Contains(text, "conflict") {
		t.Fatalf("duplicate key: isError=%v text=%s", isError, text)
	}

	// Drafts only: a caller cannot smuggle an active status through.
	text, isError = callTool(t, srv, toolCreateContractDraft, map[string]any{
		"title": "契约", "status": "active",
	})
	if !isError || !strings.Contains(text, "参数错误") {
		t.Fatalf("status argument: isError=%v text=%s", isError, text)
	}
}

func TestWriteSwitchOff(t *testing.T) {
	env := newTestEnv(t, false)
	srv := serverFor(t, env)

	// Reads keep working when writes are disabled.
	contracts := callToolJSON(t, srv, toolListContracts, map[string]any{"year": 2026})
	if contracts["total"] != float64(1) {
		t.Fatalf("contracts = %v", contracts)
	}
	status := callToolJSON(t, srv, toolStatus, map[string]any{})
	if status["mcp_write_enabled"] != false {
		t.Fatalf("mcp_write_enabled = %v", status["mcp_write_enabled"])
	}

	text, isError := callTool(t, srv, toolCreateParty, map[string]any{"name": "被拒绝", "id_number": "110101199001015678"})
	if !isError || !strings.Contains(text, "mcp_write_disabled") {
		t.Fatalf("write off: isError=%v text=%s", isError, text)
	}

	// The switch gates the MCP-only header, not the desktop app path.
	post := func(mcp bool) (int, []byte) {
		t.Helper()
		req, err := http.NewRequest(http.MethodPost, env.server.URL+"/app/api/v1/parties",
			strings.NewReader(`{"name":"直接写入","id_number":"110101199001019999"}`))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Tengshou-App", "1")
		if mcp {
			req.Header.Set(MCPMarkerHeader, "1")
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return resp.StatusCode, body
	}
	if code, body := post(true); code != http.StatusForbidden {
		t.Fatalf("mcp write with switch off: %d %s", code, body)
	}
	if code, body := post(false); code != http.StatusOK {
		t.Fatalf("desktop write with switch off: %d %s", code, body)
	}
}

func TestConnectionErrorsAreClear(t *testing.T) {
	closed := httptest.NewServer(http.NotFoundHandler())
	closedURL := closed.URL
	closed.Close()

	api, err := NewAPI(APIOptions{
		BaseURLs:     []string{closedURL},
		Client:       &http.Client{Timeout: 300 * time.Millisecond},
		ProbeTimeout: 100 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := New(Options{API: api, Version: "test"})
	text, isError := callTool(t, srv, toolListContracts, map[string]any{})
	if !isError || !strings.Contains(text, "无法连接腾手本地服务") {
		t.Fatalf("connection error: isError=%v text=%s", isError, text)
	}
}

func TestBaseURLFallsBackAcrossCandidates(t *testing.T) {
	env := newTestEnv(t, true)
	closed := httptest.NewServer(http.NotFoundHandler())
	closedURL := closed.URL
	closed.Close()

	api, err := NewAPI(APIOptions{
		BaseURLs:     []string{closedURL, env.server.URL},
		Token:        "test-token",
		Client:       &http.Client{Timeout: 2 * time.Second},
		ProbeTimeout: 200 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := New(Options{API: api, Version: "test"})
	status := callToolJSON(t, srv, toolStatus, map[string]any{})
	if base, _ := status["api_base_url"].(string); base != env.server.URL {
		t.Fatalf("api_base_url = %q, want %q", base, env.server.URL)
	}
}

func TestNewAPIRejectsNonLoopback(t *testing.T) {
	for _, raw := range []string{
		"http://192.168.1.10:8765",
		"https://127.0.0.1:8765",
		"http://example.com:8765",
		"http://127.0.0.1",
		"http://127.0.0.1:8765/api",
	} {
		if _, err := NewAPI(APIOptions{BaseURLs: []string{raw}}); err == nil {
			t.Fatalf("expected %q to be rejected", raw)
		}
	}
	if _, err := NewAPI(APIOptions{BaseURLs: []string{"http://127.0.0.1:8765"}}); err != nil {
		t.Fatalf("loopback URL rejected: %v", err)
	}
	if _, err := NewAPI(APIOptions{BaseURLs: []string{"http://localhost:8765"}}); err != nil {
		t.Fatalf("localhost URL rejected: %v", err)
	}
}

func TestDecodeArgsRejectsUnknownFields(t *testing.T) {
	var dst struct {
		Name string `json:"name"`
	}
	if err := decodeArgs(json.RawMessage(`{"name":"ok"}`), &dst); err != nil {
		t.Fatalf("valid args rejected: %v", err)
	}
	if err := decodeArgs(json.RawMessage(`{"name":"ok","extra":1}`), &dst); err == nil {
		t.Fatal("unknown field should be rejected")
	}
	if err := decodeArgs(nil, &dst); err != nil {
		t.Fatalf("empty args rejected: %v", err)
	}
}

func TestToolCountsMatchSpec(t *testing.T) {
	reads, writes := 0, 0
	for _, tool := range toolDefinitions() {
		if isWriteTool(tool.Name) {
			writes++
		} else {
			reads++
		}
	}
	if reads != mcpspec.ReadToolCount || writes != mcpspec.WriteToolCount {
		t.Fatalf("tool counts = %d read / %d write, spec says %d / %d",
			reads, writes, mcpspec.ReadToolCount, mcpspec.WriteToolCount)
	}
}

func TestWriteGateShortCircuitsWriteTools(t *testing.T) {
	env := newTestEnv(t, true) // the HTTP service allows writes
	allowed := false
	srv := New(Options{
		API:          env.api,
		Version:      "test",
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		WriteEnabled: func() bool { return allowed },
	})

	text, isError := callTool(t, srv, toolCreateParty, map[string]any{"name": "被开关拒绝", "id_number": "110101199001017777"})
	if !isError || !strings.Contains(text, "mcp_write_disabled") {
		t.Fatalf("write gate off: isError=%v text=%s", isError, text)
	}
	// Reads are never gated.
	callToolJSON(t, srv, toolListContracts, map[string]any{})

	allowed = true
	created := callToolJSON(t, srv, toolCreateParty, map[string]any{"name": "允许写入", "id_number": "110101199001016666"})
	item, _ := created["item"].(map[string]any)
	if item["name"] != "允许写入" {
		t.Fatalf("create after gate opened = %v", created)
	}
}
