package service

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/LuSrackhall/tengshou/internal/model"
	"github.com/LuSrackhall/tengshou/internal/seal"
)

// SealInput is the editable metadata for an imported seal picture. Image is
// only used on import and replace; ImagePNG/SHA256 are filled by Normalize.
type SealInput struct {
	ID      string `json:"id"`
	PartyID string `json:"party_id"`
	Name    string `json:"name"`
	Note    string `json:"note"`
	Image   []byte `json:"-"`
}

// ImportSeal normalizes an uploaded PNG/JPG, writes it under assets/seals and
// records the metadata row. A failed database write removes the file so no
// orphan asset is left behind.
func (s *Service) ImportSeal(ctx context.Context, in SealInput) (model.Seal, error) {
	root := strings.TrimSpace(s.dataDir)
	if root == "" {
		return model.Seal{}, business("not_configured", "公章存储目录未配置")
	}
	name := truncateRunes(strings.TrimSpace(in.Name), 120)
	if name == "" {
		return model.Seal{}, business("invalid_name", "公章名称不能为空")
	}
	partyID := strings.TrimSpace(in.PartyID)
	if partyID != "" {
		party, err := s.rd.GetParty(ctx, partyID)
		if err != nil {
			if isNotFound(err) {
				return model.Seal{}, business("not_found", "所属单位档案不存在")
			}
			return model.Seal{}, err
		}
		partyID = party.ID
	}
	normalized, err := seal.Normalize(in.Image)
	if err != nil {
		var imgErr *seal.ImageError
		if ok := asImageError(err, &imgErr); ok {
			return model.Seal{}, business(imgErr.Code, "%s", imgErr.Message)
		}
		return model.Seal{}, err
	}
	id, err := model.NewID("seal")
	if err != nil {
		return model.Seal{}, err
	}
	rel := sealRelPath(id, normalized.SHA256)
	full, err := s.assetFile(rel)
	if err != nil {
		return model.Seal{}, err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		return model.Seal{}, fmt.Errorf("create seal dir: %w", err)
	}
	if err := writeFileAtomic(full, normalized.PNG, 0o600); err != nil {
		return model.Seal{}, fmt.Errorf("write seal png: %w", err)
	}
	now := s.now()
	record := model.Seal{
		ID:        id,
		PartyID:   partyID,
		Name:      name,
		AssetPath: filepath.ToSlash(rel),
		SHA256:    normalized.SHA256,
		MIME:      "image/png",
		WidthPx:   normalized.Width,
		HeightPx:  normalized.Height,
		Note:      truncateRunes(strings.TrimSpace(in.Note), 500),
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.st.PutSeal(ctx, &record); err != nil {
		if rmErr := os.Remove(full); rmErr != nil && !os.IsNotExist(rmErr) {
			slog.Warn("回滚公章图片文件失败", "path", full, "error", rmErr)
		}
		return model.Seal{}, err
	}
	s.logChange(ctx, model.ChangeEntitySeal, record.ID, model.ChangeActionCreate,
		sealSummary("导入公章图片", record, normalized.MadeTransparent), nil)
	return record, nil
}

// UpdateSeal changes the descriptive metadata of a seal; the picture itself is
// replaced through ReplaceSealImage.
func (s *Service) UpdateSeal(ctx context.Context, id string, in SealInput) (model.Seal, error) {
	current, err := s.rd.GetSeal(ctx, strings.TrimSpace(id))
	if err != nil {
		if isNotFound(err) {
			return model.Seal{}, business("not_found", "公章图片不存在")
		}
		return model.Seal{}, err
	}
	next := current
	if name := truncateRunes(strings.TrimSpace(in.Name), 120); name != "" {
		next.Name = name
	}
	if partyID := strings.TrimSpace(in.PartyID); partyID != "" {
		if _, err := s.rd.GetParty(ctx, partyID); err != nil {
			if isNotFound(err) {
				return model.Seal{}, business("not_found", "所属单位档案不存在")
			}
			return model.Seal{}, err
		}
		next.PartyID = partyID
	}
	next.Note = truncateRunes(strings.TrimSpace(in.Note), 500)
	changes := []model.FieldChange{}
	if next.Name != current.Name {
		changes = append(changes, model.FieldChange{Field: "name", Label: "名称", Old: current.Name, New: next.Name})
	}
	if next.PartyID != current.PartyID {
		changes = append(changes, model.FieldChange{Field: "party_id", Label: "所属单位",
			Old: s.partyName(ctx, current.PartyID), New: s.partyName(ctx, next.PartyID)})
	}
	if next.Note != current.Note {
		changes = append(changes, model.FieldChange{Field: "note", Label: "说明", Old: current.Note, New: next.Note})
	}
	if len(changes) == 0 {
		return current, nil
	}
	if err := s.st.PutSeal(ctx, &next); err != nil {
		return model.Seal{}, err
	}
	s.logChange(ctx, model.ChangeEntitySeal, next.ID, model.ChangeActionUpdate,
		fmt.Sprintf("修改公章图片信息「%s」", next.Name), changes)
	return next, nil
}

// ReplaceSealImage swaps the picture of an existing seal. The new PNG gets a
// digest-scoped file name so no reader can observe a partially written file;
// the old file is removed after the database commit.
func (s *Service) ReplaceSealImage(ctx context.Context, id string, image []byte) (model.Seal, error) {
	current, err := s.rd.GetSeal(ctx, strings.TrimSpace(id))
	if err != nil {
		if isNotFound(err) {
			return model.Seal{}, business("not_found", "公章图片不存在")
		}
		return model.Seal{}, err
	}
	if strings.TrimSpace(s.dataDir) == "" {
		return model.Seal{}, business("not_configured", "公章存储目录未配置")
	}
	normalized, err := seal.Normalize(image)
	if err != nil {
		var imgErr *seal.ImageError
		if ok := asImageError(err, &imgErr); ok {
			return model.Seal{}, business(imgErr.Code, "%s", imgErr.Message)
		}
		return model.Seal{}, err
	}
	newRel := sealRelPath(current.ID, normalized.SHA256)
	if filepath.ToSlash(newRel) == filepath.ToSlash(current.AssetPath) {
		// Same picture: metadata is unchanged, report success without a write.
		return current, nil
	}
	full, err := s.assetFile(newRel)
	if err != nil {
		return model.Seal{}, err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		return model.Seal{}, fmt.Errorf("create seal dir: %w", err)
	}
	if err := writeFileAtomic(full, normalized.PNG, 0o600); err != nil {
		return model.Seal{}, fmt.Errorf("write seal png: %w", err)
	}
	next := current
	next.AssetPath = filepath.ToSlash(newRel)
	next.SHA256 = normalized.SHA256
	next.MIME = "image/png"
	next.WidthPx = normalized.Width
	next.HeightPx = normalized.Height
	if err := s.st.PutSeal(ctx, &next); err != nil {
		if rmErr := os.Remove(full); rmErr != nil && !os.IsNotExist(rmErr) {
			slog.Warn("回滚公章图片文件失败", "path", full, "error", rmErr)
		}
		return model.Seal{}, err
	}
	s.removeAsset(current.AssetPath)
	oldPrefix := shortSHA(current.SHA256)
	s.logChange(ctx, model.ChangeEntitySeal, next.ID, model.ChangeActionUpdate,
		fmt.Sprintf("替换公章图片「%s」（sha256 %s → %s，%dx%d）",
			next.Name, oldPrefix, shortSHA(next.SHA256), next.WidthPx, next.HeightPx),
		[]model.FieldChange{{Field: "sha256", Label: "图片摘要", Old: current.SHA256, New: next.SHA256}})
	return next, nil
}

// DeleteSeal removes a seal, its contract links and the asset file. The audit
// entry records how many contracts lost the association.
func (s *Service) DeleteSeal(ctx context.Context, id string) error {
	current, err := s.rd.GetSeal(ctx, strings.TrimSpace(id))
	if err != nil {
		if isNotFound(err) {
			return business("not_found", "公章图片不存在")
		}
		return err
	}
	linked, err := s.rd.SealContractIDs(ctx, current.ID)
	if err != nil {
		return err
	}
	if err := s.st.DeleteSeal(ctx, current.ID); err != nil {
		if isNotFound(err) {
			return business("not_found", "公章图片不存在")
		}
		return err
	}
	s.removeAsset(current.AssetPath)
	summary := fmt.Sprintf("删除公章图片「%s」（sha256 %s…）", current.Name, shortSHA(current.SHA256))
	if len(linked) > 0 {
		summary += fmt.Sprintf("，同时解除 %d 份合同的关联", len(linked))
	}
	s.logChange(ctx, model.ChangeEntitySeal, current.ID, model.ChangeActionDelete, summary, nil)
	return nil
}

// AttachSealToContracts links one seal to one or many contracts. This is the
// batch placement path: a single picture can be reused by every selected
// contract.
func (s *Service) AttachSealToContracts(ctx context.Context, sealID string, contractIDs []string) (int, error) {
	current, err := s.rd.GetSeal(ctx, strings.TrimSpace(sealID))
	if err != nil {
		if isNotFound(err) {
			return 0, business("not_found", "公章图片不存在")
		}
		return 0, err
	}
	ids := uniqueIDs(contractIDs)
	if len(ids) == 0 {
		return 0, business("invalid_contracts", "请选择至少一份合同")
	}
	for _, id := range ids {
		if _, err := s.rd.GetContract(ctx, id); err != nil {
			if isNotFound(err) {
				return 0, business("not_found", "合同不存在：%s", id)
			}
			return 0, err
		}
	}
	attached, err := s.st.AttachSeal(ctx, current.ID, ids, s.now())
	if err != nil {
		return 0, err
	}
	summary := fmt.Sprintf("关联公章图片「%s」（sha256 %s…）", current.Name, shortSHA(current.SHA256))
	for _, id := range ids {
		s.logChange(ctx, model.ChangeEntityContract, id, model.ChangeActionUpdate, summary, nil)
	}
	return attached, nil
}

// DetachSealFromContracts removes seal links from contracts.
func (s *Service) DetachSealFromContracts(ctx context.Context, sealID string, contractIDs []string) (int, error) {
	current, err := s.rd.GetSeal(ctx, strings.TrimSpace(sealID))
	if err != nil {
		if isNotFound(err) {
			return 0, business("not_found", "公章图片不存在")
		}
		return 0, err
	}
	ids := uniqueIDs(contractIDs)
	if len(ids) == 0 {
		return 0, business("invalid_contracts", "请选择至少一份合同")
	}
	linked, err := s.rd.SealContractIDs(ctx, current.ID)
	if err != nil {
		return 0, err
	}
	isLinked := map[string]bool{}
	for _, id := range linked {
		isLinked[id] = true
	}
	removed, err := s.st.DetachSeal(ctx, current.ID, ids)
	if err != nil {
		return 0, err
	}
	for _, id := range ids {
		if !isLinked[id] {
			continue
		}
		s.logChange(ctx, model.ChangeEntityContract, id, model.ChangeActionUpdate,
			fmt.Sprintf("解除公章图片「%s」的关联", current.Name), nil)
	}
	return removed, nil
}

// SealsForContract returns the seals applicable to a contract: explicitly
// linked seals first, then seals that belong to either party. Templates receive
// the combined list through TemplateData.
func (s *Service) SealsForContract(ctx context.Context, contract model.Contract) (all, attached []model.Seal, err error) {
	attached, err = s.rd.ListSealsForContract(ctx, contract.ID)
	if err != nil {
		return nil, nil, err
	}
	all = append(all, attached...)
	seen := map[string]bool{}
	for _, item := range attached {
		seen[item.ID] = true
	}
	for _, partyID := range []string{contract.PartyAID, contract.PartyBID} {
		if partyID == "" {
			continue
		}
		partySeals, err := s.rd.ListSealsForParty(ctx, partyID)
		if err != nil {
			return nil, nil, err
		}
		for _, item := range partySeals {
			if seen[item.ID] {
				continue
			}
			seen[item.ID] = true
			all = append(all, item)
		}
	}
	return all, attached, nil
}

func asImageError(err error, target **seal.ImageError) bool {
	imgErr, ok := err.(*seal.ImageError)
	if ok {
		*target = imgErr
	}
	return ok
}

func sealRelPath(id, sha string) string {
	return path.Join("assets", "seals", id+"-"+shortSHA(sha)+".png")
}

func shortSHA(sha string) string {
	sha = strings.TrimSpace(sha)
	if len(sha) <= 12 {
		return sha
	}
	return sha[:12]
}

func sealSummary(prefix string, record model.Seal, madeTransparent bool) string {
	summary := fmt.Sprintf("%s「%s」（%dx%d，sha256 %s…）",
		prefix, record.Name, record.WidthPx, record.HeightPx, shortSHA(record.SHA256))
	if madeTransparent {
		summary += "，已处理为透明底"
	}
	return summary
}

func uniqueIDs(ids []string) []string {
	out := make([]string, 0, len(ids))
	seen := map[string]bool{}
	for _, raw := range ids {
		id := strings.TrimSpace(raw)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// assetFile resolves a relative asset path inside the data directory and
// rejects paths that escape it.
func (s *Service) assetFile(rel string) (string, error) {
	if err := validateAssetPath(rel); err != nil {
		return "", err
	}
	if strings.TrimSpace(rel) == "" {
		return "", fmt.Errorf("empty asset path")
	}
	root := strings.TrimSpace(s.dataDir)
	if root == "" {
		return "", fmt.Errorf("data directory is not configured")
	}
	full := filepath.Join(root, filepath.FromSlash(path.Clean(rel)))
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	fullAbs, err := filepath.Abs(full)
	if err != nil {
		return "", err
	}
	if fullAbs != rootAbs && !strings.HasPrefix(fullAbs, rootAbs+string(os.PathSeparator)) {
		return "", fmt.Errorf("asset path %q escapes the data directory", rel)
	}
	return fullAbs, nil
}

// removeAsset deletes an asset file. Failures only log a warning because the
// database change has already committed; a missing file is not an error, which
// keeps repeated cleanup of the same path quiet.
func (s *Service) removeAsset(rel string) {
	if strings.TrimSpace(rel) == "" {
		return
	}
	full, err := s.assetFile(rel)
	if err != nil {
		slog.Warn("解析公章图片路径失败，跳过文件清理", "asset", rel, "error", err)
		return
	}
	if err := os.Remove(full); err != nil && !os.IsNotExist(err) {
		slog.Warn("删除公章图片文件失败", "path", full, "error", err)
	}
}
