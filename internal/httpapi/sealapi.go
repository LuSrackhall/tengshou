package httpapi

import (
	"encoding/base64"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/LuSrackhall/tengshou/internal/service"
	"github.com/LuSrackhall/tengshou/internal/store"
)

// maxSealBytes bounds the decoded picture. The normalized PNG is much smaller;
// this only protects the service from absurd uploads.
const maxSealBytes = 12 << 20

type sealImportInput struct {
	Name       string `json:"name"`
	PartyID    string `json:"party_id"`
	Note       string `json:"note"`
	FileBase64 string `json:"file_base64"`
}

type sealReplaceInput struct {
	FileBase64 string `json:"file_base64"`
}

type sealAttachInput struct {
	ContractIDs []string `json:"contract_ids"`
}

func (s *Server) handleAppListSeals(w http.ResponseWriter, r *http.Request) {
	f := filter(r)
	f.PartyID = strings.TrimSpace(r.URL.Query().Get("party_id"))
	items, err := s.opts.Reader.ListSeals(r.Context(), f)
	if err != nil {
		s.internalError(w, "list seals", err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleAppGetSeal(w http.ResponseWriter, r *http.Request) {
	seal, err := s.opts.Reader.GetSeal(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "公章图片不存在")
		return
	}
	if err != nil {
		s.internalError(w, "get seal", err)
		return
	}
	writeJSON(w, http.StatusOK, seal)
}

func (s *Server) handleAppImportSeal(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.appService(w)
	if !ok {
		return
	}
	var in sealImportInput
	if !s.readAppJSON(w, r, &in) {
		return
	}
	image, err := decodeSealImage(in.FileBase64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_image", err.Error())
		return
	}
	seal, err := svc.ImportSeal(r.Context(), service.SealInput{
		Name: in.Name, PartyID: in.PartyID, Note: in.Note, Image: image,
	})
	if err != nil {
		s.writeServiceError(w, "import seal", err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"item": seal})
}

func (s *Server) handleAppUpdateSeal(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.appService(w)
	if !ok {
		return
	}
	var in service.SealInput
	if !s.readAppJSON(w, r, &in) {
		return
	}
	seal, err := svc.UpdateSeal(r.Context(), r.PathValue("id"), in)
	if err != nil {
		s.writeServiceError(w, "update seal", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"item": seal})
}

func (s *Server) handleAppReplaceSealImage(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.appService(w)
	if !ok {
		return
	}
	var in sealReplaceInput
	if !s.readAppJSON(w, r, &in) {
		return
	}
	image, err := decodeSealImage(in.FileBase64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_image", err.Error())
		return
	}
	seal, err := svc.ReplaceSealImage(r.Context(), r.PathValue("id"), image)
	if err != nil {
		s.writeServiceError(w, "replace seal image", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"item": seal})
}

func (s *Server) handleAppDeleteSeal(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.appService(w)
	if !ok {
		return
	}
	if err := svc.DeleteSeal(r.Context(), r.PathValue("id")); err != nil {
		s.writeServiceError(w, "delete seal", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
}

// handleAppSealContracts lists the contracts a seal is linked to so the UI can
// preselect them in the batch association dialog.
func (s *Server) handleAppSealContracts(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if _, err := s.opts.Reader.GetSeal(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "公章图片不存在")
			return
		}
		s.internalError(w, "get seal", err)
		return
	}
	ids, err := s.opts.Reader.SealContractIDs(r.Context(), id)
	if err != nil {
		s.internalError(w, "list seal contracts", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"contract_ids": ids})
}

func (s *Server) handleAppAttachSeal(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.appService(w)
	if !ok {
		return
	}
	var in sealAttachInput
	if !s.readAppJSON(w, r, &in) {
		return
	}
	attached, err := svc.AttachSealToContracts(r.Context(), r.PathValue("id"), in.ContractIDs)
	if err != nil {
		s.writeServiceError(w, "attach seal", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"attached": attached})
}

func (s *Server) handleAppDetachSeal(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.appService(w)
	if !ok {
		return
	}
	var in sealAttachInput
	if !s.readAppJSON(w, r, &in) {
		return
	}
	removed, err := svc.DetachSealFromContracts(r.Context(), r.PathValue("id"), in.ContractIDs)
	if err != nil {
		s.writeServiceError(w, "detach seal", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"detached": removed})
}

// handleAppSealImage serves the stored PNG for previews. The path comes from
// the database but is still checked against the data directory before opening.
func (s *Server) handleAppSealImage(w http.ResponseWriter, r *http.Request) {
	seal, err := s.opts.Reader.GetSeal(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "公章图片不存在")
		return
	}
	if err != nil {
		s.internalError(w, "get seal image", err)
		return
	}
	svc, ok := s.appService(w)
	if !ok {
		return
	}
	full, err := assetFilePath(svc.DataDir(), seal.AssetPath)
	if err != nil {
		s.internalError(w, "resolve seal image", err)
		return
	}
	raw, err := os.ReadFile(full)
	if errors.Is(err, os.ErrNotExist) {
		writeError(w, http.StatusNotFound, "not_found", "公章图片文件不存在")
		return
	}
	if err != nil {
		s.internalError(w, "read seal image", err)
		return
	}
	contentType := seal.MIME
	if contentType == "" {
		contentType = "image/png"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Length", strconv.Itoa(len(raw)))
	w.Header().Set("ETag", `"`+seal.SHA256+`"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}

func assetFilePath(root, rel string) (string, error) {
	if strings.TrimSpace(root) == "" {
		return "", errors.New("data directory is not configured")
	}
	if strings.Contains(rel, "://") || strings.HasPrefix(strings.ToLower(rel), "data:") ||
		filepath.IsAbs(rel) || strings.HasPrefix(rel, "/") || strings.HasPrefix(rel, `\`) {
		return "", errors.New("invalid seal asset path")
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", errors.New("seal asset path escapes the data directory")
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	full, err := filepath.Abs(filepath.Join(rootAbs, clean))
	if err != nil {
		return "", err
	}
	if full != rootAbs && !strings.HasPrefix(full, rootAbs+string(filepath.Separator)) {
		return "", errors.New("seal asset path escapes the data directory")
	}
	return full, nil
}

func decodeSealImage(encoded string) ([]byte, error) {
	encoded = strings.TrimSpace(encoded)
	if encoded == "" {
		return nil, errors.New("请选择公章图片文件")
	}
	if idx := strings.Index(encoded, ","); idx >= 0 && strings.HasPrefix(encoded, "data:") {
		encoded = encoded[idx+1:]
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, errors.New("公章图片不是有效的 base64")
	}
	if len(raw) == 0 {
		return nil, errors.New("公章图片为空")
	}
	if len(raw) > maxSealBytes {
		return nil, errors.New("公章图片文件过大，请压缩到 12MB 以内")
	}
	return raw, nil
}
