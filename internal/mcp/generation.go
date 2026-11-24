package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// 生成与文档查询工具（B4 能力面）。预检与生成都复用桌面应用的
// internal/service 校验；连接器只做参数检查、预检阻断和结果投影。

// preflightIssue mirrors service.PreflightIssue on the wire.
type preflightIssue struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// generationPreflight is the subset of the preflight answer the generate tool
// needs before it may call the render endpoint.
type generationPreflight struct {
	ContractID        string           `json:"contract_id"`
	ContractNo        string           `json:"contract_no"`
	TemplateID        string           `json:"template_id"`
	TemplateName      string           `json:"template_name"`
	TemplateVersion   int              `json:"template_version"`
	PreviewSHA256     string           `json:"preview_sha256"`
	Status            string           `json:"status"`
	Ready             bool             `json:"ready"`
	Issues            []preflightIssue `json:"issues"`
	PreviousDocuments int              `json:"previous_documents"`
}

// preflightBlockedError reports every blocking issue in one tool error. The
// generate tool returns this before any write, so a blocked run leaves neither
// an output_documents row nor a file behind.
type preflightBlockedError struct {
	Issues []preflightIssue
}

func (e *preflightBlockedError) Error() string {
	if len(e.Issues) == 0 {
		return "生成前置检查未通过，未生成文件、未写入 PDF 记录"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "生成前置检查未通过（%d 项），未生成文件、未写入 PDF 记录：", len(e.Issues))
	for _, issue := range e.Issues {
		b.WriteString("\n- ")
		if issue.Code != "" {
			b.WriteString("[" + issue.Code + "] ")
		}
		b.WriteString(issue.Message)
	}
	b.WriteString("\n请转告用户先在腾手桌面应用内处理这些问题，然后重试。")
	return b.String()
}

type generationArgs struct {
	TemplateID string `json:"template_id"`
	ContractID string `json:"contract_id"`
}

func templateGenerationPath(templateID, action string) string {
	return "/app/api/v1/templates/" + url.PathEscape(templateID) + "/" + action
}

func requiredTemplateContract(templateID, contractID string) (string, string, error) {
	templateID = strings.TrimSpace(templateID)
	contractID = strings.TrimSpace(contractID)
	if templateID == "" {
		return "", "", fmt.Errorf("template_id 不能为空")
	}
	if contractID == "" {
		return "", "", fmt.Errorf("contract_id 不能为空")
	}
	return templateID, contractID, nil
}

// toolPreflightGeneration calls the read-only preflight endpoint. The raw
// server payload is returned unchanged so the agent sees every field
// (ready/issues/warnings/preview_sha256/signatures/previous_documents/...).
func (s *Server) toolPreflightGeneration(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	var in generationArgs
	if err := decodeArgs(args, &in); err != nil {
		return nil, err
	}
	templateID, contractID, err := requiredTemplateContract(in.TemplateID, in.ContractID)
	if err != nil {
		return nil, err
	}
	return s.api.PostRead(ctx, templateGenerationPath(templateID, "preflight"),
		map[string]string{"contract_id": contractID})
}

type generateContractArgs struct {
	TemplateID          string `json:"template_id"`
	ContractID          string `json:"contract_id"`
	AllowStaleSignature bool   `json:"allow_stale_signature,omitempty"`
	Kind                string `json:"kind,omitempty"`
}

func (s *Server) toolGenerateContract(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	var in generateContractArgs
	if err := decodeArgs(args, &in); err != nil {
		return nil, err
	}
	templateID, contractID, err := requiredTemplateContract(in.TemplateID, in.ContractID)
	if err != nil {
		return nil, err
	}
	kind := strings.ToLower(strings.TrimSpace(in.Kind))
	switch kind {
	case "", "initial", "regenerate":
	default:
		return nil, fmt.Errorf("kind 只能是 initial 或 regenerate（省略时按合同是否已有成功 PDF 自动判断）")
	}

	rawPreflight, err := s.api.PostRead(ctx, templateGenerationPath(templateID, "preflight"),
		map[string]string{"contract_id": contractID})
	if err != nil {
		return nil, fmt.Errorf("生成前置检查失败：%w", err)
	}
	var pf generationPreflight
	if err := json.Unmarshal(rawPreflight, &pf); err != nil {
		return nil, fmt.Errorf("解析前置检查结果失败：%w", err)
	}
	blocking := pf.Issues
	if in.AllowStaleSignature {
		// Mirrors the server: an explicit confirmation clears content_changed
		// only; every other blocking issue still stops the run.
		blocking = filterPreflightIssues(blocking, "content_changed")
	}
	if len(blocking) > 0 {
		return nil, &preflightBlockedError{Issues: blocking}
	}
	if pf.PreviewSHA256 == "" {
		return nil, fmt.Errorf("前置检查未返回 preview_sha256，无法生成；请确认模板可以预览")
	}

	payload := map[string]any{
		"contract_id":    contractID,
		"preview_sha256": pf.PreviewSHA256,
	}
	if kind != "" {
		payload["kind"] = kind
	}
	if in.AllowStaleSignature {
		payload["allow_stale_signature"] = true
	}
	raw, err := s.api.Post(ctx, templateGenerationPath(templateID, "generate"), payload)
	if err != nil {
		return nil, err
	}
	var generated struct {
		Item     json.RawMessage   `json:"item"`
		Warnings []json.RawMessage `json:"warnings"`
	}
	if err := json.Unmarshal(raw, &generated); err != nil {
		return nil, fmt.Errorf("解析生成结果失败：%w", err)
	}
	return json.Marshal(map[string]any{
		"item":     generated.Item,
		"warnings": generated.Warnings,
	})
}

func filterPreflightIssues(issues []preflightIssue, code string) []preflightIssue {
	out := make([]preflightIssue, 0, len(issues))
	for _, issue := range issues {
		if issue.Code == code {
			continue
		}
		out = append(out, issue)
	}
	return out
}

type listDocumentsArgs struct {
	ContractID string `json:"contract_id"`
	Status     string `json:"status"`
	Limit      int    `json:"limit"`
	Offset     int    `json:"offset"`
}

// documentWire decodes one /api/v1/documents row.
type documentWire struct {
	ID              string    `json:"id"`
	ContractID      string    `json:"contract_id"`
	TemplateID      string    `json:"template_id"`
	TemplateVersion int       `json:"template_version"`
	Kind            string    `json:"kind"`
	Status          string    `json:"status"`
	ErrorMessage    string    `json:"error_message"`
	CreatedAt       time.Time `json:"created_at"`
}

type documentWireList struct {
	Items  []documentWire `json:"items"`
	Total  int            `json:"total"`
	Limit  int            `json:"limit"`
	Offset int            `json:"offset"`
}

// documentRecord is the projected list row: PDF records carry opaque template
// ids, so the connector resolves the contract number and template name for the
// agent and exposes the download path the desktop app serves.
type documentRecord struct {
	ID              string    `json:"id"`
	ContractID      string    `json:"contract_id,omitempty"`
	ContractNo      string    `json:"contract_no,omitempty"`
	TemplateID      string    `json:"template_id,omitempty"`
	TemplateName    string    `json:"template_name,omitempty"`
	TemplateVersion int       `json:"template_version"`
	Kind            string    `json:"kind"`
	Status          string    `json:"status"`
	CreatedAt       time.Time `json:"created_at"`
	DownloadPath    string    `json:"download_path"`
	ErrorMessage    string    `json:"error_message,omitempty"`
}

type documentList struct {
	Items  []documentRecord `json:"items"`
	Total  int              `json:"total"`
	Limit  int              `json:"limit"`
	Offset int              `json:"offset"`
}

func (s *Server) toolListDocuments(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	var in listDocumentsArgs
	if err := decodeArgs(args, &in); err != nil {
		return nil, err
	}
	q, err := listQuery("", in.Limit, in.Offset)
	if err != nil {
		return nil, err
	}
	if id := strings.TrimSpace(in.ContractID); id != "" {
		q.Set("contract_id", id)
	}
	switch status := strings.ToLower(strings.TrimSpace(in.Status)); status {
	case "":
	case "success", "failed":
		q.Set("status", status)
	default:
		return nil, fmt.Errorf("status 只能是 success 或 failed")
	}

	raw, err := s.api.Get(ctx, "/api/v1/documents", q)
	if err != nil {
		return nil, err
	}
	var page documentWireList
	if err := json.Unmarshal(raw, &page); err != nil {
		return nil, fmt.Errorf("解析文档列表失败：%w", err)
	}

	contractNos := map[string]string{}
	templateNames := map[string]string{}
	items := make([]documentRecord, 0, len(page.Items))
	for _, doc := range page.Items {
		rec := documentRecord{
			ID:              doc.ID,
			ContractID:      doc.ContractID,
			TemplateID:      doc.TemplateID,
			TemplateVersion: doc.TemplateVersion,
			Kind:            doc.Kind,
			Status:          doc.Status,
			CreatedAt:       doc.CreatedAt,
			DownloadPath:    "/app/api/v1/documents/" + url.PathEscape(doc.ID) + "/file",
			ErrorMessage:    doc.ErrorMessage,
		}
		rec.ContractNo = s.contractNo(ctx, doc.ContractID, contractNos)
		rec.TemplateName = s.templateName(ctx, doc.TemplateID, templateNames)
		items = append(items, rec)
	}
	if page.Limit <= 0 {
		page.Limit = 50
	}
	return json.Marshal(documentList{
		Items: items, Total: page.Total, Limit: page.Limit, Offset: page.Offset,
	})
}

// contractNo resolves a contract number once per list call. A missing or
// deleted contract leaves the field empty instead of failing the whole list;
// the PDF record itself stays queryable.
func (s *Server) contractNo(ctx context.Context, id string, cache map[string]string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	if no, ok := cache[id]; ok {
		return no
	}
	raw, err := s.api.Get(ctx, "/api/v1/contracts/"+url.PathEscape(id), nil)
	if err != nil {
		s.logger.Debug("mcp list documents: resolve contract failed", "contract_id", id, "error", err)
		return ""
	}
	var detail struct {
		Contract struct {
			ContractNo string `json:"contract_no"`
		} `json:"contract"`
	}
	if err := json.Unmarshal(raw, &detail); err != nil {
		s.logger.Debug("mcp list documents: decode contract failed", "contract_id", id, "error", err)
		return ""
	}
	cache[id] = detail.Contract.ContractNo
	return detail.Contract.ContractNo
}

// templateName resolves a template family name once per list call. Template
// names are shared across versions; the row's own template_version stays the
// exact version used for generation.
func (s *Server) templateName(ctx context.Context, id string, cache map[string]string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	if name, ok := cache[id]; ok {
		return name
	}
	raw, err := s.api.Get(ctx, "/api/v1/templates/"+url.PathEscape(id), nil)
	if err != nil {
		s.logger.Debug("mcp list documents: resolve template failed", "template_id", id, "error", err)
		return ""
	}
	var tpl struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &tpl); err != nil {
		s.logger.Debug("mcp list documents: decode template failed", "template_id", id, "error", err)
		return ""
	}
	cache[id] = tpl.Name
	return tpl.Name
}
