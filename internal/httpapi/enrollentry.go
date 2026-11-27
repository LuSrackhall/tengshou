package httpapi

import (
	"net/http"

	"github.com/skip2/go-qrcode"
)

// Long-lived self-enrollment entry printed on the wall poster (Q1, te-48hkl).
//
// The token lives in config.json and never expires; the landlord can rotate it
// (old posters stop working immediately) or disable it. Submissions through
// this token create ordinary self-enrolled archives and are not audited before
// creation, exactly like the one-time enrollment dialog.

func (s *Server) enrollEntryToken() (string, bool) {
	if s.opts.Settings == nil {
		return "", false
	}
	return s.opts.Settings.EnrollEntryToken()
}

// enrollEntryView builds the poster payload for the current token. The token
// is included because the poster prints a fallback URL next to the QR code.
func (s *Server) enrollEntryView(w http.ResponseWriter, token string, disabled bool) bool {
	prefixes, ok := s.lanPrefixes(w, "/enroll/", "手机无法打开自助建档页")
	if !ok {
		return false
	}
	urls := make([]string, 0, len(prefixes))
	for _, prefix := range prefixes {
		urls = append(urls, prefix+token)
	}
	item := map[string]any{
		"issued":  token != "",
		"enabled": token != "" && !disabled,
		"token":   token,
		"urls":    urls,
		"qr_url":  "/app/api/v1/enroll-entry/qr.png",
	}
	if len(urls) > 0 {
		item["url"] = urls[0]
	}
	writeJSON(w, http.StatusOK, map[string]any{"item": item})
	return true
}

func (s *Server) handleAppEnrollEntry(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.appService(w); !ok {
		return
	}
	if s.opts.Settings == nil {
		writeError(w, http.StatusServiceUnavailable, "settings_unavailable", "设置不可用，无法读取自助录入入口")
		return
	}
	token, disabled := s.opts.Settings.EnrollEntryToken()
	if token == "" {
		writeJSON(w, http.StatusOK, map[string]any{"item": map[string]any{
			"issued":  false,
			"enabled": false,
			"urls":    []string{},
			"qr_url":  "/app/api/v1/enroll-entry/qr.png",
		}})
		return
	}
	s.enrollEntryView(w, token, disabled)
}

func (s *Server) handleAppRotateEnrollEntry(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.appService(w); !ok {
		return
	}
	if s.opts.Settings == nil {
		writeError(w, http.StatusServiceUnavailable, "settings_unavailable", "设置不可用，无法生成自助录入入口")
		return
	}
	token, err := s.opts.Settings.RotateEnrollEntry()
	if err != nil {
		s.internalError(w, "rotate enroll entry", err)
		return
	}
	s.enrollEntryView(w, token, false)
}

func (s *Server) handleAppDisableEnrollEntry(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.appService(w); !ok {
		return
	}
	if s.opts.Settings == nil {
		writeError(w, http.StatusServiceUnavailable, "settings_unavailable", "设置不可用，无法停用自助录入入口")
		return
	}
	if err := s.opts.Settings.DisableEnrollEntry(); err != nil {
		s.internalError(w, "disable enroll entry", err)
		return
	}
	token, disabled := s.opts.Settings.EnrollEntryToken()
	s.enrollEntryView(w, token, disabled)
}

func (s *Server) handleAppEnrollEntryQR(w http.ResponseWriter, r *http.Request) {
	token, disabled := s.enrollEntryToken()
	if token == "" || disabled {
		http.NotFound(w, r)
		return
	}
	prefixes, ok := s.lanPrefixes(w, "/enroll/", "手机无法打开自助建档页")
	if !ok {
		return
	}
	idx := intParam(r, "index", 0)
	if idx < 0 || idx >= len(prefixes) {
		idx = 0
	}
	png, err := qrcode.Encode(prefixes[idx]+token, qrcode.Medium, 512)
	if err != nil {
		s.internalError(w, "encode enroll entry qr", err)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(png)
}
