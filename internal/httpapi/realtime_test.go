package httpapi_test

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LuSrackhall/tengshou/internal/config"
	"github.com/LuSrackhall/tengshou/internal/model"
	"github.com/LuSrackhall/tengshou/internal/service"
)

func createContractField(t *testing.T, f *fixture, key string) map[string]any {
	t.Helper()
	resp, body := f.appRequest(t, http.MethodPost, "/app/api/v1/fields", map[string]any{
		"entity": "contract", "key": key, "label": key, "type": "text",
	}, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create field %s: %d %s", key, resp.StatusCode, body)
	}
	return decodeAppItem(t, body)
}

func decodeChangeLogList(t *testing.T, body []byte) model.List[model.ChangeLog] {
	t.Helper()
	var list model.List[model.ChangeLog]
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatalf("decode changelog: %v body=%s", err, body)
	}
	return list
}

func TestChangeLogAfterCursorOnlyReturnsNewEntries(t *testing.T) {
	f := newFixture(t)
	first := createContractField(t, f, "cursor_a")
	second := createContractField(t, f, "cursor_b")

	resp, body := f.get(t, "/app/api/v1/changelog?limit=10", false)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("changelog: %d %s", resp.StatusCode, body)
	}
	all := decodeChangeLogList(t, body)
	if len(all.Items) != 2 {
		t.Fatalf("changelog items = %d %s", len(all.Items), body)
	}
	// Cursors are change-log ids, not entity ids.
	logIDOf := func(fieldID any) string {
		for _, item := range all.Items {
			if item.EntityID == fieldID {
				return item.ID
			}
		}
		t.Fatalf("no change log for field %v", fieldID)
		return ""
	}
	firstLogID := logIDOf(first["id"])
	secondLogID := logIDOf(second["id"])

	resp, body = f.get(t, "/app/api/v1/changelog?after="+firstLogID, false)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("after cursor: %d %s", resp.StatusCode, body)
	}
	after := decodeChangeLogList(t, body)
	if len(after.Items) != 1 || after.Items[0].EntityID != second["id"] {
		t.Fatalf("after cursor items = %+v", after.Items)
	}
	if after.Items[0].Entity != model.ChangeEntityField {
		t.Fatalf("after cursor entity = %q", after.Items[0].Entity)
	}

	// The same entries share a truncated second; the rowid cursor still finds
	// the second write. See the store test for the ordering detail.
	resp, body = f.get(t, "/app/api/v1/changelog?after="+secondLogID, false)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("after newest: %d %s", resp.StatusCode, body)
	}
	if after = decodeChangeLogList(t, body); len(after.Items) != 0 {
		t.Fatalf("after newest items = %+v", after.Items)
	}
}

func readSSEData(t *testing.T, reader *bufio.Reader, timeout time.Duration) map[string]any {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read event stream: %v", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		var event map[string]any
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			t.Fatalf("decode event %q: %v", payload, err)
		}
		return event
	}
	t.Fatal("timed out waiting for an SSE data frame")
	return nil
}

func openEventStream(t *testing.T, f *fixture, query string, headers map[string]string) (*http.Response, *bufio.Reader, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	url := f.server.URL + "/app/api/v1/events" + query
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		t.Fatalf("open event stream: %v", err)
	}
	t.Cleanup(func() {
		cancel()
		resp.Body.Close()
	})
	if resp.StatusCode != http.StatusOK {
		cancel()
		t.Fatalf("event stream status = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("event stream content-type = %q", ct)
	}
	return resp, bufio.NewReader(resp.Body), cancel
}

func TestEventStreamReplaysAfterLastEventID(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	older := &model.ChangeLog{
		Entity: model.ChangeEntityContract, EntityID: "ctr_older", Action: model.ChangeActionCreate,
		Actor: service.MCPActor, Summary: "较早的变更", CreatedAt: model.Now().Add(-time.Second),
	}
	newer := &model.ChangeLog{
		Entity: model.ChangeEntityContract, EntityID: "ctr_newer", Action: model.ChangeActionUpdate,
		Actor: service.MCPActor, Summary: "较新的变更", CreatedAt: model.Now(),
	}
	for _, entry := range []*model.ChangeLog{older, newer} {
		if err := f.store.AppendChangeLog(ctx, entry); err != nil {
			t.Fatal(err)
		}
	}

	_, reader, _ := openEventStream(t, f, "?after="+older.ID, nil)
	event := readSSEData(t, reader, 3*time.Second)
	if event["id"] != newer.ID {
		t.Fatalf("replayed event = %v, want %s", event, newer.ID)
	}
	if event["actor"] != service.MCPActor {
		t.Fatalf("replayed actor = %v", event["actor"])
	}
	if event["entity"] != model.ChangeEntityContract {
		t.Fatalf("replayed entity = %v", event["entity"])
	}

	// The standard Last-Event-ID header works the same way (EventSource sends
	// it automatically when it reconnects on its own).
	_, reader, _ = openEventStream(t, f, "", map[string]string{"Last-Event-ID": older.ID})
	event = readSSEData(t, reader, 3*time.Second)
	if event["id"] != newer.ID {
		t.Fatalf("Last-Event-ID replay = %v, want %s", event, newer.ID)
	}
}

func TestEventStreamDeliversNewEntries(t *testing.T) {
	f := newFixtureWithOptions(t, fixtureOptions{
		eventPoll:      20 * time.Millisecond,
		eventHeartbeat: time.Hour,
	})
	_, reader, _ := openEventStream(t, f, "", nil)

	entry := &model.ChangeLog{
		Entity: model.ChangeEntityParty, EntityID: "pty_live", Action: model.ChangeActionCreate,
		Actor: service.MCPActor, Summary: "agent 新建档案", CreatedAt: model.Now(),
	}
	if err := f.store.AppendChangeLog(context.Background(), entry); err != nil {
		t.Fatal(err)
	}
	event := readSSEData(t, reader, 3*time.Second)
	if event["id"] != entry.ID || event["summary"] != "agent 新建档案" {
		t.Fatalf("live event = %v", event)
	}
}

func TestMCPSettingsSwitchAppliesImmediately(t *testing.T) {
	f := newFixture(t)

	// Initial state: writes on, reads on; counts come from the connector spec.
	resp, body := f.get(t, "/app/api/v1/mcp/status", false)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("mcp status: %d %s", resp.StatusCode, body)
	}
	var status map[string]any
	if err := json.Unmarshal(body, &status); err != nil {
		t.Fatal(err)
	}
	if status["write_enabled"] != true || status["read_enabled"] != true {
		t.Fatalf("initial status = %v", status)
	}
	if status["read_tools"] != float64(10) || status["write_tools"] != float64(3) {
		t.Fatalf("tool counts = %v", status)
	}
	if status["agent_actor"] != "tengshou-mcp" {
		t.Fatalf("agent_actor = %v", status["agent_actor"])
	}

	// Close the write switch through the settings endpoint.
	resp, body = f.appRequest(t, http.MethodPost, "/app/api/v1/settings/mcp",
		map[string]any{"write_enabled": false}, nil)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), `"write_enabled":false`) {
		t.Fatalf("disable writes: %d %s", resp.StatusCode, body)
	}
	// It is persisted through config.Save, so a restart keeps the value.
	paths, err := config.ResolvePaths(f.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if paths.ConfigFile != filepath.Join(f.dataDir, "config.json") {
		t.Fatalf("unexpected config path %q", paths.ConfigFile)
	}
	cfg, err := config.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MCP.WriteEnabled {
		t.Fatal("write switch was not persisted to config.json")
	}

	// MCP writes are rejected immediately; MCP reads and desktop writes still
	// work. No restart happened between the toggle and these requests.
	resp, body = f.appRequest(t, http.MethodPost, "/app/api/v1/fields", map[string]any{
		"entity": "contract", "key": "blocked_by_switch", "label": "被拒绝", "type": "text",
	}, map[string]string{"X-Tengshou-MCP": "1"})
	if resp.StatusCode != http.StatusForbidden || !strings.Contains(string(body), "mcp_write_disabled") {
		t.Fatalf("mcp write with switch off: %d %s", resp.StatusCode, body)
	}
	resp, body = f.appRequest(t, http.MethodGet, "/app/api/v1/fields", nil,
		map[string]string{"X-Tengshou-MCP": "1"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("mcp read with switch off: %d %s", resp.StatusCode, body)
	}
	resp, body = f.appRequest(t, http.MethodPost, "/app/api/v1/fields", map[string]any{
		"entity": "contract", "key": "human_ok", "label": "人工写入", "type": "text",
	}, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("desktop write with switch off: %d %s", resp.StatusCode, body)
	}

	// The switch change itself is audited as a settings update by the operator.
	resp, body = f.get(t, "/app/api/v1/changelog?entity=settings&limit=10", false)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("settings changelog: %d %s", resp.StatusCode, body)
	}
	settingsLogs := decodeChangeLogList(t, body)
	if settingsLogs.Total != 1 || len(settingsLogs.Items) != 1 {
		t.Fatalf("settings change logs = %+v", settingsLogs)
	}
	logEntry := settingsLogs.Items[0]
	if logEntry.Actor != "tester" || logEntry.Action != model.ChangeActionUpdate {
		t.Fatalf("settings log = %+v", logEntry)
	}
	if len(logEntry.Changes) != 1 || logEntry.Changes[0].Field != "mcp.write_enabled" ||
		logEntry.Changes[0].Old != "开启" || logEntry.Changes[0].New != "关闭" {
		t.Fatalf("settings diff = %+v", logEntry.Changes)
	}

	// Source filters separate human edits from agent edits.
	resp, body = f.get(t, "/app/api/v1/changelog?source=agent", false)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("agent changelog: %d %s", resp.StatusCode, body)
	}
	if agentLogs := decodeChangeLogList(t, body); agentLogs.Total != 0 {
		t.Fatalf("agent changelog = %+v", agentLogs.Items)
	}
	resp, body = f.get(t, "/app/api/v1/changelog?source=human", false)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("human changelog: %d %s", resp.StatusCode, body)
	}
	if humanLogs := decodeChangeLogList(t, body); humanLogs.Total < 2 {
		t.Fatalf("human changelog total = %d", humanLogs.Total)
	}

	// Re-enable: MCP writes work again without a restart.
	resp, body = f.appRequest(t, http.MethodPost, "/app/api/v1/settings/mcp",
		map[string]any{"write_enabled": true}, nil)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), `"write_enabled":true`) {
		t.Fatalf("enable writes: %d %s", resp.StatusCode, body)
	}
	resp, body = f.appRequest(t, http.MethodPost, "/app/api/v1/fields", map[string]any{
		"entity": "contract", "key": "agent_ok", "label": "Agent 写入", "type": "text",
	}, map[string]string{"X-Tengshou-MCP": "1"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("mcp write after re-enable: %d %s", resp.StatusCode, body)
	}
	resp, body = f.get(t, "/app/api/v1/changelog?source=agent&entity=field", false)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("agent field changelog: %d %s", resp.StatusCode, body)
	}
	agentLogs := decodeChangeLogList(t, body)
	if agentLogs.Total != 1 || agentLogs.Items[0].Actor != "tengshou-mcp" {
		t.Fatalf("agent audit = %+v", agentLogs.Items)
	}

	// An agent must not be able to flip its own switch.
	resp, body = f.appRequest(t, http.MethodPost, "/app/api/v1/settings/mcp",
		map[string]any{"write_enabled": false}, map[string]string{"X-Tengshou-MCP": "1"})
	if resp.StatusCode != http.StatusForbidden || !strings.Contains(string(body), "settings_ui_only") {
		t.Fatalf("mcp settings write: %d %s", resp.StatusCode, body)
	}
}

func TestShutdownEndsEventStream(t *testing.T) {
	f := newFixtureWithOptions(t, fixtureOptions{eventHeartbeat: time.Hour})
	req := httptest.NewRequest(http.MethodGet, "/app/api/v1/events", nil)
	req.RemoteAddr = "127.0.0.1:5000"
	done := make(chan struct{})
	go func() {
		defer close(done)
		f.handler.ServeHTTP(httptest.NewRecorder(), req)
	}()
	// Give the handler time to enter its stream loop, then shut the server
	// down. The SSE context must be cancelled so shutdown does not wait for
	// the client or the heartbeat timeout.
	time.Sleep(100 * time.Millisecond)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := f.apiServer.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("event stream handler did not exit on shutdown")
	}
}
