package httpapi

import (
	"encoding/base64"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

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
	ContractID          string `json:"contract_id"`
	PreviewSHA256       string `json:"preview_sha256"`
	Kind                string `json:"kind"`
	SourceDocumentID    string `json:"source_document_id"`
	AllowStaleSignature bool   `json:"allow_stale_signature"`
}

type templatePreflightRequest struct {
	ContractID string `json:"contract_id"`
}

type templateBatchPreviewRequest struct {
	ContractIDs []string `json:"contract_ids"`
	Year        int      `json:"year"`
	Status      string   `json:"status"`
	Limit       int      `json:"limit"`
	Sample      bool     `json:"sample"`
}

type templateBatchGenerateRequest struct {
	Items               []service.BatchGenerateItemInput `json:"items"`
	Kind                string                           `json:"kind"`
	AllowStaleSignature bool                             `json:"allow_stale_signature"`
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
	// 导入要把 Word 文档转成 PDF，服务端转换上限是 conversionTimeout
	// （service 侧 3 分钟），而服务端 WriteTimeout 是 30s 的绝对截止时间。
	// 与 SSE 相同，先清除本响应的写截止时间；否则转换超过 30s 的导入响应
	// 永远写不回浏览器，界面一直停在「导入中」。
	_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
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

// handleAppTemplateCheck validates the calibrated placements of one template
// version against the current field definitions (B8 template wizard). It is
// read-only and never changes the template.
func (s *Server) handleAppTemplateCheck(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.appService(w)
	if !ok {
		return
	}
	res, err := svc.CheckTemplate(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeServiceError(w, "check template", err)
		return
	}
	writeJSON(w, http.StatusOK, res)
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
	res, err := svc.GenerateContractDocument(r.Context(), service.GenerateInput{
		TemplateID:          r.PathValue("id"),
		ContractID:          in.ContractID,
		PreviewSHA256:       in.PreviewSHA256,
		Kind:                in.Kind,
		SourceDocumentID:    in.SourceDocumentID,
		AllowStaleSignature: in.AllowStaleSignature,
	})
	if err != nil {
		s.writeServiceError(w, "generate contract pdf", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"item": res.Document, "usage": res.Usage, "warnings": res.Warnings,
	})
}

// handleAppPreflightGeneration reports whether one contract can be generated
// with the selected template version (required fields, party data, signatures
// and seals) and returns the preview digest generation must echo.
func (s *Server) handleAppPreflightGeneration(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.appService(w)
	if !ok {
		return
	}
	var in templatePreflightRequest
	if !s.readAppJSON(w, r, &in) {
		return
	}
	res, err := svc.PreflightContractGeneration(r.Context(), r.PathValue("id"), in.ContractID)
	if err != nil {
		s.writeServiceError(w, "preflight generation", err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// handleAppBatchPreviewGeneration reports per-contract readiness for a batch
// run and optionally returns one sample-data PDF of the template version.
func (s *Server) handleAppBatchPreviewGeneration(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.appService(w)
	if !ok {
		return
	}
	var in templateBatchPreviewRequest
	if !s.readAppJSON(w, r, &in) {
		return
	}
	res, err := svc.PreflightBatchGeneration(r.Context(), service.BatchPreviewInput{
		TemplateID:  r.PathValue("id"),
		ContractIDs: in.ContractIDs,
		Year:        in.Year,
		Status:      in.Status,
		Limit:       in.Limit,
		Sample:      in.Sample,
	})
	if err != nil {
		s.writeServiceError(w, "batch preview", err)
		return
	}
	payload := map[string]any{
		"template":              res.Template,
		"items":                 res.Items,
		"summary":               res.Summary,
		"sample_preview_sha256": res.SamplePreviewSHA256,
		"warnings":              res.Warnings,
	}
	if len(res.SamplePDF) > 0 {
		payload["pdf_base64"] = base64.StdEncoding.EncodeToString(res.SamplePDF)
	}
	writeJSON(w, http.StatusOK, payload)
}

// handleAppBatchGenerate renders several contracts with one template version.
// Failures are reported per contract; the request only fails when the template
// itself or the input is unusable.
func (s *Server) handleAppBatchGenerate(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.appService(w)
	if !ok {
		return
	}
	var in templateBatchGenerateRequest
	if !s.readAppJSON(w, r, &in) {
		return
	}
	res, err := svc.GenerateContractBatch(r.Context(), service.BatchGenerateInput{
		TemplateID:          r.PathValue("id"),
		Items:               in.Items,
		Kind:                in.Kind,
		AllowStaleSignature: in.AllowStaleSignature,
	})
	if err != nil {
		s.writeServiceError(w, "batch generate", err)
		return
	}
	writeJSON(w, http.StatusOK, res)
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
	if doc.Status == model.DocumentStatusFailed || strings.TrimSpace(doc.FilePath) == "" {
		writeError(w, http.StatusNotFound, "not_found", "这次生成没有输出文件（失败记录）")
		return
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
