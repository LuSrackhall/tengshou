package httpapi

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/LuSrackhall/tengshou/internal/service"
	"github.com/LuSrackhall/tengshou/internal/sign"
	"github.com/LuSrackhall/tengshou/internal/store"
)

const (
	// maxPartyPhotoBody bounds one base64-encoded ID-card photo upload.
	maxPartyPhotoBody = 24 << 20
	// maxEnrollBody bounds one self-enrollment submission (two photos plus a
	// signature, all base64-encoded).
	maxEnrollBody = 48 << 20
)

type partyPhotoInput struct {
	Image string `json:"image"`
}

// handleAppUploadPartyIDPhoto stores or replaces one side of an archive's
// ID-card photos (front/back). The image is compressed to JPEG before storage.
func (s *Server) handleAppUploadPartyIDPhoto(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.appService(w)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxPartyPhotoBody)
	var in partyPhotoInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "请求体不是有效的 JSON")
		return
	}
	raw, err := decodeImageDataURL(in.Image)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_image", err.Error())
		return
	}
	party, err := svc.SavePartyIDPhoto(r.Context(), r.PathValue("id"), r.PathValue("side"), raw)
	if err != nil {
		s.writeServiceError(w, "save party id photo", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"item": party})
}

// handleAppPartyIDPhoto serves a stored ID-card photo back to the loopback app
// surface; the digest is verified before the bytes leave the process.
func (s *Server) handleAppPartyIDPhoto(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.appService(w)
	if !ok {
		return
	}
	raw, err := svc.PartyIDPhoto(r.Context(), r.PathValue("id"), r.PathValue("side"))
	if err != nil {
		s.writeServiceError(w, "read party id photo", err)
		return
	}
	serveImage(w, "image/jpeg", raw)
}

// handleAppPartySignatureImage serves the archive-level signature PNG.
func (s *Server) handleAppPartySignatureImage(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.appService(w)
	if !ok {
		return
	}
	raw, err := svc.PartySignatureImage(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeServiceError(w, "read party signature", err)
		return
	}
	serveImage(w, "image/png", raw)
}

func serveImage(w http.ResponseWriter, contentType string, raw []byte) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Length", strconv.Itoa(len(raw)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}

// handleAppCreatePartySignatureSession starts an archive-level signature
// capture: the signature is kept with the party archive and can be stamped
// into later contracts directly (v0.2 D3=B).
func (s *Server) handleAppCreatePartySignatureSession(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.appService(w); !ok {
		return
	}
	prefixes, ok := s.lanPrefixes(w, "/sign/", "手机无法打开签名页")
	if !ok {
		return
	}
	party, err := s.opts.Reader.GetParty(r.Context(), r.PathValue("id"))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "相对方档案不存在")
			return
		}
		s.internalError(w, "read party", err)
		return
	}
	sess, _, err := s.sign.Create(sign.CreateInput{
		Kind:        sign.KindSignature,
		PartyID:     party.ID,
		SignerName:  party.Name,
		URLPrefixes: prefixes,
	})
	if err != nil {
		s.internalError(w, "create archive signature session", err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"item": s.signSessionView(sess)})
}

// handleAppCreateEnrollSession starts a LAN self-enrollment session. The QR
// code is shown on the landlord side; the tenant fills the archive on a phone.
func (s *Server) handleAppCreateEnrollSession(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.appService(w); !ok {
		return
	}
	prefixes, ok := s.lanPrefixes(w, "/enroll/", "手机无法打开自助建档页")
	if !ok {
		return
	}
	sess, _, err := s.sign.Create(sign.CreateInput{
		Kind:        sign.KindEnroll,
		URLPrefixes: prefixes,
	})
	if err != nil {
		s.internalError(w, "create enroll session", err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"item": enrollSessionView(sess)})
}

// handleAppGetEnrollSession returns the current enrollment session state.
func (s *Server) handleAppGetEnrollSession(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.appService(w); !ok {
		return
	}
	sess, err := s.sign.GetByID(r.PathValue("id"))
	if err != nil || sess.Kind != sign.KindEnroll {
		writeError(w, http.StatusNotFound, "not_found", "自助建档会话不存在或已失效")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"item": enrollSessionView(sess)})
}

// handleAppCancelEnrollSession cancels an unfinished enrollment session.
func (s *Server) handleAppCancelEnrollSession(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.appService(w); !ok {
		return
	}
	cancelled := s.sign.CancelKind(r.PathValue("id"), sign.KindEnroll)
	writeJSON(w, http.StatusOK, map[string]any{"cancelled": cancelled})
}

func enrollSessionView(sess sign.Session) map[string]any {
	item := map[string]any{
		"id":         sess.ID,
		"kind":       sess.Kind,
		"state":      sess.State,
		"urls":       sess.URLs,
		"qr_url":     "/app/api/v1/enroll-sessions/" + sess.ID + "/qr.png",
		"created_at": sess.CreatedAt.Format(time.RFC3339),
		"expires_at": sess.ExpiresAt.Format(time.RFC3339),
	}
	if len(sess.URLs) > 0 {
		item["url"] = sess.URLs[0]
	}
	if sess.State == sign.StateCompleted {
		item["completed_at"] = sess.CompletedAt.Format(time.RFC3339)
	}
	return item
}

// lanPrefixes validates LAN availability and builds the phone-page URL
// prefixes. It writes the error response itself when unavailable.
func (s *Server) lanPrefixes(w http.ResponseWriter, pathPrefix, purpose string) ([]string, bool) {
	if !s.opts.Config.HTTP.AllowLAN {
		writeError(w, http.StatusConflict, "lan_required",
			"当前为仅本机模式，"+purpose+"；请以局域网模式重启腾手后再试")
		return nil, false
	}
	hosts := s.lanHosts()
	if len(hosts) == 0 {
		writeError(w, http.StatusConflict, "lan_unavailable",
			"未发现局域网 IPv4 地址，请确认电脑已连接 Wi-Fi 或网线后再试")
		return nil, false
	}
	port := s.opts.Config.HTTP.Port
	prefixes := make([]string, 0, len(hosts))
	for _, host := range hosts {
		prefixes = append(prefixes, "http://"+net.JoinHostPort(host, strconv.Itoa(port))+pathPrefix)
	}
	return prefixes, true
}

type enrollPageData struct {
	State     string
	Active    bool
	Completed bool
	Expired   bool
	ExpiresAt string
}

// handleEnrollPage renders the LAN self-enrollment page for the phone.
func (s *Server) handleEnrollPage(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	data := enrollPageData{State: sign.StateExpired, Expired: true}
	if sign.ValidToken(token) {
		if sess, err := s.sign.Get(token); err == nil && sess.Kind == sign.KindEnroll {
			data.ExpiresAt = sess.ExpiresAt.Format(time.RFC3339)
			switch sess.State {
			case sign.StateCompleted:
				data.State = sign.StateCompleted
				data.Completed = true
				data.Expired = false
			case sign.StateExpired:
				// Default: the page explains the link expired.
			default:
				data.State = sess.State
				data.Active = true
				data.Expired = false
			}
		}
	}
	status := http.StatusOK
	if data.Expired {
		status = http.StatusGone
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := enrollPageTemplate.ExecuteTemplate(w, "enroll.html", data); err != nil {
		if s.opts.Logger != nil {
			s.opts.Logger.Error("render enroll page", "error", err)
		}
	}
}

// handleEnrollStatus reports whether the phone page is still usable.
func (s *Server) handleEnrollStatus(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	if !sign.ValidToken(token) {
		writeError(w, http.StatusNotFound, "not_found", "自助建档链接无效")
		return
	}
	sess, err := s.sign.Get(token)
	if err != nil || sess.Kind != sign.KindEnroll {
		writeError(w, http.StatusNotFound, "not_found", "自助建档链接已失效，请在电脑上重新生成二维码")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"state":      sess.State,
		"expires_at": sess.ExpiresAt.Format(time.RFC3339),
	})
}

type enrollSubmitInput struct {
	Name      string `json:"name"`
	IDNumber  string `json:"id_number"`
	Phone     string `json:"phone"`
	Address   string `json:"address"`
	IDFront   string `json:"id_front"`
	IDBack    string `json:"id_back"`
	Signature string `json:"signature"`
}

// handleEnrollSubmit creates the archive from the phone submission. The
// one-time token is claimed before writing, so a double tap cannot create two
// archives. A duplicate ID number is rejected and the session stays usable so
// the tenant can correct a typo.
func (s *Server) handleEnrollSubmit(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	if !sign.ValidToken(token) {
		writeError(w, http.StatusGone, "enroll_expired", "自助建档链接已失效，请在电脑上重新生成二维码")
		return
	}
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(strings.ToLower(ct), "application/json") {
		writeError(w, http.StatusUnsupportedMediaType, "json_required", "请求体必须是 application/json")
		return
	}
	if r.Header.Get("X-Tengshou-Sign") != "1" {
		writeError(w, http.StatusForbidden, "enroll_header_required", "缺少提交请求头")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxEnrollBody)
	var in enrollSubmitInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "请求体不是有效的 JSON")
		return
	}
	front, err := decodeImageDataURL(in.IDFront)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_image", "证件照正面："+err.Error())
		return
	}
	back, err := decodeImageDataURL(in.IDBack)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_image", "证件照反面："+err.Error())
		return
	}
	rawSig, err := decodeImageDataURL(in.Signature)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_image", "手写签名："+err.Error())
		return
	}
	normalized, err := sign.NormalizePNG(rawSig)
	if err != nil {
		var imgErr *sign.ImageError
		if errors.As(err, &imgErr) {
			status := http.StatusUnprocessableEntity
			if imgErr.Code == "invalid_image" || imgErr.Code == "too_large" {
				status = http.StatusBadRequest
			}
			writeError(w, status, "sign_"+imgErr.Code, imgErr.Message)
			return
		}
		s.internalError(w, "normalize enrollment signature", err)
		return
	}
	_, err = s.sign.ClaimKind(token, sign.KindEnroll)
	switch {
	case err == nil:
	case errors.Is(err, sign.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "自助建档链接无效")
		return
	case errors.Is(err, sign.ErrExpired):
		writeError(w, http.StatusGone, "enroll_expired", "自助建档链接已过期，请在电脑上重新生成二维码")
		return
	case errors.Is(err, sign.ErrConsumed):
		writeError(w, http.StatusConflict, "enroll_consumed", "这份档案已经提交过了，请回到电脑上查看")
		return
	case errors.Is(err, sign.ErrBusy):
		writeError(w, http.StatusConflict, "enroll_busy", "正在提交，请稍候")
		return
	default:
		writeError(w, http.StatusGone, "enroll_expired", "自助建档链接已失效，请在电脑上重新生成二维码")
		return
	}
	svc, ok := s.appService(w)
	if !ok {
		s.sign.Release(token)
		return
	}
	party, err := svc.EnrollParty(r.Context(), service.EnrollPartyInput{
		Name:            in.Name,
		IDNumber:        in.IDNumber,
		Phone:           in.Phone,
		Address:         in.Address,
		IDFront:         front,
		IDBack:          back,
		SignaturePNG:    normalized.PNG,
		SignatureSHA256: normalized.SHA256,
		SignatureWidth:  normalized.Width,
		SignatureHeight: normalized.Height,
	})
	if err != nil {
		// Duplicate ID / validation problems stay recoverable: the tenant can
		// correct the form and submit the same one-time link again.
		s.sign.Release(token)
		s.writeServiceError(w, "enroll party", err)
		return
	}
	s.sign.Complete(token, party.SignatureID, "", 0, 0)
	if logger := s.opts.Logger; logger != nil {
		logger.Info("party self-enrolled", "party", party.ID, "id_number", service.MaskIDNumber(party.IDNumber))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"state": "completed",
		"party": map[string]any{
			"id":         party.ID,
			"name":       party.Name,
			"created_at": party.CreatedAt.Format(time.RFC3339),
		},
	})
}
