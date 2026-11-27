package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	"github.com/LuSrackhall/tengshou/internal/model"
	"github.com/LuSrackhall/tengshou/internal/seal"
)

// Party archive seal and signature-stamp images (te-48hkl).
//
// The company seal is retained in seal_assets and linked only through
// parties.seal_asset_id: "停用" clears that link, so contracts stop picking the
// seal up while the immutable asset (and its audit trail) stays. The signature
// stamp (签名章/私章) is retained in signature_assets and linked through
// parties.signature_seal_asset_id; it stamps exactly like an archive signature
// but is reported as a separate source so the contract UI can say which one
// was used.

// PartyImageSource normalizes an archive image source marker. Unknown values
// fall back to the desktop-import marker.
func PartyImageSource(source string) string {
	switch strings.TrimSpace(source) {
	case model.PartyAssetSourceLANEnroll:
		return model.PartyAssetSourceLANEnroll
	default:
		return model.PartyAssetSourceAppUpload
	}
}

// SavePartySealImage imports or replaces a party's official-seal picture. The
// seal is normalized by internal/seal (transparent background where possible)
// and the archive is pointed at the new asset.
func (s *Service) SavePartySealImage(ctx context.Context, partyID string, raw []byte, source string) (model.Party, model.Seal, error) {
	party, err := s.rd.GetParty(ctx, strings.TrimSpace(partyID))
	if err != nil {
		if isNotFound(err) {
			return model.Party{}, model.Seal{}, business("not_found", "相对方档案不存在")
		}
		return model.Party{}, model.Seal{}, err
	}
	name := truncateRunes("档案公章 · "+party.Name, 120)
	record, err := s.ImportSeal(ctx, SealInput{Name: name, Image: raw})
	if err != nil {
		return model.Party{}, model.Seal{}, err
	}
	party.SealAssetID = record.ID
	party.SealSource = PartyImageSource(source)
	if err := s.st.PutParty(ctx, &party); err != nil {
		return model.Party{}, model.Seal{}, err
	}
	s.logChange(ctx, model.ChangeEntityParty, party.ID, model.ChangeActionUpdate,
		fmt.Sprintf("保存档案公章：%s（来源：%s）", party.Name, partySourceLabel(party.SealSource)), nil)
	return party, record, nil
}

// PartySeal returns the party's archive seal record.
func (s *Service) PartySeal(ctx context.Context, partyID string) (model.Seal, error) {
	party, err := s.rd.GetParty(ctx, strings.TrimSpace(partyID))
	if err != nil {
		if isNotFound(err) {
			return model.Seal{}, business("not_found", "相对方档案不存在")
		}
		return model.Seal{}, err
	}
	if party.SealAssetID == "" {
		return model.Seal{}, business("not_found", "该档案还没有公章图片")
	}
	record, err := s.rd.GetSeal(ctx, party.SealAssetID)
	if err != nil {
		if isNotFound(err) {
			return model.Seal{}, business("not_found", "档案公章记录不存在，请重新上传")
		}
		return model.Seal{}, err
	}
	return record, nil
}

// PartySealImage reads the archive seal picture after verifying its digest.
func (s *Service) PartySealImage(ctx context.Context, partyID string) ([]byte, error) {
	record, err := s.PartySeal(ctx, partyID)
	if err != nil {
		return nil, err
	}
	full, err := s.assetFile(record.AssetPath)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(full)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, business("missing_asset", "档案公章文件不在数据目录中，请重新上传")
		}
		return nil, err
	}
	sum := sha256.Sum256(raw)
	if !strings.EqualFold(hex.EncodeToString(sum[:]), strings.TrimSpace(record.SHA256)) {
		return nil, business("checksum_mismatch", "档案公章文件校验失败（摘要与记录不符），请重新上传")
	}
	return raw, nil
}

// ClearPartySeal disables archive-seal reuse for the party without deleting
// the immutable seal asset.
func (s *Service) ClearPartySeal(ctx context.Context, partyID string) (model.Party, error) {
	party, err := s.rd.GetParty(ctx, strings.TrimSpace(partyID))
	if err != nil {
		if isNotFound(err) {
			return model.Party{}, business("not_found", "相对方档案不存在")
		}
		return model.Party{}, err
	}
	if party.SealAssetID == "" {
		return party, nil
	}
	party.SealAssetID = ""
	party.SealSource = ""
	if err := s.st.PutParty(ctx, &party); err != nil {
		return model.Party{}, err
	}
	s.logChange(ctx, model.ChangeEntityParty, party.ID, model.ChangeActionUpdate,
		fmt.Sprintf("停用档案公章：%s", party.Name), nil)
	return party, nil
}

// SavePartySignatureSeal stores a party's signature stamp (签名章/私章) as a
// signature asset alternative to the handwritten archive signature.
func (s *Service) SavePartySignatureSeal(ctx context.Context, partyID string, raw []byte, source string) (model.Party, error) {
	party, err := s.rd.GetParty(ctx, strings.TrimSpace(partyID))
	if err != nil {
		if isNotFound(err) {
			return model.Party{}, business("not_found", "相对方档案不存在")
		}
		return model.Party{}, err
	}
	normalized, err := seal.Normalize(raw)
	if err != nil {
		var imgErr *seal.ImageError
		if ok := asImageError(err, &imgErr); ok {
			return model.Party{}, business(imgErr.Code, "%s", imgErr.Message)
		}
		return model.Party{}, err
	}
	sig, err := s.SaveSignature(ctx, SignatureInput{
		PartyID:    party.ID,
		SignerName: party.Name,
		Source:     PartyImageSource(source),
		ImagePNG:   normalized.PNG,
		SHA256:     normalized.SHA256,
		WidthPx:    normalized.Width,
		HeightPx:   normalized.Height,
	})
	if err != nil {
		return model.Party{}, err
	}
	party.SignatureSealID = sig.ID
	party.SignatureSealSource = sig.Source
	if err := s.st.PutParty(ctx, &party); err != nil {
		return model.Party{}, err
	}
	s.logChange(ctx, model.ChangeEntityParty, party.ID, model.ChangeActionUpdate,
		fmt.Sprintf("保存档案签名章：%s（来源：%s）", party.Name, partySourceLabel(sig.Source)), nil)
	return party, nil
}

// PartySignatureSealImage reads the archive signature-stamp picture after
// verifying its digest.
func (s *Service) PartySignatureSealImage(ctx context.Context, partyID string) ([]byte, error) {
	party, err := s.rd.GetParty(ctx, strings.TrimSpace(partyID))
	if err != nil {
		if isNotFound(err) {
			return nil, business("not_found", "相对方档案不存在")
		}
		return nil, err
	}
	if party.SignatureSealID == "" {
		return nil, business("not_found", "该档案还没有签名章图片")
	}
	sig, err := s.rd.GetSignature(ctx, party.SignatureSealID)
	if err != nil {
		if isNotFound(err) {
			return nil, business("not_found", "档案签名章记录不存在，请重新上传")
		}
		return nil, err
	}
	full, err := s.assetFile(sig.AssetPath)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(full)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, business("missing_asset", "档案签名章文件不在数据目录中，请重新上传")
		}
		return nil, err
	}
	sum := sha256.Sum256(raw)
	if !strings.EqualFold(hex.EncodeToString(sum[:]), strings.TrimSpace(sig.SHA256)) {
		return nil, business("checksum_mismatch", "档案签名章文件校验失败（摘要与记录不符），请重新上传")
	}
	return raw, nil
}

// ClearPartySignatureSeal disables the signature stamp without deleting the
// immutable signature asset.
func (s *Service) ClearPartySignatureSeal(ctx context.Context, partyID string) (model.Party, error) {
	party, err := s.rd.GetParty(ctx, strings.TrimSpace(partyID))
	if err != nil {
		if isNotFound(err) {
			return model.Party{}, business("not_found", "相对方档案不存在")
		}
		return model.Party{}, err
	}
	if party.SignatureSealID == "" {
		return party, nil
	}
	party.SignatureSealID = ""
	party.SignatureSealSource = ""
	if err := s.st.PutParty(ctx, &party); err != nil {
		return model.Party{}, err
	}
	s.logChange(ctx, model.ChangeEntityParty, party.ID, model.ChangeActionUpdate,
		fmt.Sprintf("停用档案签名章：%s", party.Name), nil)
	return party, nil
}

func partySourceLabel(source string) string {
	if source == model.PartyAssetSourceLANEnroll {
		return "手机自助录入"
	}
	return "桌面导入"
}
