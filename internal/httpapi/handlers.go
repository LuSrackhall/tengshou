package httpapi

import (
	"embed"
	"encoding/json"
	"errors"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/LuSrackhall/tengshou/internal/model"
	"github.com/LuSrackhall/tengshou/internal/store"
)

//go:embed web/index.html web/sign.html
var webFS embed.FS

var indexTemplate = template.Must(template.ParseFS(webFS, "web/index.html"))
var signPageTemplate = template.Must(template.ParseFS(webFS, "web/sign.html"))

type pageData struct {
	Version       string
	URLs          []string
	AllowLAN      bool
	TokenRequired bool
	SchemaVersion string
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" && r.URL.Path != "/index.html" {
		http.NotFound(w, r)
		return
	}
	s.mu.RLock()
	urls := append([]string(nil), s.urls...)
	s.mu.RUnlock()
	if len(urls) == 0 {
		urls = s.URLs(nil)
	}
	schemaVersion := 0
	if s.opts.Reader != nil {
		schemaVersion, _ = s.opts.Reader.SchemaVersion(r.Context())
	}
	data := pageData{
		Version:       s.version(),
		URLs:          urls,
		AllowLAN:      s.opts.Config.HTTP.AllowLAN,
		TokenRequired: s.opts.Config.HTTP.Token != "",
		SchemaVersion: strconv.Itoa(schemaVersion),
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := indexTemplate.Execute(w, data); err != nil {
		if s.opts.Logger != nil {
			s.opts.Logger.Error("render index", "error", err)
		}
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	schemaVersion := 0
	if s.opts.Reader != nil {
		schemaVersion, _ = s.opts.Reader.SchemaVersion(r.Context())
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":          "ok",
		"version":         s.version(),
		"time":            time.Now().UTC().Format(time.RFC3339),
		"database_schema": schemaVersion,
	})
}

func (s *Server) handleAPIRoot(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"name":      "腾手本地数据接口",
		"version":   "v1",
		"read_only": true,
		"auth":      "Authorization: Bearer <token> or X-Api-Token: <token>",
		"schema":    "/api/v1/schema",
		"resources": []string{
			"/api/v1/stats",
			"/api/v1/fields",
			"/api/v1/parties",
			"/api/v1/parties/{id}",
			"/api/v1/contracts",
			"/api/v1/contracts/{id}",
			"/api/v1/templates",
			"/api/v1/templates/{id}",
			"/api/v1/signature-assets",
			"/api/v1/documents",
		},
	})
}

func (s *Server) handleListFields(w http.ResponseWriter, r *http.Request) {
	entity := strings.TrimSpace(r.URL.Query().Get("entity"))
	if entity != "" {
		fields, err := s.opts.Reader.ListFieldDefinitions(r.Context(), entity, false)
		if err != nil {
			s.internalError(w, "list fields", err)
			return
		}
		writeJSON(w, http.StatusOK, model.List[model.FieldDefinition]{
			Items: fields, Total: len(fields), Limit: len(fields), Offset: 0,
		})
		return
	}
	contractFields, err := s.opts.Reader.ListFieldDefinitions(r.Context(), model.FieldEntityContract, false)
	if err != nil {
		s.internalError(w, "list contract fields", err)
		return
	}
	partyFields, err := s.opts.Reader.ListFieldDefinitions(r.Context(), model.FieldEntityParty, false)
	if err != nil {
		s.internalError(w, "list party fields", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"contract": contractFields,
		"party":    partyFields,
	})
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	stats, err := s.opts.Reader.Stats(r.Context())
	if err != nil {
		s.internalError(w, "read stats", err)
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func (s *Server) handleListParties(w http.ResponseWriter, r *http.Request) {
	items, err := s.opts.Reader.ListParties(r.Context(), filter(r))
	if err != nil {
		s.internalError(w, "list parties", err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleGetParty(w http.ResponseWriter, r *http.Request) {
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

func (s *Server) handleListContracts(w http.ResponseWriter, r *http.Request) {
	f := filter(r)
	f.PartyID = strings.TrimSpace(r.URL.Query().Get("party_id"))
	items, err := s.opts.Reader.ListContracts(r.Context(), f)
	if err != nil {
		s.internalError(w, "list contracts", err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleGetContract(w http.ResponseWriter, r *http.Request) {
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

func (s *Server) handleListTemplates(w http.ResponseWriter, r *http.Request) {
	items, err := s.opts.Reader.ListTemplates(r.Context(), filter(r))
	if err != nil {
		s.internalError(w, "list templates", err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleGetTemplate(w http.ResponseWriter, r *http.Request) {
	tpl, err := s.opts.Reader.GetTemplate(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "template not found")
		return
	}
	if err != nil {
		s.internalError(w, "get template", err)
		return
	}
	writeJSON(w, http.StatusOK, tpl)
}

func (s *Server) handleListSignatures(w http.ResponseWriter, r *http.Request) {
	f := filter(r)
	if id := strings.TrimSpace(r.URL.Query().Get("contract_id")); id != "" {
		f.ContractID = id
	}
	items, err := s.opts.Reader.ListSignatures(r.Context(), f)
	if err != nil {
		s.internalError(w, "list signatures", err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleListDocuments(w http.ResponseWriter, r *http.Request) {
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

func filter(r *http.Request) model.ListFilter {
	f := model.ListFilter{
		Limit:  intParam(r, "limit", 50),
		Offset: intParam(r, "offset", 0),
		Query:  strings.TrimSpace(r.URL.Query().Get("q")),
		Status: strings.TrimSpace(r.URL.Query().Get("status")),
		Year:   intParam(r, "year", 0),
	}
	if f.Year < 0 {
		f.Year = 0
	}
	f.Normalize()
	return f
}

func intParam(r *http.Request, key string, def int) int {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	return n
}

func (s *Server) internalError(w http.ResponseWriter, msg string, err error) {
	if s.opts.Logger != nil {
		s.opts.Logger.Error(msg, "error", err)
	}
	writeError(w, http.StatusInternalServerError, "internal", "internal server error")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]string{"code": code, "message": message},
	})
}
