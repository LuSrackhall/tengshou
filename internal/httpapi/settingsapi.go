package httpapi

import (
	"net/http"
)

// Settings page switches beyond MCP (te-48hkl): the archive signature/seal
// reuse master switch. Mutating requests stay loopback-only and carry
// X-Tengshou-App thanks to the app guard, exactly like the MCP switch.

type archiveReuseInput struct {
	Enabled *bool `json:"enabled"`
}

func (s *Server) handleAppArchiveReuseSettings(w http.ResponseWriter, r *http.Request) {
	if s.opts.Settings == nil {
		writeError(w, http.StatusServiceUnavailable, "settings_unavailable", "设置不可用，无法读取档案复用开关")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled": s.opts.Settings.ArchiveReuseEnabled(),
		"risk_notice": "复用档案签名、签名章与公章的法律风险由使用者自行承担；" +
			"请确认已获得租户/单位授权，并在需要时到合同里取消复用、现场重新签名或盖章。",
	})
}

func (s *Server) handleAppUpdateArchiveReuseSettings(w http.ResponseWriter, r *http.Request) {
	if s.opts.Settings == nil {
		writeError(w, http.StatusServiceUnavailable, "settings_unavailable", "设置不可用，无法保存档案复用开关")
		return
	}
	if r.Header.Get(HeaderMCP) == "1" {
		writeError(w, http.StatusForbidden, "settings_ui_only", "档案复用开关只能由本机界面修改")
		return
	}
	var in archiveReuseInput
	if !s.readAppJSON(w, r, &in) {
		return
	}
	if in.Enabled == nil {
		writeError(w, http.StatusBadRequest, "invalid_settings", "缺少 enabled")
		return
	}
	if err := s.opts.Settings.SetArchiveReuseEnabled(*in.Enabled); err != nil {
		s.internalError(w, "save archive reuse settings", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"enabled": s.opts.Settings.ArchiveReuseEnabled()})
}
