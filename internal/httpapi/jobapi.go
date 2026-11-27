package httpapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/LuSrackhall/tengshou/internal/service"
)

// This file wires the background-job start endpoints. Every POST mirrors the
// synchronous endpoint's request body and success payload; the only difference
// is that the work runs in the registry and is read back via GET /jobs/{id}.
// The synchronous routes stay in place for MCP and existing callers.

func (s *Server) handleAppGetJob(w http.ResponseWriter, r *http.Request) {
	if s.jobs == nil {
		writeError(w, http.StatusServiceUnavailable, "not_ready", "后台任务未初始化")
		return
	}
	snap, ok := s.jobs.Snapshot(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "任务不存在或已过期")
		return
	}
	writeJSON(w, http.StatusOK, snap)
}

// startJob claims a registry slot and answers 202 with the job id.
func (s *Server) startJob(w http.ResponseWriter, r *http.Request, kind string, run jobRun) {
	if s.jobs == nil {
		writeError(w, http.StatusServiceUnavailable, "not_ready", "后台任务未初始化")
		return
	}
	parent := s.jobsCtx
	if parent == nil {
		parent = context.Background()
	}
	if r.Header.Get(HeaderMCP) == "1" {
		parent = service.WithActor(parent, service.MCPActor)
	}
	id, err := s.jobs.Start(parent, kind, run)
	if err != nil {
		s.internalError(w, "start job", err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"job_id": id})
}

// jobContractPreviewRequest is the synchronous preview body plus template_id:
// the job path is keyed by contract, so the template version travels in the
// body (the synchronous route carries it in the URL).
type jobContractPreviewRequest struct {
	TemplateID string `json:"template_id"`
	ContractID string `json:"contract_id"`
	Sample     bool   `json:"sample"`
}

// jobContractGenerateRequest is the synchronous generate body plus template_id.
type jobContractGenerateRequest struct {
	TemplateID          string `json:"template_id"`
	ContractID          string `json:"contract_id"`
	PreviewSHA256       string `json:"preview_sha256"`
	Kind                string `json:"kind"`
	SourceDocumentID    string `json:"source_document_id"`
	AllowStaleSignature bool   `json:"allow_stale_signature"`
}

// jobContractIDs resolves the contract id from the path (authoritative) with
// the body as a fallback, and validates the template selection.
func jobContractIDs(w http.ResponseWriter, r *http.Request, templateID, bodyContractID string) (string, string, bool) {
	templateID = strings.TrimSpace(templateID)
	contractID := strings.TrimSpace(r.PathValue("id"))
	if contractID == "" {
		contractID = strings.TrimSpace(bodyContractID)
	}
	if templateID == "" {
		writeError(w, http.StatusBadRequest, "invalid_template", "请选择模板版本")
		return "", "", false
	}
	if contractID == "" {
		writeError(w, http.StatusBadRequest, "invalid_contract", "请选择要生成 PDF 的合同")
		return "", "", false
	}
	return templateID, contractID, true
}

func (s *Server) handleAppStartTemplateImportJob(w http.ResponseWriter, r *http.Request) {
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
	s.startJob(w, r, "template_import", func(ctx context.Context, report service.ProgressFunc) (any, error) {
		res, err := svc.ImportTemplate(ctx, service.TemplateImportInput{
			Name: in.Name, FileName: in.FileName, Data: data, Progress: report,
		})
		if err != nil {
			return nil, err
		}
		return templateImportPayload(res), nil
	})
}

func (s *Server) handleAppStartContractPreviewJob(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.appService(w)
	if !ok {
		return
	}
	var in jobContractPreviewRequest
	if !s.readAppJSON(w, r, &in) {
		return
	}
	templateID, contractID, ok := jobContractIDs(w, r, in.TemplateID, in.ContractID)
	if !ok {
		return
	}
	s.startJob(w, r, "contract_preview", func(ctx context.Context, report service.ProgressFunc) (any, error) {
		report(service.Progress{Stage: service.ProgressRendering, Message: "正在生成预览…"})
		res, err := svc.PreviewTemplate(ctx, templateID, service.TemplatePreviewInput{
			ContractID: contractID, Sample: in.Sample,
		})
		if err != nil {
			return nil, err
		}
		return contractPreviewPayload(res), nil
	})
}

func (s *Server) handleAppStartContractGenerateJob(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.appService(w)
	if !ok {
		return
	}
	var in jobContractGenerateRequest
	if !s.readAppJSON(w, r, &in) {
		return
	}
	templateID, contractID, ok := jobContractIDs(w, r, in.TemplateID, in.ContractID)
	if !ok {
		return
	}
	s.startJob(w, r, "contract_generate", func(ctx context.Context, report service.ProgressFunc) (any, error) {
		res, err := svc.GenerateContractDocument(ctx, service.GenerateInput{
			TemplateID:          templateID,
			ContractID:          contractID,
			PreviewSHA256:       in.PreviewSHA256,
			Kind:                in.Kind,
			SourceDocumentID:    in.SourceDocumentID,
			AllowStaleSignature: in.AllowStaleSignature,
			Progress:            report,
		})
		if err != nil {
			return nil, err
		}
		return contractGeneratePayload(res), nil
	})
}

func (s *Server) handleAppStartBatchPreviewJob(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.appService(w)
	if !ok {
		return
	}
	templateID := strings.TrimSpace(r.PathValue("id"))
	if templateID == "" {
		writeError(w, http.StatusBadRequest, "invalid_template", "请选择模板版本")
		return
	}
	var in templateBatchPreviewRequest
	if !s.readAppJSON(w, r, &in) {
		return
	}
	s.startJob(w, r, "batch_preview", func(ctx context.Context, report service.ProgressFunc) (any, error) {
		res, err := svc.PreflightBatchGeneration(ctx, service.BatchPreviewInput{
			TemplateID:  templateID,
			ContractIDs: in.ContractIDs,
			Year:        in.Year,
			Status:      in.Status,
			Limit:       in.Limit,
			Sample:      in.Sample,
			Progress:    report,
		})
		if err != nil {
			return nil, err
		}
		return batchPreviewPayload(res), nil
	})
}

func (s *Server) handleAppStartBatchGenerateJob(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.appService(w)
	if !ok {
		return
	}
	templateID := strings.TrimSpace(r.PathValue("id"))
	if templateID == "" {
		writeError(w, http.StatusBadRequest, "invalid_template", "请选择模板版本")
		return
	}
	var in templateBatchGenerateRequest
	if !s.readAppJSON(w, r, &in) {
		return
	}
	s.startJob(w, r, "batch_generate", func(ctx context.Context, report service.ProgressFunc) (any, error) {
		res, err := svc.GenerateContractBatch(ctx, service.BatchGenerateInput{
			TemplateID:          templateID,
			Items:               in.Items,
			Kind:                in.Kind,
			AllowStaleSignature: in.AllowStaleSignature,
			Progress:            report,
		})
		if err != nil {
			return nil, err
		}
		return res, nil
	})
}

func (s *Server) handleAppStartExcelPreviewJob(w http.ResponseWriter, r *http.Request) {
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
	s.startJob(w, r, "excel_preview", func(ctx context.Context, report service.ProgressFunc) (any, error) {
		opts := in.Options
		opts.Progress = report
		preview, err := svc.PreviewImport(ctx, data, opts)
		if err != nil {
			return nil, err
		}
		return preview, nil
	})
}

func (s *Server) handleAppStartExcelCommitJob(w http.ResponseWriter, r *http.Request) {
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
	s.startJob(w, r, "excel_commit", func(ctx context.Context, report service.ProgressFunc) (any, error) {
		opts := in.Options
		opts.Progress = report
		result, err := svc.CommitImport(ctx, data, opts)
		if err != nil {
			// import_errors keeps its per-row result alongside the error, just
			// like the synchronous 422 body.
			return result, err
		}
		return result, nil
	})
}
