package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/LuSrackhall/tengshou/internal/service"
	"github.com/LuSrackhall/tengshou/internal/store"
)

// handleAppListAssets lists property archives. Filters: q (name/address/room
// number), status (enabled|disabled), limit, offset.
func (s *Server) handleAppListAssets(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.appService(w)
	if !ok {
		return
	}
	f := filter(r)
	switch f.Status {
	case "", "enabled", "disabled":
	default:
		writeError(w, http.StatusBadRequest, "invalid_status", "status 只能是 enabled 或 disabled")
		return
	}
	items, err := svc.ListAssets(r.Context(), f)
	if err != nil {
		s.internalError(w, "list assets", err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleAppGetAsset(w http.ResponseWriter, r *http.Request) {
	asset, err := s.opts.Reader.GetAsset(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "房产档案不存在")
		return
	}
	if err != nil {
		s.internalError(w, "get asset", err)
		return
	}
	writeJSON(w, http.StatusOK, asset)
}

func (s *Server) handleAppSaveAsset(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.appService(w)
	if !ok {
		return
	}
	var in service.AssetInput
	if !s.readAppJSON(w, r, &in) {
		return
	}
	if id := strings.TrimSpace(r.PathValue("id")); id != "" {
		in.ID = id
	}
	asset, warnings, err := svc.SaveAsset(r.Context(), in)
	if err != nil {
		s.writeServiceError(w, "save asset", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"item": asset, "warnings": warnings})
}

func (s *Server) handleAppDeleteAsset(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.appService(w)
	if !ok {
		return
	}
	if err := svc.DeleteAsset(r.Context(), r.PathValue("id")); err != nil {
		s.writeServiceError(w, "delete asset", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
}
