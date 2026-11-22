package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/LuSrackhall/tengshou/internal/model"
	"github.com/LuSrackhall/tengshou/internal/service"
	"github.com/LuSrackhall/tengshou/internal/store"
)

const maxAppBody = 32 << 20 // 32 MiB, enough for a base64-encoded workbook

func (s *Server) handleAppAPIRoot(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"name":     "腾手应用接口",
		"version":  "app/v1",
		"loopback": "只允许本机访问",
		"header":   "写操作需携带 X-Tengshou-App: 1",
		"resources": []string{
			"/app/api/v1/bootstrap",
			"/app/api/v1/fields",
			"/app/api/v1/parties",
			"/app/api/v1/contracts",
			"/app/api/v1/contracts/years",
			"/app/api/v1/changelog",
			"/app/api/v1/seals",
			"/app/api/v1/seals/{id}",
			"/app/api/v1/seals/{id}/image",
			"/app/api/v1/seals/{id}/attach",
			"/app/api/v1/excel/template",
			"/app/api/v1/excel/ledger",
			"/app/api/v1/excel/preview",
			"/app/api/v1/excel/commit",
			"/app/api/v1/signature-sessions",
			"/app/api/v1/signature-sessions/{id}",
			"/app/api/v1/signature-sessions/{id}/qr.png",
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
		"version":        s.version(),
		"schema_version": schemaVersion,
		"operator":       svc.Actor(),
		"current_year":   time.Now().Year(),
		"years":          years,
		"stats":          stats,
		"now":            time.Now().UTC().Format(time.RFC3339),
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

func (s *Server) handleAppChangeLog(w http.ResponseWriter, r *http.Request) {
	f := store.ChangeFilter{
		Entity:   strings.TrimSpace(r.URL.Query().Get("entity")),
		EntityID: strings.TrimSpace(r.URL.Query().Get("entity_id")),
		Limit:    intParam(r, "limit", 100),
		Offset:   intParam(r, "offset", 0),
	}
	items, err := s.opts.Reader.ListChangeLogs(r.Context(), f)
	if err != nil {
		s.internalError(w, "list change log", err)
		return
	}
	writeJSON(w, http.StatusOK, items)
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
		case "conflict", "immutable", "in_use", "duplicate", "ambiguous_party":
			status = http.StatusConflict
		case "import_errors":
			status = http.StatusUnprocessableEntity
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
