package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/skip2/go-qrcode"

	"github.com/LuSrackhall/tengshou/internal/model"
	"github.com/LuSrackhall/tengshou/internal/service"
	"github.com/LuSrackhall/tengshou/internal/sign"
	"github.com/LuSrackhall/tengshou/internal/store"
)

const maxSignBody = 8 << 20 // base64-encoded PNG from the phone canvas

type createSignSessionInput struct {
	ContractID string `json:"contract_id"`
	PartyID    string `json:"party_id"`
	SignerName string `json:"signer_name"`
}

func (s *Server) handleAppCreateSignatureSession(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.appService(w); !ok {
		return
	}
	if !s.opts.Config.HTTP.AllowLAN {
		writeError(w, http.StatusConflict, "lan_required",
			"当前为仅本机模式，手机无法打开签名页；请以局域网模式重启腾手后再试")
		return
	}
	hosts := s.lanHosts()
	if len(hosts) == 0 {
		writeError(w, http.StatusConflict, "lan_unavailable",
			"未发现局域网 IPv4 地址，请确认电脑已连接 Wi-Fi 或网线后再试")
		return
	}
	var in createSignSessionInput
	if !s.readAppJSON(w, r, &in) {
		return
	}
	contract, err := s.opts.Reader.GetContract(r.Context(), strings.TrimSpace(in.ContractID))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "合同不存在")
			return
		}
		s.internalError(w, "read contract", err)
		return
	}
	partyID := strings.TrimSpace(in.PartyID)
	switch {
	case partyID == "":
		partyID = contract.PartyBID
	case partyID == contract.PartyAID || partyID == contract.PartyBID:
	default:
		writeError(w, http.StatusUnprocessableEntity, "invalid_signer",
			"签名人必须是本合同甲方或乙方的档案")
		return
	}
	if partyID == "" {
		writeError(w, http.StatusUnprocessableEntity, "missing_party",
			"合同还没有相对方档案，无法确定签名人")
		return
	}
	party, err := s.opts.Reader.GetParty(r.Context(), partyID)
	if err != nil {
		s.internalError(w, "read signer party", err)
		return
	}
	signerName := strings.TrimSpace(in.SignerName)
	if signerName == "" {
		signerName = party.Name
	}
	digest, err := service.ContentSHA256(contract)
	if err != nil {
		s.internalError(w, "contract content digest", err)
		return
	}
	port := s.opts.Config.HTTP.Port
	prefixes := make([]string, 0, len(hosts))
	for _, host := range hosts {
		prefixes = append(prefixes, "http://"+net.JoinHostPort(host, strconv.Itoa(port))+"/sign/")
	}
	sess, _, err := s.sign.Create(sign.CreateInput{
		ContractID:    contract.ID,
		ContractNo:    contract.ContractNo,
		ContractTitle: contract.Title,
		ContentSHA256: digest,
		PartyID:       partyID,
		SignerName:    signerName,
		URLPrefixes:   prefixes,
	})
	if err != nil {
		s.internalError(w, "create signature session", err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"item": s.signSessionView(sess)})
}

func (s *Server) handleAppGetSignatureSession(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.appService(w); !ok {
		return
	}
	sess, err := s.sign.GetByID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "签名会话不存在或已失效")
		return
	}
	item := s.signSessionView(sess)
	// Surface content changes that happened after the QR was generated, so a
	// signature cannot silently be attributed to a newer version.
	if sess.ContractID != "" && s.opts.Reader != nil {
		if contract, err := s.opts.Reader.GetContract(r.Context(), sess.ContractID); err == nil {
			if digest, err := service.ContentSHA256(contract); err == nil {
				item["current_content_sha256"] = digest
				item["content_changed"] = digest != sess.ContentSHA256
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"item": item})
}

func (s *Server) handleAppCancelSignatureSession(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.appService(w); !ok {
		return
	}
	cancelled := s.sign.Cancel(r.PathValue("id"))
	writeJSON(w, http.StatusOK, map[string]any{"cancelled": cancelled})
}

func (s *Server) handleAppSignatureQR(w http.ResponseWriter, r *http.Request) {
	sess, err := s.sign.GetByID(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if len(sess.URLs) == 0 {
		http.NotFound(w, r)
		return
	}
	idx := intParam(r, "index", 0)
	if idx < 0 || idx >= len(sess.URLs) {
		idx = 0
	}
	png, err := qrcode.Encode(sess.URLs[idx], qrcode.Medium, 512)
	if err != nil {
		s.internalError(w, "encode qr", err)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Length", strconv.Itoa(len(png)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(png)
}

// signSessionView is the app-API shape of a session. The one-time token is
// deliberately absent: only the QR/URL carries it.
func (s *Server) signSessionView(sess sign.Session) map[string]any {
	item := map[string]any{
		"id":             sess.ID,
		"state":          sess.State,
		"contract_id":    sess.ContractID,
		"contract_no":    sess.ContractNo,
		"contract_title": sess.ContractTitle,
		"content_sha256": sess.ContentSHA256,
		"party_id":       sess.PartyID,
		"signer_name":    sess.SignerName,
		"urls":           sess.URLs,
		"qr_url":         "/app/api/v1/signature-sessions/" + sess.ID + "/qr.png",
		"created_at":     sess.CreatedAt.Format(time.RFC3339),
		"expires_at":     sess.ExpiresAt.Format(time.RFC3339),
	}
	if len(sess.URLs) > 0 {
		item["url"] = sess.URLs[0]
	}
	if sess.State == sign.StateCompleted {
		item["completed_at"] = sess.CompletedAt.Format(time.RFC3339)
		item["signature_id"] = sess.SignatureID
		item["asset_path"] = sess.SignaturePath
		item["width_px"] = sess.SignatureWidth
		item["height_px"] = sess.SignatureHeight
	}
	return item
}

type signPageData struct {
	State         string
	Active        bool
	Completed     bool
	Expired       bool
	SignerName    string
	ContractNo    string
	ContractTitle string
	ExpiresAt     string
}

func (s *Server) handleSignPage(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	data := signPageData{State: sign.StateExpired, Expired: true}
	if sign.ValidToken(token) {
		if sess, err := s.sign.Get(token); err == nil {
			data.SignerName = sess.SignerName
			data.ContractNo = sess.ContractNo
			data.ContractTitle = sess.ContractTitle
			data.ExpiresAt = sess.ExpiresAt.Format(time.RFC3339)
			switch sess.State {
			case sign.StateCompleted:
				data.State = sign.StateCompleted
				data.Completed = true
				data.Expired = false
			case sign.StateExpired:
				// Already the default: the page explains the link expired.
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
	if err := signPageTemplate.ExecuteTemplate(w, "sign.html", data); err != nil {
		if s.opts.Logger != nil {
			s.opts.Logger.Error("render sign page", "error", err)
		}
	}
}

func (s *Server) handleSignStatus(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	if !sign.ValidToken(token) {
		writeError(w, http.StatusNotFound, "not_found", "签名链接无效")
		return
	}
	sess, err := s.sign.Get(token)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "签名链接已失效，请在电脑上重新生成二维码")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"state":      sess.State,
		"expires_at": sess.ExpiresAt.Format(time.RFC3339),
	})
}

type signSubmitInput struct {
	Image string `json:"image"`
}

func (s *Server) handleSignSubmit(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	if !sign.ValidToken(token) {
		writeError(w, http.StatusGone, "sign_expired", "签名链接已失效，请在电脑上重新生成二维码")
		return
	}
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(strings.ToLower(ct), "application/json") {
		writeError(w, http.StatusUnsupportedMediaType, "json_required", "请求体必须是 application/json")
		return
	}
	// A custom header forces a CORS preflight for foreign pages; this server
	// sends no CORS headers, so only the same-origin signing page can submit.
	if r.Header.Get("X-Tengshou-Sign") != "1" {
		writeError(w, http.StatusForbidden, "sign_header_required", "缺少签名提交请求头")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxSignBody)
	var in signSubmitInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "请求体不是有效的 JSON")
		return
	}
	raw, err := decodeSignatureImage(in.Image)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_image", err.Error())
		return
	}
	normalized, err := sign.NormalizePNG(raw)
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
		s.internalError(w, "normalize signature", err)
		return
	}
	sess, err := s.sign.Claim(token)
	switch {
	case err == nil:
	case errors.Is(err, sign.ErrExpired):
		writeError(w, http.StatusGone, "sign_expired", "签名链接已过期，请在电脑上重新生成二维码")
		return
	case errors.Is(err, sign.ErrConsumed):
		writeError(w, http.StatusConflict, "sign_consumed", "这份签名已经提交过了，请回到电脑上查看")
		return
	case errors.Is(err, sign.ErrBusy):
		writeError(w, http.StatusConflict, "sign_busy", "签名正在提交，请稍候")
		return
	default:
		writeError(w, http.StatusGone, "sign_expired", "签名链接已失效，请在电脑上重新生成二维码")
		return
	}
	svc, ok := s.appService(w)
	if !ok {
		s.sign.Release(token)
		return
	}
	sig, err := svc.SaveSignature(r.Context(), service.SignatureInput{
		PartyID:       sess.PartyID,
		ContractID:    sess.ContractID,
		SignerName:    sess.SignerName,
		ContentSHA256: sess.ContentSHA256,
		Source:        model.SignatureSourceLANQR,
		SessionID:     sess.ID,
		ImagePNG:      normalized.PNG,
		SHA256:        normalized.SHA256,
		WidthPx:       normalized.Width,
		HeightPx:      normalized.Height,
	})
	if err != nil {
		s.sign.Release(token)
		s.writeServiceError(w, "save signature", err)
		return
	}
	s.sign.Complete(token, sig.ID, sig.AssetPath, sig.WidthPx, sig.HeightPx)
	if logger := s.opts.Logger; logger != nil {
		logger.Info("signature captured", "contract", sig.ContractID, "signer", sig.SignerName,
			"asset", sig.AssetPath, "sha256", sig.SHA256)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"state": "completed",
		"signature": map[string]any{
			"id":          sig.ID,
			"signer_name": sig.SignerName,
			"captured_at": sig.CapturedAt.Format(time.RFC3339),
		},
	})
}

func decodeSignatureImage(encoded string) ([]byte, error) {
	encoded = strings.TrimSpace(encoded)
	if encoded == "" {
		return nil, errors.New("缺少签名图片")
	}
	if idx := strings.Index(encoded, ","); idx >= 0 && strings.HasPrefix(encoded, "data:") {
		encoded = encoded[idx+1:]
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, errors.New("签名图片不是有效的 base64")
	}
	if len(raw) == 0 {
		return nil, errors.New("签名图片为空")
	}
	return raw, nil
}

func (s *Server) lanHosts() []string {
	if s.opts.LANHosts != nil {
		return s.opts.LANHosts
	}
	return localIPv4s()
}
