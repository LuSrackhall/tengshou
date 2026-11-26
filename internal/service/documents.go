package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/LuSrackhall/tengshou/internal/model"
	"github.com/LuSrackhall/tengshou/internal/overlay"
)

// maxBatchContracts bounds one batch preflight/generation request so a local
// UI action cannot accidentally queue thousands of renders.
const maxBatchContracts = 200

// GenerateInput configures one PDF generation attempt (B4).
type GenerateInput struct {
	TemplateID    string `json:"template_id"`
	ContractID    string `json:"contract_id"`
	PreviewSHA256 string `json:"preview_sha256"`
	// Kind is initial or regenerate; empty picks regenerate when the contract
	// already has a successful output.
	Kind string `json:"kind,omitempty"`
	// SourceDocumentID records the document being replaced by a regenerate.
	// Empty uses the newest successful document of the contract.
	SourceDocumentID string `json:"source_document_id,omitempty"`
	// AllowStaleSignature explicitly confirms reusing signatures collected
	// before a contract content change. The confirmation is audited.
	AllowStaleSignature bool `json:"allow_stale_signature,omitempty"`
	// BatchID groups documents produced by one batch run.
	BatchID string `json:"batch_id,omitempty"`
}

// Preflight issue/status values. The UI maps status to Chinese labels.
const (
	PreflightReady          = "ready"
	PreflightNeedsSignature = "needs_signature"
	PreflightNeedsResign    = "needs_resign"
	PreflightMissingFields  = "missing_fields"
	PreflightMissingParty   = "missing_party"
	PreflightMissingSeal    = "missing_seal"
	PreflightBlocked        = "blocked"
)

// PreflightIssue is one blocking problem found before generation.
type PreflightIssue struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// PreflightResult is the per-contract generation readiness report used by the
// single-contract panel and by batch previews.
type PreflightResult struct {
	ContractID   string `json:"contract_id"`
	ContractNo   string `json:"contract_no"`
	Title        string `json:"title"`
	TemplateID   string `json:"template_id"`
	TemplateName string `json:"template_name,omitempty"`
	// TemplateVersion is the version selected for generation.
	TemplateVersion int    `json:"template_version"`
	ContentSHA256   string `json:"content_sha256,omitempty"`
	// PreviewSHA256 is the digest a generate request must echo.
	PreviewSHA256 string                            `json:"preview_sha256,omitempty"`
	Status        string                            `json:"status"`
	Ready         bool                              `json:"ready"`
	Issues        []PreflightIssue                  `json:"issues"`
	Warnings      []overlay.Warning                 `json:"warnings"`
	Signatures    []model.DocumentSignatureSnapshot `json:"signatures"`
	// PreviousDocuments counts successful PDFs already generated for the
	// contract; >0 means this run is a regenerate.
	PreviousDocuments int `json:"previous_documents"`
	// PinnedTemplateID/Version describe the version the contract currently
	// references (before this generation pins the selected one).
	PinnedTemplateID      string `json:"pinned_template_id,omitempty"`
	PinnedTemplateName    string `json:"pinned_template_name,omitempty"`
	PinnedTemplateVersion int    `json:"pinned_template_version,omitempty"`
}

// BatchPreviewInput selects contracts for a batch readiness report.
type BatchPreviewInput struct {
	TemplateID  string   `json:"template_id"`
	ContractIDs []string `json:"contract_ids"`
	// Year and Status are used when ContractIDs is empty.
	Year   int    `json:"year,omitempty"`
	Status string `json:"status,omitempty"`
	Limit  int    `json:"limit,omitempty"`
	// Sample renders one sample-data PDF of the selected template version.
	Sample bool `json:"sample"`
}

// BatchPreviewSummary counts ready and blocked contracts.
type BatchPreviewSummary struct {
	Total   int `json:"total"`
	Ready   int `json:"ready"`
	Blocked int `json:"blocked"`
}

// BatchPreviewResult is the per-contract readiness report before a batch run.
type BatchPreviewResult struct {
	Template            model.Template      `json:"template"`
	Items               []PreflightResult   `json:"items"`
	Summary             BatchPreviewSummary `json:"summary"`
	SamplePreviewSHA256 string              `json:"sample_preview_sha256,omitempty"`
	Warnings            []overlay.Warning   `json:"warnings,omitempty"`
	// SamplePDF is base64-encoded by the HTTP layer.
	SamplePDF []byte `json:"-"`
}

// BatchGenerateItemInput is one contract to render in a batch.
type BatchGenerateItemInput struct {
	ContractID       string `json:"contract_id"`
	PreviewSHA256    string `json:"preview_sha256"`
	SourceDocumentID string `json:"source_document_id,omitempty"`
}

// BatchGenerateInput runs a batch over previously previewed contracts.
type BatchGenerateInput struct {
	TemplateID          string                   `json:"template_id"`
	Items               []BatchGenerateItemInput `json:"items"`
	Kind                string                   `json:"kind,omitempty"`
	AllowStaleSignature bool                     `json:"allow_stale_signature,omitempty"`
}

// BatchGenerateItemResult is the outcome for one contract.
type BatchGenerateItemResult struct {
	ContractID   string                `json:"contract_id"`
	ContractNo   string                `json:"contract_no,omitempty"`
	Title        string                `json:"title,omitempty"`
	Success      bool                  `json:"success"`
	Document     *model.OutputDocument `json:"document,omitempty"`
	ErrorCode    string                `json:"error_code,omitempty"`
	ErrorMessage string                `json:"error_message,omitempty"`
	Warnings     []overlay.Warning     `json:"warnings,omitempty"`
}

// BatchGenerateSummary counts batch outcomes.
type BatchGenerateSummary struct {
	Total   int `json:"total"`
	Success int `json:"success"`
	Failed  int `json:"failed"`
}

// BatchGenerateResult reports each contract independently; one failure never
// stops the remaining contracts.
type BatchGenerateResult struct {
	BatchID         string                    `json:"batch_id"`
	TemplateID      string                    `json:"template_id"`
	TemplateName    string                    `json:"template_name,omitempty"`
	TemplateVersion int                       `json:"template_version"`
	Items           []BatchGenerateItemResult `json:"items"`
	Summary         BatchGenerateSummary      `json:"summary"`
}

// generationPreflight is the internal readiness state of one render context.
type generationPreflight struct {
	issues     []PreflightIssue
	warnings   []overlay.Warning
	signatures []model.DocumentSignatureSnapshot
	stale      []model.Signature
	content    string
}

func (pf *generationPreflight) hasCode(code string) bool {
	for _, issue := range pf.issues {
		if issue.Code == code {
			return true
		}
	}
	return false
}

// PreflightContractGeneration checks one contract before generation: required
// fields, counterparty archive, template version, signatures (present and
// collected for the same content version) and seal images. It also returns
// the preview digest the generate request must echo.
func (s *Service) PreflightContractGeneration(ctx context.Context, templateID, contractID string) (PreflightResult, error) {
	tpl, err := s.GetTemplate(ctx, templateID)
	if err != nil {
		return PreflightResult{}, err
	}
	if tpl.Kind != model.TemplateKindPDF {
		return PreflightResult{}, business("invalid_kind", "只有 PDF 模板可以生成套印 PDF")
	}
	contractID = strings.TrimSpace(contractID)
	if contractID == "" {
		return PreflightResult{}, business("invalid_contract", "请选择要生成 PDF 的合同")
	}
	_, pages, err := s.templatePages(tpl)
	if err != nil {
		return PreflightResult{}, err
	}
	if _, err := s.validatePlacements(ctx, tpl.Placements, pages); err != nil {
		return PreflightResult{}, err
	}
	rc, err := s.loadTemplateContext(ctx, tpl, contractID, false)
	if err != nil {
		return PreflightResult{}, err
	}
	pf, err := s.preflightRenderContext(rc)
	if err != nil {
		return PreflightResult{}, err
	}
	return s.preflightResult(ctx, tpl, rc, pf), nil
}

// PreflightBatchGeneration reports, for every selected contract (or every
// contract matching the year/status filter), whether it can be generated and
// why not. Sample additionally renders one sample-data PDF of the selected
// template version for visual inspection.
func (s *Service) PreflightBatchGeneration(ctx context.Context, in BatchPreviewInput) (BatchPreviewResult, error) {
	tpl, err := s.GetTemplate(ctx, in.TemplateID)
	if err != nil {
		return BatchPreviewResult{}, err
	}
	if tpl.Kind != model.TemplateKindPDF {
		return BatchPreviewResult{}, business("invalid_kind", "只有 PDF 模板可以生成套印 PDF")
	}
	_, pages, err := s.templatePages(tpl)
	if err != nil {
		return BatchPreviewResult{}, err
	}
	if _, err := s.validatePlacements(ctx, tpl.Placements, pages); err != nil {
		return BatchPreviewResult{}, err
	}
	contracts, err := s.batchContracts(ctx, in)
	if err != nil {
		return BatchPreviewResult{}, err
	}
	if len(contracts) == 0 {
		return BatchPreviewResult{}, business("invalid_contracts", "没有可生成 PDF 的合同")
	}
	items := make([]PreflightResult, 0, len(contracts))
	for i := range contracts {
		c := contracts[i]
		rc, err := s.loadTemplateContext(ctx, tpl, c.ID, false)
		if err != nil {
			items = append(items, failedPreflight(c, tpl, err))
			continue
		}
		pf, err := s.preflightRenderContext(rc)
		if err != nil {
			items = append(items, failedPreflight(c, tpl, err))
			continue
		}
		items = append(items, s.preflightResult(ctx, tpl, rc, pf))
	}
	res := BatchPreviewResult{Template: tpl, Items: items}
	for _, item := range items {
		res.Summary.Total++
		if item.Ready {
			res.Summary.Ready++
		} else {
			res.Summary.Blocked++
		}
	}
	if in.Sample {
		preview, err := s.PreviewTemplate(ctx, tpl.ID, TemplatePreviewInput{Sample: true})
		if err != nil {
			return res, err
		}
		res.SamplePDF = preview.PDF
		res.SamplePreviewSHA256 = preview.PreviewSHA256
		res.Warnings = preview.Warnings
	}
	return res, nil
}

// GenerateContractBatch renders several already-previewed contracts. Each
// item success/failure is recorded independently; one failure never aborts the
// remaining items.
func (s *Service) GenerateContractBatch(ctx context.Context, in BatchGenerateInput) (BatchGenerateResult, error) {
	tpl, err := s.GetTemplate(ctx, in.TemplateID)
	if err != nil {
		return BatchGenerateResult{}, err
	}
	if tpl.Kind != model.TemplateKindPDF {
		return BatchGenerateResult{}, business("invalid_kind", "只有 PDF 模板可以生成套印 PDF")
	}
	if len(in.Items) == 0 {
		return BatchGenerateResult{}, business("invalid_contracts", "请选择要生成 PDF 的合同")
	}
	if len(in.Items) > maxBatchContracts {
		return BatchGenerateResult{}, business("too_many_contracts",
			"一次最多生成 %d 份合同 PDF，请分批处理", maxBatchContracts)
	}
	batchID, err := model.NewID("batch")
	if err != nil {
		return BatchGenerateResult{}, err
	}
	res := BatchGenerateResult{
		BatchID: batchID, TemplateID: tpl.ID, TemplateName: tpl.Name,
		TemplateVersion: tpl.Version, Items: []BatchGenerateItemResult{},
	}
	for _, item := range in.Items {
		ir := BatchGenerateItemResult{ContractID: strings.TrimSpace(item.ContractID)}
		if ir.ContractID == "" {
			ir.ErrorCode, ir.ErrorMessage = "invalid_contract", "合同标识为空"
			res.Items = append(res.Items, ir)
			res.Summary.Failed++
			continue
		}
		if c, err := s.rd.GetContract(ctx, ir.ContractID); err == nil {
			ir.ContractNo, ir.Title = c.ContractNo, c.Title
		}
		out, err := s.generateContractDocument(ctx, tpl, GenerateInput{
			TemplateID: tpl.ID, ContractID: ir.ContractID,
			PreviewSHA256: item.PreviewSHA256, Kind: in.Kind,
			SourceDocumentID:    item.SourceDocumentID,
			AllowStaleSignature: in.AllowStaleSignature, BatchID: batchID,
		})
		if err != nil {
			ir.ErrorCode = errorCodeOf(err)
			ir.ErrorMessage = err.Error()
			res.Items = append(res.Items, ir)
			res.Summary.Failed++
			continue
		}
		doc := out.Document
		ir.Success = true
		ir.Document = &doc
		ir.Warnings = out.Warnings
		res.Items = append(res.Items, ir)
		res.Summary.Success++
	}
	res.Summary.Total = len(res.Items)
	return res, nil
}

// GenerateContractDocument is the B4 generation entry point. It enforces the
// preview digest, required fields, counterparty data, signature presence and
// signature content version before rendering, records every attempt (success
// or failure) in output_documents, and audits who generated what.
func (s *Service) GenerateContractDocument(ctx context.Context, in GenerateInput) (TemplateRenderResult, error) {
	tpl, err := s.GetTemplate(ctx, in.TemplateID)
	if err != nil {
		return TemplateRenderResult{}, err
	}
	return s.generateContractDocument(ctx, tpl, in)
}

func (s *Service) generateContractDocument(ctx context.Context, tpl model.Template, in GenerateInput) (TemplateRenderResult, error) {
	if tpl.Kind != model.TemplateKindPDF {
		return TemplateRenderResult{}, business("invalid_kind", "只有 PDF 模板可以生成套印 PDF")
	}
	contractID := strings.TrimSpace(in.ContractID)
	if contractID == "" {
		return TemplateRenderResult{}, business("invalid_contract", "请选择要生成 PDF 的合同")
	}
	pdfAbs, pages, err := s.templatePages(tpl)
	if err != nil {
		return TemplateRenderResult{}, err
	}
	placements, err := s.validatePlacements(ctx, tpl.Placements, pages)
	if err != nil {
		return TemplateRenderResult{}, err
	}
	rc, err := s.loadTemplateContext(ctx, tpl, contractID, false)
	if err != nil {
		return TemplateRenderResult{}, err
	}
	kind := s.resolveDocumentKind(ctx, rc.contract.ID, in.Kind)
	sourceID := strings.TrimSpace(in.SourceDocumentID)
	if kind == model.DocumentKindRegenerate && sourceID == "" {
		latest, err := s.rd.LatestSuccessfulDocument(ctx, rc.contract.ID)
		if err != nil {
			slog.Warn("整改重出未找到来源文档，继续生成",
				"contract_id", rc.contract.ID, "error", err)
		} else {
			sourceID = latest.ID
		}
	}
	want := templatePreviewSHA(tpl, rc)
	if strings.TrimSpace(in.PreviewSHA256) != want {
		return TemplateRenderResult{}, business("preview_required",
			"生成前必须先在预览中确认版式与数据。若预览后修改了合同内容，请重新预览。")
	}
	pf, err := s.preflightRenderContext(rc)
	if err != nil {
		return TemplateRenderResult{}, err
	}
	blocking := pf.issues
	if in.AllowStaleSignature {
		blocking = filterIssues(blocking, "content_changed")
	}
	if len(blocking) > 0 {
		message := issueMessages(blocking)
		s.recordFailedDocument(ctx, tpl, rc, pf, kind, in.BatchID, sourceID, message)
		return TemplateRenderResult{}, business(preflightErrorCode(blocking), "%s", message)
	}
	family, err := s.FontFamily()
	if err != nil {
		s.recordFailedDocument(ctx, tpl, rc, pf, kind, in.BatchID, sourceID, err.Error())
		return TemplateRenderResult{}, err
	}
	built, err := overlay.Build(overlay.Options{
		Pages:      pages,
		Placements: placements,
		Font:       family,
		Resolver:   &templateResolver{rc: rc},
	})
	if err != nil {
		message := "生成 PDF 失败：" + err.Error()
		s.recordFailedDocument(ctx, tpl, rc, pf, kind, in.BatchID, sourceID, message)
		return TemplateRenderResult{}, business("render_failed", "%s", message)
	}
	composed, err := overlay.Compose(pdfAbs, built.PDF)
	if err != nil {
		message := "生成 PDF 失败：" + err.Error()
		s.recordFailedDocument(ctx, tpl, rc, pf, kind, in.BatchID, sourceID, message)
		return TemplateRenderResult{}, business("render_failed", "%s", message)
	}
	now := s.now()
	outName := fmt.Sprintf("%s-v%d-%s-%d.pdf", rc.contract.ID, tpl.Version,
		now.Format("20060102-150405"), time.Now().UnixNano()%1_000_000)
	outRel := path.Join("outputs", outName)
	outAbs, err := s.assetFile(outRel)
	if err != nil {
		s.recordFailedDocument(ctx, tpl, rc, pf, kind, in.BatchID, sourceID, err.Error())
		return TemplateRenderResult{}, err
	}
	if err := os.MkdirAll(filepath.Dir(outAbs), 0o700); err != nil {
		s.recordFailedDocument(ctx, tpl, rc, pf, kind, in.BatchID, sourceID, err.Error())
		return TemplateRenderResult{}, err
	}
	if err := writeFileAtomic(outAbs, composed, 0o600); err != nil {
		message := "保存 PDF 失败：" + err.Error()
		s.recordFailedDocument(ctx, tpl, rc, pf, kind, in.BatchID, sourceID, message)
		return TemplateRenderResult{}, fmt.Errorf("保存 PDF 失败：%w", err)
	}
	doc := model.OutputDocument{
		ContractID:       rc.contract.ID,
		TemplateID:       tpl.ID,
		TemplateVersion:  tpl.Version,
		FilePath:         filepath.ToSlash(outRel),
		SHA256:           sha256Hex(composed),
		PageCount:        len(pages),
		ContentSHA256:    pf.content,
		Signatures:       pf.signatures,
		Operator:         s.actorFor(ctx),
		Kind:             kind,
		Status:           model.DocumentStatusSuccess,
		BatchID:          strings.TrimSpace(in.BatchID),
		SourceDocumentID: sourceID,
		CreatedAt:        now,
	}
	if err := s.st.PutOutputDocument(ctx, &doc); err != nil {
		s.removeAsset(outRel)
		return TemplateRenderResult{}, err
	}
	if rc.contract.TemplateID != tpl.ID {
		before := rc.contract.TemplateID
		rc.contract.TemplateID = tpl.ID
		if err := s.st.PutContract(ctx, &rc.contract); err != nil {
			return TemplateRenderResult{}, err
		}
		s.logChange(ctx, model.ChangeEntityContract, rc.contract.ID, model.ChangeActionUpdate,
			fmt.Sprintf("合同模板版本更新为「%s」v%d", tpl.Name, tpl.Version),
			[]model.FieldChange{{Field: "template_id", Label: "模板", Old: before, New: tpl.ID}})
	}
	summary := fmt.Sprintf("按模板「%s」v%d 生成 PDF（%d 页，sha256 %s…）",
		tpl.Name, tpl.Version, len(pages), shortSHA(doc.SHA256))
	if kind == model.DocumentKindRegenerate {
		summary = fmt.Sprintf("按模板「%s」v%d 整改重出 PDF（%d 页，sha256 %s…）",
			tpl.Name, tpl.Version, len(pages), shortSHA(doc.SHA256))
		if sourceID != "" {
			summary += "，来源文档 " + sourceID
		}
	}
	s.logChange(ctx, model.ChangeEntityContract, rc.contract.ID, model.ChangeActionGenerate, summary, nil)
	if in.AllowStaleSignature && len(pf.stale) > 0 {
		names := make([]string, 0, len(pf.stale))
		for _, sig := range pf.stale {
			names = append(names, sig.SignerName)
		}
		s.logChange(ctx, model.ChangeEntityContract, rc.contract.ID, model.ChangeActionGenerate,
			fmt.Sprintf("确认保留旧签名重出 PDF（签名人：%s；签名采集于合同内容修改前）",
				strings.Join(names, "、")),
			[]model.FieldChange{{
				Field: "signature_reuse", Label: "旧签名复用", Old: "", New: "已确认",
			}})
	}
	warnings := append([]overlay.Warning{}, built.Warnings...)
	warnings = append(warnings, staleSignatureWarnings(rc)...)
	usage, err := s.rd.TemplateUsage(ctx, tpl.ID)
	if err != nil {
		return TemplateRenderResult{}, err
	}
	return TemplateRenderResult{Document: doc, Usage: usage, Warnings: warnings}, nil
}

// preflightRenderContext evaluates all blocking conditions for one render
// context. It is shared by single and batch preflight and by generation.
func (s *Service) preflightRenderContext(rc *templateRenderContext) (*generationPreflight, error) {
	if rc == nil {
		return nil, fmt.Errorf("render context is nil")
	}
	pf := &generationPreflight{
		issues:     []PreflightIssue{},
		warnings:   []overlay.Warning{},
		signatures: []model.DocumentSignatureSnapshot{},
	}
	content, err := ContentSHA256(rc.contract)
	if err != nil {
		return nil, err
	}
	pf.content = content

	usesPartyA := templateUsesParty(rc.template, model.PlacementPartyA)
	if rc.partyB == nil {
		pf.issues = append(pf.issues, PreflightIssue{
			Code: "missing_party", Message: "合同没有相对方（乙方）档案，请先在档案页补齐",
		})
	}
	if usesPartyA && rc.partyA == nil {
		pf.issues = append(pf.issues, PreflightIssue{
			Code: "missing_party", Message: "模板引用了甲方信息，但合同没有甲方档案",
		})
	}
	if templateUsesAsset(rc.template) && rc.asset == nil {
		pf.issues = append(pf.issues, PreflightIssue{
			Code:    "missing_asset_record",
			Message: "模板引用了房产信息，但合同没有选择房产档案，请先在合同表单选择",
		})
	}

	contractFields, err := s.rd.ListFieldDefinitions(rc.ctx, model.FieldEntityContract, false)
	if err != nil {
		return nil, err
	}
	for _, field := range contractFields {
		if !field.Required {
			continue
		}
		if strings.TrimSpace(rc.requiredContractValue(field)) == "" {
			pf.issues = append(pf.issues, PreflightIssue{
				Code: "missing_field", Message: fmt.Sprintf("必填字段「%s」为空", field.Label),
			})
		}
	}
	partyFields, err := s.rd.ListFieldDefinitions(rc.ctx, model.FieldEntityParty, false)
	if err != nil {
		return nil, err
	}
	checkParty := func(party *model.Party, role string) {
		if party == nil {
			return
		}
		for _, field := range partyFields {
			if !field.Required {
				continue
			}
			if strings.TrimSpace(rc.requiredPartyValue(party, field)) == "" {
				pf.issues = append(pf.issues, PreflightIssue{
					Code:    "missing_field",
					Message: fmt.Sprintf("相对方（%s）必填字段「%s」为空", role, field.Label),
				})
			}
		}
	}
	checkParty(rc.partyB, "乙方")
	if usesPartyA {
		checkParty(rc.partyA, "甲方")
	}

	seenSig := map[string]bool{}
	seenStale := map[string]bool{}
	for _, p := range rc.template.Placements {
		if p.Type != model.PlacementSignature {
			continue
		}
		sig, ok := rc.signatureFor(p)
		if !ok {
			pf.issues = append(pf.issues, PreflightIssue{
				Code: "missing_signature",
				Message: fmt.Sprintf("签名位置「%s」没有%s签名，请先扫码采集",
					p.ID, partyRoleLabel(p.Party)),
			})
			continue
		}
		if !seenSig[sig.ID] {
			seenSig[sig.ID] = true
			pf.signatures = append(pf.signatures, model.DocumentSignatureSnapshot{
				SignatureID:   sig.ID,
				PartyID:       sig.PartyID,
				SignerName:    sig.SignerName,
				SHA256:        sig.SHA256,
				ContentSHA256: sig.ContentSHA256,
			})
			if !s.assetExists(sig.AssetPath) {
				pf.issues = append(pf.issues, PreflightIssue{
					Code: "missing_signature",
					Message: fmt.Sprintf("签名「%s」的图片文件缺失，请重新采集",
						sig.SignerName),
				})
			}
		}
		if sig.ContentSHA256 != "" && sig.ContentSHA256 != content && !seenStale[sig.ID] {
			seenStale[sig.ID] = true
			pf.stale = append(pf.stale, sig)
			pf.issues = append(pf.issues, PreflightIssue{
				Code: "content_changed",
				Message: fmt.Sprintf("签名「%s」采集于合同内容修改前（内容版本 %s…），不能自动复用",
					sig.SignerName, shortSHA(sig.ContentSHA256)),
			})
		}
	}

	seenSealFile := map[string]bool{}
	for _, p := range rc.template.Placements {
		if p.Type != model.PlacementSeal {
			continue
		}
		seal, ok, err := rc.sealFor(p)
		if err != nil {
			pf.issues = append(pf.issues, PreflightIssue{
				Code:    "missing_seal",
				Message: fmt.Sprintf("公章位置「%s」不可用：%s", p.ID, err.Error()),
			})
			continue
		}
		if !ok {
			pf.issues = append(pf.issues, PreflightIssue{
				Code: "missing_seal", Message: fmt.Sprintf("公章位置「%s」没有可用的公章图片", p.ID),
			})
			continue
		}
		if !seenSealFile[seal.ID] {
			seenSealFile[seal.ID] = true
			if !s.assetExists(seal.AssetPath) {
				pf.issues = append(pf.issues, PreflightIssue{
					Code:    "missing_seal",
					Message: fmt.Sprintf("公章「%s」的图片文件缺失，请重新导入", seal.Name),
				})
			}
		}
	}

	pf.warnings = append(pf.warnings, staleSignatureWarnings(rc)...)
	return pf, nil
}

// preflightResult materializes a user-facing readiness report.
func (s *Service) preflightResult(ctx context.Context, tpl model.Template, rc *templateRenderContext, pf *generationPreflight) PreflightResult {
	res := PreflightResult{
		ContractID:       rc.contract.ID,
		ContractNo:       rc.contract.ContractNo,
		Title:            rc.contract.Title,
		TemplateID:       tpl.ID,
		TemplateName:     tpl.Name,
		TemplateVersion:  tpl.Version,
		ContentSHA256:    pf.content,
		PreviewSHA256:    templatePreviewSHA(tpl, rc),
		Status:           preflightStatus(pf.issues),
		Ready:            len(pf.issues) == 0,
		Issues:           pf.issues,
		Warnings:         pf.warnings,
		Signatures:       pf.signatures,
		PinnedTemplateID: rc.contract.TemplateID,
	}
	if rc.contract.TemplateID != "" {
		if pinned, err := s.rd.GetTemplate(ctx, rc.contract.TemplateID); err == nil {
			res.PinnedTemplateName = pinned.Name
			res.PinnedTemplateVersion = pinned.Version
		}
	}
	if docs, err := s.rd.ListDocuments(ctx, model.ListFilter{
		ContractID: rc.contract.ID, Status: model.DocumentStatusSuccess, Limit: 1,
	}); err == nil {
		res.PreviousDocuments = docs.Total
	}
	return res
}

// GenerateContractPDF keeps the B3 single-generation entry point: it now runs
// the full B4 checks and records the attempt.
func (s *Service) GenerateContractPDF(ctx context.Context, id, contractID, previewSHA string) (TemplateRenderResult, error) {
	return s.GenerateContractDocument(ctx, GenerateInput{
		TemplateID: id, ContractID: contractID, PreviewSHA256: previewSHA,
	})
}

func (s *Service) resolveDocumentKind(ctx context.Context, contractID, requested string) string {
	switch strings.ToLower(strings.TrimSpace(requested)) {
	case model.DocumentKindInitial:
		return model.DocumentKindInitial
	case model.DocumentKindRegenerate:
		return model.DocumentKindRegenerate
	}
	docs, err := s.rd.ListDocuments(ctx, model.ListFilter{
		ContractID: contractID, Status: model.DocumentStatusSuccess, Limit: 1,
	})
	if err == nil && docs.Total > 0 {
		return model.DocumentKindRegenerate
	}
	return model.DocumentKindInitial
}

// recordFailedDocument keeps a failed generation attempt so the user can see
// what failed and retry. Failures never point at a file.
func (s *Service) recordFailedDocument(ctx context.Context, tpl model.Template, rc *templateRenderContext,
	pf *generationPreflight, kind, batchID, sourceID, message string) {
	if rc == nil || rc.contract.ID == "" {
		return
	}
	content := ""
	signatures := []model.DocumentSignatureSnapshot{}
	if pf != nil {
		content = pf.content
		signatures = pf.signatures
	}
	doc := model.OutputDocument{
		ContractID:       rc.contract.ID,
		TemplateID:       tpl.ID,
		TemplateVersion:  tpl.Version,
		ContentSHA256:    content,
		Signatures:       signatures,
		Operator:         s.actorFor(ctx),
		Kind:             kind,
		Status:           model.DocumentStatusFailed,
		ErrorMessage:     truncateRunes(strings.TrimSpace(message), 500),
		BatchID:          strings.TrimSpace(batchID),
		SourceDocumentID: strings.TrimSpace(sourceID),
		CreatedAt:        s.now(),
	}
	if err := s.st.PutOutputDocument(ctx, &doc); err != nil {
		slog.Error("记录生成失败尝试失败",
			"contract_id", rc.contract.ID, "template_id", tpl.ID, "error", err)
	}
	s.logChange(ctx, model.ChangeEntityContract, rc.contract.ID, model.ChangeActionGenerate,
		fmt.Sprintf("生成 PDF 失败（模板「%s」v%d）：%s", tpl.Name, tpl.Version,
			truncateRunes(message, 200)), nil)
}

func (s *Service) batchContracts(ctx context.Context, in BatchPreviewInput) ([]model.Contract, error) {
	seen := map[string]bool{}
	ids := make([]string, 0, len(in.ContractIDs))
	for _, raw := range in.ContractIDs {
		id := strings.TrimSpace(raw)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if len(ids) > maxBatchContracts {
		return nil, business("too_many_contracts",
			"一次最多生成 %d 份合同 PDF，请分批处理", maxBatchContracts)
	}
	if len(ids) > 0 {
		out := make([]model.Contract, 0, len(ids))
		for _, id := range ids {
			c, err := s.rd.GetContract(ctx, id)
			if err != nil {
				if isNotFound(err) {
					return nil, business("not_found", "合同不存在：%s", id)
				}
				return nil, err
			}
			out = append(out, c)
		}
		return out, nil
	}
	limit := in.Limit
	if limit <= 0 || limit > maxBatchContracts {
		limit = maxBatchContracts
	}
	list, err := s.rd.ListContracts(ctx, model.ListFilter{
		Year: in.Year, Status: strings.TrimSpace(in.Status), Limit: limit,
	})
	if err != nil {
		return nil, err
	}
	return list.Items, nil
}

func failedPreflight(c model.Contract, tpl model.Template, err error) PreflightResult {
	return PreflightResult{
		ContractID:      c.ID,
		ContractNo:      c.ContractNo,
		Title:           c.Title,
		TemplateID:      tpl.ID,
		TemplateName:    tpl.Name,
		TemplateVersion: tpl.Version,
		Status:          PreflightBlocked,
		Issues:          []PreflightIssue{{Code: "load_failed", Message: err.Error()}},
	}
}

func preflightStatus(issues []PreflightIssue) string {
	has := func(code string) bool {
		for _, issue := range issues {
			if issue.Code == code {
				return true
			}
		}
		return false
	}
	switch {
	case len(issues) == 0:
		return PreflightReady
	case has("content_changed"):
		return PreflightNeedsResign
	case has("missing_signature"):
		return PreflightNeedsSignature
	case has("missing_party"):
		return PreflightMissingParty
	case has("missing_field"):
		return PreflightMissingFields
	case has("missing_seal"):
		return PreflightMissingSeal
	default:
		return PreflightBlocked
	}
}

func preflightErrorCode(issues []PreflightIssue) string {
	for _, code := range []string{"content_changed", "missing_signature", "missing_party", "missing_field", "missing_seal"} {
		for _, issue := range issues {
			if issue.Code == code {
				return code
			}
		}
	}
	return "preflight_failed"
}

func issueMessages(issues []PreflightIssue) string {
	parts := make([]string, 0, len(issues))
	for _, issue := range issues {
		parts = append(parts, issue.Message)
	}
	return strings.Join(parts, "；")
}

func filterIssues(issues []PreflightIssue, code string) []PreflightIssue {
	out := make([]PreflightIssue, 0, len(issues))
	for _, issue := range issues {
		if issue.Code == code {
			continue
		}
		out = append(out, issue)
	}
	return out
}

func (rc *templateRenderContext) requiredContractValue(field model.FieldDefinition) string {
	if field.BuiltIn {
		return rc.contractValue(field.Key)
	}
	return formatDynamicValue(field, rc.contract.Data[field.Key])
}

func (rc *templateRenderContext) requiredPartyValue(party *model.Party, field model.FieldDefinition) string {
	if field.BuiltIn {
		return rc.partyValue(party, field.Key)
	}
	return formatDynamicValue(field, party.Data[field.Key])
}

// templateUsesParty reports whether the template binds anything to one party.
func templateUsesParty(tpl model.Template, role string) bool {
	prefix := role + "."
	for _, p := range tpl.Placements {
		switch p.Type {
		case model.PlacementField:
			if strings.HasPrefix(strings.TrimSpace(p.Field), prefix) {
				return true
			}
		case model.PlacementSignature, model.PlacementSeal:
			if p.Party == role {
				return true
			}
		}
	}
	return false
}

// templateUsesAsset reports whether any field placement binds contract.asset_*
// values, which need a selected property archive.
func templateUsesAsset(tpl model.Template) bool {
	for _, p := range tpl.Placements {
		if p.Type == model.PlacementField &&
			strings.HasPrefix(strings.TrimSpace(p.Field), "contract.asset_") {
			return true
		}
	}
	return false
}

func partyRoleLabel(role string) string {
	switch role {
	case model.PlacementPartyA:
		return "甲方"
	case model.PlacementPartyB:
		return "乙方"
	}
	return "签约方"
}

func (s *Service) assetExists(rel string) bool {
	if strings.TrimSpace(rel) == "" {
		return false
	}
	full, err := s.assetFile(rel)
	if err != nil {
		return false
	}
	info, err := os.Stat(full)
	return err == nil && !info.IsDir()
}

func errorCodeOf(err error) string {
	var businessErr *Error
	if errors.As(err, &businessErr) {
		return businessErr.Code
	}
	return "internal"
}
