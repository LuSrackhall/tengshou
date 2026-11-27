package httpapi

import (
	"net/http"
)

// Settings page switches beyond MCP (te-48hkl): the archive signature/seal
// reuse notice switch. Reuse itself is always available; the switch only
// controls whether the contract form shows the "will reuse archive assets
// (source: archive)" notice and the fallback hint (Q6 修正). Mutating requests
// stay loopback-only and carry X-Tengshou-App thanks to the app guard, exactly
// like the MCP switch.

type archiveReuseInput struct {
	Notify *bool `json:"notify"`
}

func (s *Server) handleAppArchiveReuseSettings(w http.ResponseWriter, r *http.Request) {
	if s.opts.Settings == nil {
		writeError(w, http.StatusServiceUnavailable, "settings_unavailable", "设置不可用，无法读取档案复用告知开关")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"notify": s.opts.Settings.ArchiveReuseNotify(),
		"risk_notice": "本开关只控制是否展示档案复用的告知提示，复用功能始终可用。" +
			"关闭后仍会按档案静默复用签名、签名章与公章，不再显示来源与回退提示；" +
			"复用档案签名、签名章与公章的法律风险由使用者自行承担，请确认已获得租户/单位授权。" +
			"合同变更记录仍在本机内部记录来源（档案 / 现场扫码 / 手动），不向对方展示。",
	})
}

func (s *Server) handleAppUpdateArchiveReuseSettings(w http.ResponseWriter, r *http.Request) {
	if s.opts.Settings == nil {
		writeError(w, http.StatusServiceUnavailable, "settings_unavailable", "设置不可用，无法保存档案复用告知开关")
		return
	}
	if r.Header.Get(HeaderMCP) == "1" {
		writeError(w, http.StatusForbidden, "settings_ui_only", "档案复用告知开关只能由本机界面修改")
		return
	}
	var in archiveReuseInput
	if !s.readAppJSON(w, r, &in) {
		return
	}
	if in.Notify == nil {
		writeError(w, http.StatusBadRequest, "invalid_settings", "缺少 notify")
		return
	}
	if err := s.opts.Settings.SetArchiveReuseNotify(*in.Notify); err != nil {
		s.internalError(w, "save archive reuse settings", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"notify": s.opts.Settings.ArchiveReuseNotify()})
}
