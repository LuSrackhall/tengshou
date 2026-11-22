package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/LuSrackhall/tengshou/internal/mcpspec"
	"github.com/LuSrackhall/tengshou/internal/model"
	"github.com/LuSrackhall/tengshou/internal/service"
	"github.com/LuSrackhall/tengshou/internal/store"
)

const maxAppBody = 64 << 20 // 64 MiB: base64 workbook or Word template upload

func (s *Server) handleAppAPIRoot(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"name":     "腾手应用接口",
		"version":  "app/v1",
		"loopback": "只允许本机访问",
		"header":   "写操作需携带 X-Tengshou-App: 1",
		"resources": []string{
			"/app/api/v1/bootstrap",
			"/app/api/v1/events",
			"/app/api/v1/mcp/status",
			"/app/api/v1/settings/mcp",
			"/app/api/v1/fields",
			"/app/api/v1/parties",
			"/app/api/v1/contracts",
			"/app/api/v1/contracts/years",
			"/app/api/v1/changelog",
			"/app/api/v1/seals",
			"/app/api/v1/seals/{id}",
			"/app/api/v1/seals/{id}/image",
			"/app/api/v1/seals/{id}/attach",
			"/app/api/v1/templates",
			"/app/api/v1/templates/import",
			"/app/api/v1/templates/{id}",
			"/app/api/v1/templates/{id}/mapping",
			"/app/api/v1/templates/{id}/usage",
			"/app/api/v1/templates/{id}/pdf",
			"/app/api/v1/templates/{id}/source",
			"/app/api/v1/templates/{id}/preview",
			"/app/api/v1/templates/{id}/preflight",
			"/app/api/v1/templates/{id}/generate",
			"/app/api/v1/templates/{id}/batch/preview",
			"/app/api/v1/templates/{id}/batch/generate",
			"/app/api/v1/converters",
			"/app/api/v1/documents",
			"/app/api/v1/documents/{id}/file",
			"/app/api/v1/excel/template",
			"/app/api/v1/excel/ledger",
			"/app/api/v1/excel/preview",
			"/app/api/v1/excel/commit",
			"/app/api/v1/signature-sessions",
			"/app/api/v1/signature-sessions/{id}",
			"/app/api/v1/signature-sessions/{id}/qr.png",
			"/app/api/v1/signatures/{id}/export",
			"/app/api/v1/backup/export",
			"/app/api/v1/backup/status",
			"/app/api/v1/backup/restore",
			"/app/api/v1/integrity",
		},
	})
}

func (s *Server) handleAppBootstrap(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.appService(w)
	if !ok {
		return
	}
	schemaVersion, _ := s.opts.Reader.SchemaVersion(r.Context())
	years, err := svc.ContractYears(r.Context())
	if err != nil {
		s.internalError(w, "contract years", err)
		return
	}
	stats, err := s.opts.Reader.Stats(r.Context())
	if err != nil {
		s.internalError(w, "stats", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version":           s.version(),
		"schema_version":    schemaVersion,
		"operator":          svc.Actor(),
		"current_year":      time.Now().Year(),
		"years":             years,
		"stats":             stats,
		"mcp_write_enabled": s.mcpWriteEnabled(),
		"now":               time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) handleAppListFields(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.appService(w)
	if !ok {
		return
	}
	includeArchived := strings.TrimSpace(r.URL.Query().Get("archived")) == "1"
	entity := strings.TrimSpace(r.URL.Query().Get("entity"))
	if entity != "" {
		fields, err := svc.ListFields(r.Context(), entity, includeArchived)
		if err != nil {
			s.writeServiceError(w, "list fields", err)
			return
		}
		writeJSON(w, http.StatusOK, model.List[model.FieldDefinition]{
			Items: fields, Total: len(fields), Limit: len(fields),
		})
		return
	}
	contractFields, err := svc.ListFields(r.Context(), model.FieldEntityContract, includeArchived)
	if err != nil {
		s.writeServiceError(w, "list contract fields", err)
		return
	}
	partyFields, err := svc.ListFields(r.Context(), model.FieldEntityParty, includeArchived)
	if err != nil {
		s.writeServiceError(w, "list party fields", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"contract": contractFields,
		"party":    partyFields,
	})
}

func (s *Server) handleAppCreateField(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.appService(w)
	if !ok {
		return
	}
	var in service.FieldInput
	if !s.readAppJSON(w, r, &in) {
		return
	}
	field, err := svc.CreateField(r.Context(), in)
	if err != nil {
		s.writeServiceError(w, "create field", err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"item": field})
}

func (s *Server) handleAppUpdateField(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.appService(w)
	if !ok {
		return
	}
	var in service.FieldInput
	if !s.readAppJSON(w, r, &in) {
		return
	}
	field, err := svc.UpdateField(r.Context(), r.PathValue("id"), in)
	if err != nil {
		s.writeServiceError(w, "update field", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"item": field})
}

func (s *Server) handleAppArchiveField(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.appService(w)
	if !ok {
		return
	}
	field, err := svc.ArchiveField(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeServiceError(w, "archive field", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"item": field})
}

func (s *Server) handleAppListParties(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.appService(w)
	if !ok {
		return
	}
	items, err := svc.ListParties(r.Context(), filter(r))
	if err != nil {
		s.internalError(w, "list parties", err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleAppGetParty(w http.ResponseWriter, r *http.Request) {
	party, err := s.opts.Reader.GetParty(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "party not found")
		return
	}
	if err != nil {
		s.internalError(w, "get party", err)
		return
	}
	writeJSON(w, http.StatusOK, party)
}

func (s *Server) handleAppSaveParty(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.appService(w)
	if !ok {
		return
	}
	var in service.PartyInput
	if !s.readAppJSON(w, r, &in) {
		return
	}
	if id := strings.TrimSpace(r.PathValue("id")); id != "" {
		in.ID = id
	}
	party, warnings, err := svc.SaveParty(r.Context(), in)
	if err != nil {
		s.writeServiceError(w, "save party", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"item": party, "warnings": warnings})
}

func (s *Server) handleAppDeleteParty(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.appService(w)
	if !ok {
		return
	}
	if err := svc.DeleteParty(r.Context(), r.PathValue("id")); err != nil {
		s.writeServiceError(w, "delete party", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
}

func (s *Server) handleAppListContracts(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.appService(w)
	if !ok {
		return
	}
	items, err := svc.ListContracts(r.Context(), filter(r))
	if err != nil {
		s.internalError(w, "list contracts", err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleAppContractYears(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.appService(w)
	if !ok {
		return
	}
	years, err := svc.ContractYears(r.Context())
	if err != nil {
		s.internalError(w, "contract years", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"years": years})
}

func (s *Server) handleAppGetContract(w http.ResponseWriter, r *http.Request) {
	detail, err := s.opts.Reader.GetContractDetail(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "contract not found")
		return
	}
	if err != nil {
		s.internalError(w, "get contract", err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) handleAppSaveContract(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.appService(w)
	if !ok {
		return
	}
	var in service.ContractInput
	if !s.readAppJSON(w, r, &in) {
		return
	}
	if id := strings.TrimSpace(r.PathValue("id")); id != "" {
		in.ID = id
	}
	contract, warnings, err := svc.SaveContract(r.Context(), in)
	if err != nil {
		s.writeServiceError(w, "save contract", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"item": contract, "warnings": warnings})
}

func (s *Server) handleAppDeleteContract(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.appService(w)
	if !ok {
		return
	}
	if err := svc.DeleteContract(r.Context(), r.PathValue("id")); err != nil {
		s.writeServiceError(w, "delete contract", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
}

// handleAppListDocuments is the B4 generation-history query surface. Filters:
// contract_id, template_id, batch_id, status (success|failed), limit, offset.
func (s *Server) handleAppListDocuments(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := filter(r)
	f.ContractID = strings.TrimSpace(q.Get("contract_id"))
	f.TemplateID = strings.TrimSpace(q.Get("template_id"))
	f.BatchID = strings.TrimSpace(q.Get("batch_id"))
	if status := strings.TrimSpace(q.Get("status")); status == model.DocumentStatusSuccess ||
		status == model.DocumentStatusFailed {
		f.Status = status
	} else {
		f.Status = ""
	}
	items, err := s.opts.Reader.ListDocuments(r.Context(), f)
	if err != nil {
		s.internalError(w, "list documents", err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleAppChangeLog(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	// Cursor mode feeds the realtime polling fallback: entity/source filters
	// do not apply, the client wants every appended entry since its cursor.
	if after := strings.TrimSpace(q.Get("after")); after != "" {
		limit := intParam(r, "limit", 200)
		if limit <= 0 {
			limit = 200
		}
		if limit > 500 {
			limit = 500
		}
		items, err := s.opts.Reader.ListChangeLogsAfter(r.Context(), after, limit)
		if err != nil {
			s.internalError(w, "list change log after cursor", err)
			return
		}
		writeJSON(w, http.StatusOK, model.List[model.ChangeLog]{
			Items: items, Total: len(items), Limit: limit, Offset: 0,
		})
		return
	}
	f := store.ChangeFilter{
		Entity:   strings.TrimSpace(q.Get("entity")),
		EntityID: strings.TrimSpace(q.Get("entity_id")),
		Limit:    intParam(r, "limit", 100),
		Offset:   intParam(r, "offset", 0),
	}
	switch strings.TrimSpace(q.Get("source")) {
	case "":
	case "agent":
		f.Actor = service.MCPActor
	case "human":
		f.ActorNot = service.MCPActor
	default:
		writeError(w, http.StatusBadRequest, "invalid_source", "source 只能是 agent 或 human")
		return
	}
	items, err := s.opts.Reader.ListChangeLogs(r.Context(), f)
	if err != nil {
		s.internalError(w, "list change log", err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// appEvent is the compact SSE payload. Field-level diffs stay in the full
// changelog endpoint; the stream only tells the UI what changed.
type appEvent struct {
	ID        string    `json:"id"`
	Entity    string    `json:"entity"`
	EntityID  string    `json:"entity_id"`
	Action    string    `json:"action"`
	Actor     string    `json:"actor"`
	Summary   string    `json:"summary"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Server) eventPollInterval() time.Duration {
	if s.opts.EventPollInterval > 0 {
		return s.opts.EventPollInterval
	}
	return time.Second
}

func (s *Server) eventHeartbeatInterval() time.Duration {
	if s.opts.EventHeartbeatInterval > 0 {
		return s.opts.EventHeartbeatInterval
	}
	return 15 * time.Second
}

// handleAppEvents streams change-log entries as Server-Sent Events so the UI
// reflects external (MCP/agent) writes without a restart or page reload.
//
// The cursor is the change-log id: ?after=<id> or the standard Last-Event-ID
// header on reconnect. A fresh connection with no cursor starts at "now" (the
// UI already loaded its data through the regular endpoints) instead of
// replaying history.
func (s *Server) handleAppEvents(w http.ResponseWriter, r *http.Request) {
	if s.opts.Reader == nil {
		writeError(w, http.StatusServiceUnavailable, "not_ready", "数据读取尚未初始化")
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	stop := context.AfterFunc(s.eventsCtx, cancel)
	defer stop()
	cursor := strings.TrimSpace(r.URL.Query().Get("after"))
	if cursor == "" {
		cursor = strings.TrimSpace(r.Header.Get("Last-Event-ID"))
	}
	replay := cursor != ""
	if cursor == "" {
		// No cursor: remember the newest entry before the response starts and
		// only stream what is appended from here on. Seeding before the
		// headers keeps this a single point in time (the client can rely on
		// its own catch-up load).
		if tail, err := s.opts.Reader.ListChangeLogsAfter(ctx, "", 1); err == nil && len(tail) > 0 {
			cursor = tail[len(tail)-1].ID
		}
	}

	// The server-wide WriteTimeout is absolute; clear it for this response so
	// the stream stays open between heartbeats. statusRecorder.Unwrap lets the
	// controller reach the real writer through the logging middleware.
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{})
	h := w.Header()
	h.Set("Content-Type", "text/event-stream; charset=utf-8")
	h.Set("Cache-Control", "no-cache, no-store")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	if _, err := fmt.Fprint(w, "retry: 3000\n\n"); err != nil {
		return
	}
	if rc.Flush() != nil {
		return
	}

	writeEvent := func(entry model.ChangeLog) bool {
		payload, err := json.Marshal(appEvent{
			ID: entry.ID, Entity: entry.Entity, EntityID: entry.EntityID,
			Action: entry.Action, Actor: entry.Actor, Summary: entry.Summary,
			CreatedAt: entry.CreatedAt,
		})
		if err != nil {
			return true // skip an unencodable row, keep the stream alive
		}
		if _, err := fmt.Fprintf(w, "id: %s\ndata: %s\n\n", entry.ID, payload); err != nil {
			return false
		}
		return rc.Flush() == nil
	}
	send := func(entries []model.ChangeLog) bool {
		for _, entry := range entries {
			if !writeEvent(entry) {
				return false
			}
			cursor = entry.ID
		}
		return true
	}

	if replay && cursor != "" {
		entries, err := s.opts.Reader.ListChangeLogsAfter(ctx, cursor, 200)
		if err != nil {
			if s.opts.Logger != nil {
				s.opts.Logger.Warn("event stream cursor query failed", "error", err)
			}
		} else if !send(entries) {
			return
		}
	}

	poll := time.NewTicker(s.eventPollInterval())
	defer poll.Stop()
	heartbeat := time.NewTicker(s.eventHeartbeatInterval())
	defer heartbeat.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": keep-alive\n\n"); err != nil {
				return
			}
			if rc.Flush() != nil {
				return
			}
		case <-poll.C:
			entries, err := s.opts.Reader.ListChangeLogsAfter(ctx, cursor, 200)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				if s.opts.Logger != nil {
					s.opts.Logger.Warn("event stream poll failed", "error", err)
				}
				continue
			}
			if !send(entries) {
				return
			}
		}
	}
}

// handleAppMCPStatus reports the connector's runtime state for the settings
// page. It is read-only: it never starts or stops the stdio connector.
func (s *Server) handleAppMCPStatus(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.appService(w)
	if !ok {
		return
	}
	ctx := r.Context()
	all, err := s.opts.Reader.ListChangeLogs(ctx, store.ChangeFilter{
		Actor: service.MCPActor, Limit: 1,
	})
	if err != nil {
		s.internalError(w, "count agent change log", err)
		return
	}
	recent, err := s.opts.Reader.ListChangeLogs(ctx, store.ChangeFilter{
		Actor: service.MCPActor, Since: time.Now().Add(-24 * time.Hour), Limit: 1,
	})
	if err != nil {
		s.internalError(w, "count recent agent change log", err)
		return
	}
	var lastAction any
	if len(all.Items) > 0 {
		last := all.Items[0]
		lastAction = map[string]any{
			"summary":    last.Summary,
			"entity":     last.Entity,
			"entity_id":  last.EntityID,
			"action":     last.Action,
			"created_at": last.CreatedAt,
		}
	}
	dataDir := svc.DataDir()
	command := "tengshou-cli mcp"
	configFile := ""
	if dataDir != "" {
		command += ` --data-dir "` + dataDir + `"`
		configFile = filepath.Join(dataDir, "config.json")
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"write_enabled":        s.mcpWriteEnabled(),
		"read_enabled":         true,
		"read_tools":           mcpspec.ReadToolCount,
		"write_tools":          mcpspec.WriteToolCount,
		"agent_actor":          service.MCPActor,
		"agent_actions_total":  all.Total,
		"agent_actions_recent": recent.Total,
		"last_agent_action":    lastAction,
		"data_dir":             dataDir,
		"config_file":          configFile,
		"transport":            "stdio",
		"command":              command,
		"settings_key":         "mcp.write_enabled",
		"write_switch_applies": "立即生效（下一次工具调用即按新开关判定）",
		"connection_help":      "在 Claude Code / Claude Desktop 中以 stdio 注册上面的命令；连接器只经本机回环访问腾手，不打开数据库。本机服务需保持运行。读工具始终可用；关闭写开关后写工具返回 mcp_write_disabled。",
	})
}

type mcpSettingsInput struct {
	WriteEnabled *bool `json:"write_enabled"`
}

// handleAppUpdateMCPSettings flips the MCP write switch. The write guard has
// already enforced loopback + X-Tengshou-App; agents themselves cannot call it
// (otherwise a write-capable agent could re-open the switch).
func (s *Server) handleAppUpdateMCPSettings(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get(HeaderMCP) == "1" {
		writeError(w, http.StatusForbidden, "settings_ui_only",
			"MCP 写开关只能由本机界面修改")
		return
	}
	svc, ok := s.appService(w)
	if !ok {
		return
	}
	if s.opts.Settings == nil {
		writeError(w, http.StatusServiceUnavailable, "settings_unavailable",
			"运行时设置尚未初始化")
		return
	}
	var in mcpSettingsInput
	if !s.readAppJSON(w, r, &in) {
		return
	}
	if in.WriteEnabled == nil {
		writeError(w, http.StatusBadRequest, "invalid_settings", "缺少 write_enabled")
		return
	}
	old := s.opts.Settings.MCPWriteEnabled()
	if err := s.opts.Settings.SetMCPWriteEnabled(*in.WriteEnabled); err != nil {
		s.internalError(w, "save mcp settings", err)
		return
	}
	if old != *in.WriteEnabled {
		svc.RecordMCPWriteEnabled(r.Context(), old, *in.WriteEnabled)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"write_enabled": s.opts.Settings.MCPWriteEnabled(),
	})
}

func (s *Server) handleAppExcelTemplate(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.appService(w)
	if !ok {
		return
	}
	data, err := svc.TemplateWorkbook(r.Context())
	if err != nil {
		s.internalError(w, "build template", err)
		return
	}
	writeXLSX(w, "tengshou-entry-template.xlsx", data)
}

func (s *Server) handleAppExcelLedger(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.appService(w)
	if !ok {
		return
	}
	data, err := svc.LedgerWorkbook(r.Context(), filter(r))
	if err != nil {
		s.internalError(w, "build ledger", err)
		return
	}
	name := "tengshou-ledger"
	if year := intParam(r, "year", 0); year > 0 {
		name = fmt.Sprintf("tengshou-ledger-%d", year)
	}
	writeXLSX(w, name+".xlsx", data)
}

type importRequest struct {
	FileBase64 string                `json:"file_base64"`
	Options    service.ImportOptions `json:"options"`
}

func (s *Server) handleAppExcelPreview(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.appService(w)
	if !ok {
		return
	}
	var in importRequest
	if !s.readAppJSON(w, r, &in) {
		return
	}
	data, err := decodeWorkbook(in.FileBase64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_file", err.Error())
		return
	}
	preview, err := svc.PreviewImport(r.Context(), data, in.Options)
	if err != nil {
		s.writeServiceError(w, "preview import", err)
		return
	}
	writeJSON(w, http.StatusOK, preview)
}

func (s *Server) handleAppExcelCommit(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.appService(w)
	if !ok {
		return
	}
	var in importRequest
	if !s.readAppJSON(w, r, &in) {
		return
	}
	data, err := decodeWorkbook(in.FileBase64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_file", err.Error())
		return
	}
	result, err := svc.CommitImport(r.Context(), data, in.Options)
	if err != nil {
		var be *service.Error
		if errors.As(err, &be) && be.Code == "import_errors" {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
				"error":  map[string]string{"code": be.Code, "message": be.Message},
				"result": result,
			})
			return
		}
		s.writeServiceError(w, "commit import", err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func decodeWorkbook(encoded string) ([]byte, error) {
	encoded = strings.TrimSpace(encoded)
	if encoded == "" {
		return nil, fmt.Errorf("缺少 Excel 文件内容")
	}
	if idx := strings.Index(encoded, ","); idx >= 0 && strings.HasPrefix(encoded, "data:") {
		encoded = encoded[idx+1:]
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("Excel 文件内容不是有效的 base64")
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("Excel 文件为空")
	}
	return data, nil
}

func writeXLSX(w http.ResponseWriter, filename string, data []byte) {
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+filename+"\"; filename*=UTF-8''"+
		urlEncodePath(filename))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func urlEncodePath(s string) string {
	var b strings.Builder
	for _, c := range []byte(s) {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
			c == '-' || c == '_' || c == '.' {
			b.WriteByte(c)
			continue
		}
		fmt.Fprintf(&b, "%%%02X", c)
	}
	return b.String()
}

func (s *Server) appService(w http.ResponseWriter) (*service.Service, bool) {
	if s.opts.Service == nil {
		writeError(w, http.StatusServiceUnavailable, "not_ready", "应用服务尚未初始化")
		return nil, false
	}
	return s.opts.Service, true
}

func (s *Server) readAppJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxAppBody)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "请求体不是有效的 JSON："+err.Error())
		return false
	}
	return true
}

func (s *Server) writeServiceError(w http.ResponseWriter, msg string, err error) {
	var validation *service.ValidationErrors
	if errors.As(err, &validation) {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
			"error": map[string]any{
				"code":    "validation",
				"message": validation.Error(),
				"fields":  validation.Items,
			},
		})
		return
	}
	var businessErr *service.Error
	if errors.As(err, &businessErr) {
		status := http.StatusBadRequest
		switch businessErr.Code {
		case "not_found":
			status = http.StatusNotFound
		case "conflict", "immutable", "in_use", "duplicate", "ambiguous_party",
			"preview_required", "missing_asset", "invalid_template", "content_changed":
			status = http.StatusConflict
		case "import_errors", "converter_unavailable", "convert_failed",
			"invalid_pdf", "invalid_kind", "too_many_placements",
			"invalid_backup", "confirm_required", "conflict_unresolved",
			"missing_signature", "missing_field", "missing_party", "missing_seal",
			"preflight_failed", "too_many_contracts":
			status = http.StatusUnprocessableEntity
		case "file_too_large", "backup_too_large":
			status = http.StatusRequestEntityTooLarge
		case "font_unavailable":
			status = http.StatusServiceUnavailable
		}
		writeError(w, status, businessErr.Code, businessErr.Message)
		return
	}
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "记录不存在")
		return
	}
	s.internalError(w, msg, err)
}
