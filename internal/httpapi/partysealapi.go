package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/LuSrackhall/tengshou/internal/model"
)

// Party archive seal and signature-stamp endpoints (te-48hkl).
//
// Images arrive as base64 data URLs, matching the ID-photo upload surface.
// Uploads are always recorded with the desktop-import source marker; phone
// self-enrollment images get lan_enroll in the enrollment submission instead.

type partyImageInput struct {
	Image string `json:"image"`
}

func (s *Server) handleAppUploadPartySeal(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.appService(w)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxPartyPhotoBody)
	var in partyImageInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "请求体不是有效的 JSON")
		return
	}
	raw, err := decodeImageDataURL(in.Image)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_image", err.Error())
		return
	}
	party, record, err := svc.SavePartySealImage(r.Context(), r.PathValue("id"), raw, model.PartyAssetSourceAppUpload)
	if err != nil {
		s.writeServiceError(w, "save party seal", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"item": party, "seal": record})
}

func (s *Server) handleAppPartySealImage(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.appService(w)
	if !ok {
		return
	}
	raw, err := svc.PartySealImage(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeServiceError(w, "read party seal", err)
		return
	}
	serveImage(w, "image/png", raw)
}

func (s *Server) handleAppClearPartySeal(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.appService(w)
	if !ok {
		return
	}
	party, err := svc.ClearPartySeal(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeServiceError(w, "disable party seal", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"item": party})
}

func (s *Server) handleAppUploadPartySignatureSeal(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.appService(w)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxPartyPhotoBody)
	var in partyImageInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "请求体不是有效的 JSON")
		return
	}
	raw, err := decodeImageDataURL(in.Image)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_image", err.Error())
		return
	}
	party, err := svc.SavePartySignatureSeal(r.Context(), r.PathValue("id"), raw, model.PartyAssetSourceAppUpload)
	if err != nil {
		s.writeServiceError(w, "save party signature seal", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"item": party})
}

func (s *Server) handleAppPartySignatureSealImage(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.appService(w)
	if !ok {
		return
	}
	raw, err := svc.PartySignatureSealImage(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeServiceError(w, "read party signature seal", err)
		return
	}
	serveImage(w, "image/png", raw)
}

func (s *Server) handleAppClearPartySignatureSeal(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.appService(w)
	if !ok {
		return
	}
	party, err := svc.ClearPartySignatureSeal(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeServiceError(w, "disable party signature seal", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"item": party})
}
