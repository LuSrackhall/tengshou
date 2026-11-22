package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/LuSrackhall/tengshou/internal/model"
)

// SignatureInput is a normalized handwritten signature ready to be retained.
// ImagePNG must already be black-ink-on-transparent (see internal/sign).
type SignatureInput struct {
	PartyID       string
	ContractID    string
	SignerName    string
	ContentSHA256 string
	Source        string
	SessionID     string
	ImagePNG      []byte
	SHA256        string
	WidthPx       int
	HeightPx      int
	CapturedAt    time.Time
}

// SaveSignature writes the PNG under assets/signatures, records the metadata
// row linked to party/contract/content version, and appends an audit entry.
// A failed database write removes the file so no orphan asset is left behind.
func (s *Service) SaveSignature(ctx context.Context, in SignatureInput) (model.Signature, error) {
	if len(in.ImagePNG) == 0 || strings.TrimSpace(in.SHA256) == "" {
		return model.Signature{}, business("invalid_image", "签名图片为空")
	}
	sum := sha256.Sum256(in.ImagePNG)
	if !strings.EqualFold(hex.EncodeToString(sum[:]), strings.TrimSpace(in.SHA256)) {
		return model.Signature{}, business("invalid_image", "签名图片摘要不匹配")
	}
	root := strings.TrimSpace(s.dataDir)
	if root == "" {
		return model.Signature{}, business("not_configured", "签名存储目录未配置")
	}
	in.SignerName = truncateRunes(strings.TrimSpace(in.SignerName), 120)
	if in.SignerName == "" {
		return model.Signature{}, business("invalid_signer", "签名人姓名不能为空")
	}
	var contract *model.Contract
	if id := strings.TrimSpace(in.ContractID); id != "" {
		c, err := s.rd.GetContract(ctx, id)
		if err != nil {
			if isNotFound(err) {
				return model.Signature{}, business("not_found", "合同不存在，无法关联签名")
			}
			return model.Signature{}, err
		}
		contract = &c
		in.ContractID = c.ID
	}
	if id := strings.TrimSpace(in.PartyID); id != "" {
		if _, err := s.rd.GetParty(ctx, id); err != nil {
			if isNotFound(err) {
				return model.Signature{}, business("not_found", "签名人档案不存在")
			}
			return model.Signature{}, err
		}
		in.PartyID = id
	}

	id, err := model.NewID("sig")
	if err != nil {
		return model.Signature{}, err
	}
	rel := path.Join("assets", "signatures", id+".png")
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		return model.Signature{}, fmt.Errorf("create signature dir: %w", err)
	}
	if err := writeFileAtomic(full, in.ImagePNG, 0o600); err != nil {
		return model.Signature{}, fmt.Errorf("write signature png: %w", err)
	}
	now := s.now()
	if in.CapturedAt.IsZero() {
		in.CapturedAt = now
	}
	sig := model.Signature{
		ID:            id,
		PartyID:       in.PartyID,
		ContractID:    in.ContractID,
		SignerName:    in.SignerName,
		AssetPath:     filepath.ToSlash(rel),
		SHA256:        in.SHA256,
		ContentSHA256: strings.TrimSpace(in.ContentSHA256),
		Source:        strings.TrimSpace(in.Source),
		SessionID:     strings.TrimSpace(in.SessionID),
		WidthPx:       in.WidthPx,
		HeightPx:      in.HeightPx,
		CapturedAt:    in.CapturedAt.UTC(),
		CreatedAt:     now,
	}
	if err := s.st.PutSignature(ctx, &sig); err != nil {
		_ = os.Remove(full)
		return model.Signature{}, err
	}
	summary := fmt.Sprintf("采集手写签名：%s", sig.SignerName)
	entity, entityID := "", ""
	if contract != nil {
		entity, entityID = model.ChangeEntityContract, contract.ID
		summary = fmt.Sprintf("采集手写签名：%s（合同 %s）", sig.SignerName, contract.ContractNo)
		if sig.ContentSHA256 != "" && len(sig.ContentSHA256) >= 12 {
			summary += "，内容版本 " + sig.ContentSHA256[:12]
		}
	} else if sig.PartyID != "" {
		entity, entityID = model.ChangeEntityParty, sig.PartyID
	}
	if entity != "" {
		s.logChange(ctx, entity, entityID, model.ChangeActionUpdate, summary, nil)
	}
	return sig, nil
}

// ExportSignature reads one retained signature image after verifying its
// SHA-256 against the database, then records the export in the change log.
// Signature images live under the private data directory and are never served
// as static files; this method is the only way out (B9).
func (s *Service) ExportSignature(ctx context.Context, id string) ([]byte, string, error) {
	sig, err := s.rd.GetSignature(ctx, strings.TrimSpace(id))
	if err != nil {
		if isNotFound(err) {
			return nil, "", business("not_found", "签名不存在")
		}
		return nil, "", err
	}
	full, err := s.assetFile(sig.AssetPath)
	if err != nil {
		return nil, "", err
	}
	raw, err := os.ReadFile(full)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, "", business("missing_asset", "签名文件不在数据目录中，无法导出")
		}
		return nil, "", err
	}
	sum := sha256.Sum256(raw)
	if !strings.EqualFold(hex.EncodeToString(sum[:]), strings.TrimSpace(sig.SHA256)) {
		return nil, "", business("checksum_mismatch", "签名文件校验失败（摘要与记录不符），已拒绝导出")
	}
	summary := fmt.Sprintf("导出签名图片「%s」（sha256 %s…）", sig.SignerName, shortSHA(sig.SHA256))
	if sig.ContractID != "" {
		if contract, err := s.rd.GetContract(ctx, sig.ContractID); err == nil {
			summary += "，合同 " + contract.ContractNo
		}
	}
	s.logChange(ctx, model.ChangeEntitySignature, sig.ID, model.ChangeActionExport, summary, nil)
	return raw, sig.ID + ".png", nil
}

// ContentSHA256 returns the content version digest of a contract: SHA-256 over
// the canonical JSON of its stable fields and dynamic data. Timestamps are
// excluded so the digest changes only when the contract content changes.
func ContentSHA256(c model.Contract) (string, error) {
	payload := struct {
		ID         string         `json:"id"`
		ContractNo string         `json:"contract_no"`
		Year       int            `json:"year"`
		Title      string         `json:"title"`
		Type       string         `json:"type"`
		PartyAID   string         `json:"party_a_id"`
		PartyBID   string         `json:"party_b_id"`
		Status     string         `json:"status"`
		SignedOn   string         `json:"signed_on"`
		ValidFrom  string         `json:"valid_from"`
		ValidTo    string         `json:"valid_to"`
		Amount     float64        `json:"amount"`
		Data       map[string]any `json:"data"`
	}{
		ID:         c.ID,
		ContractNo: c.ContractNo,
		Year:       c.Year,
		Title:      c.Title,
		Type:       c.Type,
		PartyAID:   c.PartyAID,
		PartyBID:   c.PartyBID,
		Status:     c.Status,
		SignedOn:   c.SignedOn,
		ValidFrom:  c.ValidFrom,
		ValidTo:    c.ValidTo,
		Amount:     c.Amount,
		Data:       nonNilData(c.Data),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode contract content: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func nonNilData(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

// writeFileAtomic writes to a temporary file in the same directory and
// renames it into place, so readers never observe a partial image.
func writeFileAtomic(full string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(full), "."+filepath.Base(full)+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, full)
}
