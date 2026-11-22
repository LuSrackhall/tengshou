package httpapi

import (
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/LuSrackhall/tengshou/internal/backup"
)

const maxRestoreUpload = backup.DefaultMaxUploadBytes

// handleAppBackupExport streams a complete backup package. The package is
// built first so failures can still return a proper error response; the
// download itself is a plain streamed zip, never base64 JSON.
func (s *Server) handleAppBackupExport(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.appService(w)
	if !ok {
		return
	}
	path, manifest, err := svc.CreateBackupFile(r.Context())
	if err != nil {
		s.writeServiceError(w, "create backup", err)
		return
	}
	defer os.Remove(path)
	f, err := os.Open(path)
	if err != nil {
		s.internalError(w, "open backup package", err)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		s.internalError(w, "stat backup package", err)
		return
	}
	name := "tengshou-backup-" + manifest.CreatedAt.Local().Format("20060102-150405") + ".zip"
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition",
		"attachment; filename=\""+name+"\"; filename*=UTF-8''"+urlEncodePath(name))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	w.WriteHeader(http.StatusOK)
	if _, err := io.Copy(w, f); err != nil && s.opts.Logger != nil {
		s.opts.Logger.Debug("backup download interrupted", "error", err)
	}
}

// handleAppBackupRestore accepts a multipart zip upload, validates it and
// stages it for the next restart. Nothing is replaced while the app runs.
func (s *Server) handleAppBackupRestore(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.appService(w)
	if !ok {
		return
	}
	if ct := strings.ToLower(r.Header.Get("Content-Type")); !strings.HasPrefix(ct, "multipart/form-data") {
		writeError(w, http.StatusUnsupportedMediaType, "multipart_required",
			"恢复接口需要 multipart/form-data 上传 .zip 备份包")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRestoreUpload)
	mr, err := r.MultipartReader()
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_multipart", "无法读取上传内容")
		return
	}
	var manifest backup.Manifest
	found := false
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_multipart", "上传内容不完整："+err.Error())
			return
		}
		if part.FormName() != "file" || found {
			part.Close()
			continue
		}
		manifest, err = svc.StageBackupRestore(r.Context(), part)
		part.Close()
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				writeError(w, http.StatusRequestEntityTooLarge, "backup_too_large",
					"备份包超过上传上限，请先拆分或精简后再试")
				return
			}
			s.writeServiceError(w, "stage backup restore", err)
			return
		}
		found = true
	}
	if !found {
		writeError(w, http.StatusBadRequest, "missing_file", "请选择要恢复的备份包（字段名 file）")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"item":             manifest,
		"restart_required": true,
		"message":          "备份包已校验并暂存。请关闭并重新打开腾手，恢复才会生效；当前数据已保持不动。",
	})
}

func (s *Server) handleAppBackupStatus(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.appService(w)
	if !ok {
		return
	}
	status, err := svc.BackupStatus()
	if err != nil {
		s.writeServiceError(w, "backup status", err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) handleAppIntegrity(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.appService(w)
	if !ok {
		return
	}
	report, err := svc.CheckIntegrity(r.Context())
	if err != nil {
		s.writeServiceError(w, "integrity check", err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

type signatureExportInput struct {
	Confirm bool `json:"confirm"`
}

// handleAppExportSignature exports one signature image after an explicit
// second confirmation and records the export in the change log.
func (s *Server) handleAppExportSignature(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.appService(w)
	if !ok {
		return
	}
	var in signatureExportInput
	if !s.readAppJSON(w, r, &in) {
		return
	}
	if !in.Confirm {
		writeError(w, http.StatusUnprocessableEntity, "confirm_required",
			"签名是敏感资产，导出需要二次确认")
		return
	}
	raw, name, err := svc.ExportSignature(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeServiceError(w, "export signature", err)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Content-Disposition",
		"attachment; filename=\""+name+"\"; filename*=UTF-8''"+urlEncodePath(name))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Length", strconv.Itoa(len(raw)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}
