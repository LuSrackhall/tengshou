package httpapi

import (
	"encoding/base64"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/LuSrackhall/tengshou/internal/model"
	"github.com/LuSrackhall/tengshou/internal/service"
	"github.com/LuSrackhall/tengshou/internal/store"
)

// maxTemplateBytes bounds the decoded import file (the JSON body carries it
// base64-encoded; maxAppBody is sized accordingly).
const maxTemplateBytes = 40 << 20

type templateImportRequest struct {
	Name       string `json:"name"`
	FileName   string `json:"file_name"`
	FileBase64 string `json:"file_base64"`
}

type templatePreviewRequest struct {
	ContractID string `json:"contract_id"`
	Sample     bool   `json:"sample"`
}

type templateGenerateRequest struct {
	ContractID    string `json:"contract_id"`
	PreviewSHA256 string `json:"preview_sha256"`
}

func (s *Server) handleAppListTemplates(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.appService(w)
	if !ok {
		return
	}
	f := filter(r)
	f.Kind = strings.TrimSpace(r.URL.Query().Get("kind"))
	items, err := svc.ListTemplates(r.Context(), f)
	if err != nil {
		s.internalError(w, "list templates", err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleAppGetTemplate(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.appService(w)
	if !ok {
		return
	}
	tpl, err := svc.GetTemplate(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeServiceError(w, "get template", err)
		return
	}
	writeJSON(w, http.StatusOK, tpl)
}

func (s *Server) handleAppImportTemplate(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.appService(w)
	if !ok {
		return
	}
	var in templateImportRequest
	if !s.readAppJSON(w, r, &in) {
		return
	}
	data, err := decodeTemplateFile(in.FileBase64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_file", err.Error())
		return
	}
	res, err := svc.ImportTemplate(r.Context(), service.TemplateImportInput{
		Name: in.Name, FileName: in.FileName, Data: data,
	})
	if err != nil {
		s.writeServiceError(w, "import template", err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"item": res.Template, "usage": res.Usage, "warnings": res.Warnings,
	})
}

func (s *Server) handleAppSaveTemplateMapping(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.appService(w)
	if !ok {
		return
	}
	var in service.TemplateSaveInput
	if !s.readAppJSON(w, r, &in) {
		return
	}
	res, err := svc.SaveTemplateMapping(r.Context(), r.PathValue("id"), in)
	if err != nil {
		s.writeServiceError(w, "save template mapping", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"item": res.Template, "usage": res.Usage, "warnings": res.Warnings,
	})
}

func (s *Server) handleAppDeleteTemplate(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.appService(w)
	if !ok {
		return
	}
	if err := svc.DeleteTemplateVersion(r.Context(), r.PathValue("id")); err != nil {
		s.writeServiceError(w, "delete template", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
}

func (s *Server) handleAppTemplateUsage(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.appService(w)
	if !ok {
		return
	}
	usage, err := svc.TemplateUsage(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeServiceError(w, "template usage", err)
		return
	}
	writeJSON(w, http.StatusOK, usage)
}

// handleAppTemplatePDF serves the converted template PDF for the calibration
// editor (PDF.js fetches it same-origin).
func (s *Server) handleAppTemplatePDF(w http.ResponseWriter, r *http.Request) {
	tpl, ok := s.templateForRequest(w, r)
	if !ok {
		return
	}
	if strings.TrimSpace(tpl.PDFPath) == "" {
		writeError(w, http.StatusNotFound, "not_found", "模板没有 PDF 文件")
		return
	}
	s.serveAssetFile(w, r, tpl.PDFPath, "application/pdf", false,
		"template-v"+strconv.Itoa(tpl.Version)+".pdf")
}

// handleAppTemplateSource serves the original imported Word/PDF file.
func (s *Server) handleAppTemplateSource(w http.ResponseWriter, r *http.Request) {
	tpl, ok := s.templateForRequest(w, r)
	if !ok {
		return
	}
	if strings.TrimSpace(tpl.SourcePath) == "" {
		writeError(w, http.StatusNotFound, "not_found", "模板没有原始文件")
		return
	}
	ext := strings.ToLower(filepath.Ext(tpl.SourcePath))
	contentType := "application/octet-stream"
	switch ext {
	case ".pdf":
		contentType = "application/pdf"
	case ".docx":
		contentType = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	}
	name := "template-source" + ext
	s.serveAssetFile(w, r, tpl.SourcePath, contentType, true, name)
}

func (s *Server) templateForRequest(w http.ResponseWriter, r *http.Request) (model.Template, bool) {
	tpl, err := s.opts.Reader.GetTemplate(r.Context(), r.PathValue("id"))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "模板不存在")
			return model.Template{}, false
		}
		s.internalError(w, "get template", err)
		return model.Template{}, false
	}
	return tpl, true
}

func (s *Server) serveAssetFile(w http.ResponseWriter, r *http.Request, rel, contentType string, attachment bool, filename string) {
	svc, ok := s.appService(w)
	if !ok {
		return
	}
	full, err := assetFilePath(svc.DataDir(), rel)
	if err != nil {
		s.internalError(w, "resolve asset", err)
		return
	}
	raw, err := os.ReadFile(full)
	if errors.Is(err, os.ErrNotExist) {
		writeError(w, http.StatusNotFound, "not_found", "文件不存在")
		return
	}
	if err != nil {
		s.internalError(w, "read asset", err)
		return
	}
	disposition := "inline"
	if attachment {
		disposition = "attachment"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", disposition+"; filename=\""+filename+"\"; filename*=UTF-8''"+
		urlEncodePath(filename))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Length", strconv.Itoa(len(raw)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}

func (s *Server) handleAppPreviewTemplate(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.appService(w)
	if !ok {
		return
	}
	var in templatePreviewRequest
	if !s.readAppJSON(w, r, &in) {
		return
	}
	res, err := svc.PreviewTemplate(r.Context(), r.PathValue("id"), service.TemplatePreviewInput{
		ContractID: in.ContractID, Sample: in.Sample,
	})
	if err != nil {
		s.writeServiceError(w, "preview template", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"pdf_base64":     base64.StdEncoding.EncodeToString(res.PDF),
		"preview_sha256": res.PreviewSHA256,
		"warnings":       res.Warnings,
		"page_count":     res.PageCount,
		"sample":         res.Sample,
		"contract_id":    res.ContractID,
	})
}

func (s *Server) handleAppGenerateContract(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.appService(w)
	if !ok {
		return
	}
	var in templateGenerateRequest
	if !s.readAppJSON(w, r, &in) {
		return
	}
	res, err := svc.GenerateContractPDF(r.Context(), r.PathValue("id"), in.ContractID, in.PreviewSHA256)
	if err != nil {
		s.writeServiceError(w, "generate contract pdf", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"item": res.Document, "usage": res.Usage, "warnings": res.Warnings,
	})
}

func (s *Server) handleAppConverters(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.appService(w)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": svc.Converters()})
}

// handleAppDocumentFile serves a generated PDF so the user can open, print or
// download it from the desktop UI.
func (s *Server) handleAppDocumentFile(w http.ResponseWriter, r *http.Request) {
	doc, err := s.opts.Reader.GetDocument(r.Context(), r.PathValue("id"))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "PDF 记录不存在")
			return
		}
		s.internalError(w, "get document", err)
		return
	}
	name := filepath.Base(doc.FilePath)
	if name == "" || name == "." {
		name = "contract"
	}
	// Keep the quoted fallback filename ASCII-safe; filename* carries the
	// real name for browsers that understand it.
	s.serveAssetFile(w, r, doc.FilePath, "application/pdf", false, name)
}

func decodeTemplateFile(encoded string) ([]byte, error) {
	encoded = strings.TrimSpace(encoded)
	if encoded == "" {
		return nil, errors.New("请选择要导入的 Word 或 PDF 文件")
	}
	if idx := strings.Index(encoded, ","); idx >= 0 && strings.HasPrefix(encoded, "data:") {
		encoded = encoded[idx+1:]
	}
	// base64 is 4/3 of the decoded size; reject before allocating.
	if len(encoded) > (maxTemplateBytes/3+1)*4 {
		return nil, errors.New("模板文件过大，请压缩到 40MB 以内")
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, errors.New("文件内容不是有效的 base64")
	}
	if len(raw) == 0 {
		return nil, errors.New("文件为空")
	}
	if len(raw) > maxTemplateBytes {
		return nil, errors.New("模板文件过大，请压缩到 40MB 以内")
	}
	return raw, nil
}
