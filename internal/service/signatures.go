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
	summary := fmt.Sprintf("采集手写签名：%s（来源：现场手写）", sig.SignerName)
	entity, entityID := "", ""
	if contract != nil {
		entity, entityID = model.ChangeEntityContract, contract.ID
		summary = fmt.Sprintf("采集手写签名：%s（合同 %s，来源：现场手写）", sig.SignerName, contract.ContractNo)
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

// ReusePartySignature links a party's archive-level signature to a contract.
// v0.2 D3=B treats the archive signature as directly reusable: the row points
// at the same immutable asset and deliberately carries no contract content
// version, so editing the contract later does not invalidate it. Replacing the
// archive signature afterwards only affects future reuse; rows already linked
// to contracts and already generated PDFs keep their original asset.
func (s *Service) ReusePartySignature(ctx context.Context, contractID, partyID string) (model.Signature, error) {
	contractID = strings.TrimSpace(contractID)
	partyID = strings.TrimSpace(partyID)
	contract, err := s.rd.GetContract(ctx, contractID)
	if err != nil {
		if isNotFound(err) {
			return model.Signature{}, business("not_found", "合同不存在")
		}
		return model.Signature{}, err
	}
	if partyID == "" || (partyID != contract.PartyAID && partyID != contract.PartyBID) {
		return model.Signature{}, business("invalid_party", "只能复用本合同甲方或乙方档案的签名")
	}
	party, err := s.rd.GetParty(ctx, partyID)
	if err != nil {
		if isNotFound(err) {
			return model.Signature{}, business("not_found", "相对方档案不存在")
		}
		return model.Signature{}, err
	}
	if party.SignatureID == "" {
		return model.Signature{}, business("missing_signature", "该档案还没有采集签名，请先扫码采集")
	}
	archive, err := s.rd.GetSignature(ctx, party.SignatureID)
	if err != nil {
		if isNotFound(err) {
			return model.Signature{}, business("missing_signature", "档案签名记录不存在，请重新采集")
		}
		return model.Signature{}, err
	}
	if archive.PartyID != "" && archive.PartyID != party.ID {
		return model.Signature{}, business("invalid_signature", "档案签名记录不属于该相对方，请重新采集")
	}
	full, err := s.assetFile(archive.AssetPath)
	if err != nil {
		return model.Signature{}, err
	}
	if _, err := os.Stat(full); err != nil {
		return model.Signature{}, business("missing_asset", "档案签名图片文件缺失，请重新采集")
	}
	// Idempotent per (contract, party, asset): repeated clicks, including after
	// the contract content changed, return the existing row instead of piling
	// up duplicates, because archive reuse is not bound to a content version.
	existing, err := s.rd.ListSignatures(ctx, model.ListFilter{ContractID: contract.ID, Limit: 500})
	if err != nil {
		return model.Signature{}, err
	}
	for _, sig := range existing.Items {
		if sig.PartyID == party.ID && sig.Source == model.SignatureSourceArchive &&
			sig.AssetPath == archive.AssetPath {
			return sig, nil
		}
	}
	id, err := model.NewID("sig")
	if err != nil {
		return model.Signature{}, err
	}
	now := s.now()
	sig := model.Signature{
		ID:         id,
		PartyID:    party.ID,
		ContractID: contract.ID,
		SignerName: party.Name,
		AssetPath:  archive.AssetPath,
		SHA256:     archive.SHA256,
		// No ContentSHA256 on purpose: archive reuse applies as-is (直接套印)
		// and is not tied to one contract content version (v0.2 D3=B).
		Source:     model.SignatureSourceArchive,
		WidthPx:    archive.WidthPx,
		HeightPx:   archive.HeightPx,
		CapturedAt: now,
		CreatedAt:  now,
	}
	if err := s.st.PutSignature(ctx, &sig); err != nil {
		return model.Signature{}, err
	}
	s.logChange(ctx, model.ChangeEntityContract, contract.ID, model.ChangeActionUpdate,
		fmt.Sprintf("复用档案签名：%s（合同 %s，来源：档案复用，直接套印）",
			party.Name, contract.ContractNo), nil)
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
		ID         string `json:"id"`
		ContractNo string `json:"contract_no"`
		Year       int    `json:"year"`
		Title      string `json:"title"`
		Type       string `json:"type"`
		PartyAID   string `json:"party_a_id"`
		PartyBID   string `json:"party_b_id"`
		Status     string `json:"status"`
		SignedOn   string `json:"signed_on"`
		ValidFrom  string `json:"valid_from"`
		ValidTo    string `json:"valid_to"`
		// Lease/asset fields use omitempty so historical contracts (all empty)
		// keep their pre-v0.2 digest and retained signatures stay valid.
		LeaseStart string         `json:"lease_start,omitempty"`
		LeaseEnd   string         `json:"lease_end,omitempty"`
		AssetID    string         `json:"asset_id,omitempty"`
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
		LeaseStart: c.LeaseStart,
		LeaseEnd:   c.LeaseEnd,
		AssetID:    c.AssetID,
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
