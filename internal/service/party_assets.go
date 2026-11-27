package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	xdraw "golang.org/x/image/draw"

	"github.com/LuSrackhall/tengshou/internal/model"
	"github.com/LuSrackhall/tengshou/internal/seal"
)

// ID-card photos are compressed to a bounded JPEG before being written under
// the private data directory. The database stores the path and digest only.
const (
	maxIDPhotoBytes  = 15 << 20 // 15 MiB per submitted image
	maxIDPhotoPixels = 24_000_000
	maxIDPhotoSide   = 1600 // longest edge of the stored JPEG
	idPhotoJPEGQ     = 82
)

// NormalizedIDPhoto is a compressed ID-card photo ready to be stored.
type NormalizedIDPhoto struct {
	JPEG   []byte
	SHA256 string
	Width  int
	Height int
}

// NormalizeIDPhoto decodes a submitted JPEG/PNG photo, scales it down to a
// bounded size and re-encodes it as JPEG so archives stay small.
func NormalizeIDPhoto(raw []byte) (NormalizedIDPhoto, error) {
	if len(raw) == 0 {
		return NormalizedIDPhoto{}, business("invalid_image", "证件照图片为空，请重新上传")
	}
	if len(raw) > maxIDPhotoBytes {
		return NormalizedIDPhoto{}, business("file_too_large", "证件照图片过大，请换一张尺寸小一些的照片")
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return NormalizedIDPhoto{}, business("invalid_image", "证件照无法识别，请上传 JPG 或 PNG 图片")
	}
	if format != "jpeg" && format != "png" {
		return NormalizedIDPhoto{}, business("invalid_image", "证件照必须是 JPG 或 PNG 图片")
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > maxIDPhotoPixels {
		return NormalizedIDPhoto{}, business("invalid_image", "证件照尺寸异常，请重新拍摄或选择")
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return NormalizedIDPhoto{}, business("invalid_image", "证件照无法解码，请重新上传")
	}
	src := img.Bounds()
	srcW, srcH := src.Dx(), src.Dy()
	dstW, dstH := srcW, srcH
	if longest := max(srcW, srcH); longest > maxIDPhotoSide {
		scale := float64(maxIDPhotoSide) / float64(longest)
		dstW = max(1, int(float64(srcW)*scale))
		dstH = max(1, int(float64(srcH)*scale))
	}
	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, src, xdraw.Src, nil)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: idPhotoJPEGQ}); err != nil {
		return NormalizedIDPhoto{}, fmt.Errorf("压缩证件照: %w", err)
	}
	sum := sha256.Sum256(buf.Bytes())
	return NormalizedIDPhoto{
		JPEG:   buf.Bytes(),
		SHA256: hex.EncodeToString(sum[:]),
		Width:  dstW,
		Height: dstH,
	}, nil
}

// SavePartyIDPhoto stores (or replaces) one side of a party's ID-card photos.
func (s *Service) SavePartyIDPhoto(ctx context.Context, partyID, side string, raw []byte) (model.Party, error) {
	side, ok := normalizePhotoSide(side)
	if !ok {
		return model.Party{}, business("invalid_side", "证件照面别只能是正面或反面")
	}
	party, err := s.rd.GetParty(ctx, strings.TrimSpace(partyID))
	if err != nil {
		if isNotFound(err) {
			return model.Party{}, business("not_found", "相对方档案不存在")
		}
		return model.Party{}, err
	}
	photo, err := NormalizeIDPhoto(raw)
	if err != nil {
		return model.Party{}, err
	}
	rel, err := s.writePartyIDPhotoFile(party.ID, side, photo)
	if err != nil {
		return model.Party{}, err
	}
	if side == model.PartyPhotoFront {
		party.IDFrontPath, party.IDFrontSHA256 = rel, photo.SHA256
	} else {
		party.IDBackPath, party.IDBackSHA256 = rel, photo.SHA256
	}
	if err := s.st.PutParty(ctx, &party); err != nil {
		return model.Party{}, err
	}
	label := "正面"
	if side == model.PartyPhotoBack {
		label = "反面"
	}
	s.logChange(ctx, model.ChangeEntityParty, party.ID, model.ChangeActionUpdate,
		fmt.Sprintf("更新证件照（%s）：%s", label, party.Name), nil)
	return party, nil
}

// PartyIDPhoto reads one stored ID-card photo after verifying its digest.
func (s *Service) PartyIDPhoto(ctx context.Context, partyID, side string) ([]byte, error) {
	side, ok := normalizePhotoSide(side)
	if !ok {
		return nil, business("invalid_side", "证件照面别只能是正面或反面")
	}
	party, err := s.rd.GetParty(ctx, strings.TrimSpace(partyID))
	if err != nil {
		if isNotFound(err) {
			return nil, business("not_found", "相对方档案不存在")
		}
		return nil, err
	}
	rel, want := party.IDFrontPath, party.IDFrontSHA256
	if side == model.PartyPhotoBack {
		rel, want = party.IDBackPath, party.IDBackSHA256
	}
	if rel == "" {
		return nil, business("not_found", "该档案还没有这张证件照")
	}
	full, err := s.assetFile(rel)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(full)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, business("missing_asset", "证件照文件不在数据目录中，请重新上传")
		}
		return nil, err
	}
	sum := sha256.Sum256(raw)
	if !strings.EqualFold(hex.EncodeToString(sum[:]), strings.TrimSpace(want)) {
		return nil, business("checksum_mismatch", "证件照文件校验失败（摘要与记录不符），请重新上传")
	}
	return raw, nil
}

// PartySignatureImage reads the archive-level signature PNG after verifying
// its digest.
func (s *Service) PartySignatureImage(ctx context.Context, partyID string) ([]byte, error) {
	party, err := s.rd.GetParty(ctx, strings.TrimSpace(partyID))
	if err != nil {
		if isNotFound(err) {
			return nil, business("not_found", "相对方档案不存在")
		}
		return nil, err
	}
	if party.SignatureID == "" {
		return nil, business("not_found", "该档案还没有签名")
	}
	sig, err := s.rd.GetSignature(ctx, party.SignatureID)
	if err != nil {
		if isNotFound(err) {
			return nil, business("not_found", "档案签名记录不存在")
		}
		return nil, err
	}
	full, err := s.assetFile(sig.AssetPath)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(full)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(raw)
	if !strings.EqualFold(hex.EncodeToString(sum[:]), strings.TrimSpace(sig.SHA256)) {
		return nil, business("checksum_mismatch", "签名文件校验失败（摘要与记录不符）")
	}
	return raw, nil
}

// AttachPartySignature marks a retained signature as the party's archive
// signature. It is called after a LAN signature session submits for a party
// (no contract), so later contracts can stamp it directly (v0.2 D3=B).
func (s *Service) AttachPartySignature(ctx context.Context, partyID, signatureID string) error {
	party, err := s.rd.GetParty(ctx, strings.TrimSpace(partyID))
	if err != nil {
		if isNotFound(err) {
			return business("not_found", "相对方档案不存在")
		}
		return err
	}
	if sig, err := s.rd.GetSignature(ctx, strings.TrimSpace(signatureID)); err != nil {
		if isNotFound(err) {
			return business("not_found", "签名记录不存在")
		}
		return err
	} else if sig.PartyID != "" && sig.PartyID != party.ID {
		return business("invalid_signature", "该签名属于其他档案，不能挂到当前档案")
	}
	party.SignatureID = strings.TrimSpace(signatureID)
	if err := s.st.PutParty(ctx, &party); err != nil {
		return err
	}
	s.logChange(ctx, model.ChangeEntityParty, party.ID, model.ChangeActionUpdate,
		fmt.Sprintf("保存档案级手写签名：%s", party.Name), nil)
	return nil
}

// EnrollPartyInput is one LAN self-enrollment submission: the tenant fills the
// archive fields, photographs both sides of the ID card and signs on a phone.
// The signature may be handwritten (SignaturePNG, already normalized by
// internal/sign) or a signature-stamp picture (SignatureSeal, raw bytes). The
// company seal (Seal) is optional for enterprise tenants.
type EnrollPartyInput struct {
	Name            string
	IDNumber        string
	Phone           string
	Address         string
	IDFront         []byte
	IDBack          []byte
	SignaturePNG    []byte
	SignatureSHA256 string
	SignatureWidth  int
	SignatureHeight int
	SignatureSeal   []byte
	Seal            []byte
}

// EnrollParty creates a complete archive from a self-enrollment submission.
// A duplicate ID number is a hard error: the tenant is told to contact the
// landlord instead of silently creating a second archive.
func (s *Service) EnrollParty(ctx context.Context, in EnrollPartyInput) (model.Party, error) {
	// Serialize the duplicate check with archive creation: two phones must not
	// create two archives for the same ID number.
	unlock := s.writeLock()
	defer unlock()

	name := truncateRunes(strings.TrimSpace(in.Name), 120)
	idNumber := truncateRunes(strings.TrimSpace(in.IDNumber), 120)
	validation := &ValidationErrors{}
	if name == "" {
		validation.add(model.FieldPartyName, "姓名", "不能为空")
	}
	if idNumber == "" {
		validation.add(model.FieldPartyIDNo, "证件号", "不能为空")
	}
	if len(in.IDFront) == 0 {
		validation.add("id_front", "证件照正面", "请上传证件照正面")
	}
	if len(in.IDBack) == 0 {
		validation.add("id_back", "证件照反面", "请上传证件照反面")
	}
	if len(in.SignaturePNG) == 0 && len(in.SignatureSeal) == 0 {
		validation.add("signature", "手写签名/签名章", "请手写签名或上传签名章图片")
	}
	if len(in.SignaturePNG) > 0 && (strings.TrimSpace(in.SignatureSHA256) == "") {
		validation.add("signature", "手写签名", "请手写签名")
	}
	if err := validation.errOrNil(); err != nil {
		return model.Party{}, err
	}
	if strings.TrimSpace(s.dataDir) == "" {
		return model.Party{}, business("not_configured", "数据目录未配置，无法保存证件照与签名")
	}
	// Normalize the optional pictures before creating the archive so an
	// unreadable upload cannot leave a half-built record behind.
	var sealImage, sealSignature seal.NormalizedImage
	if len(in.Seal) > 0 {
		normalized, err := seal.Normalize(in.Seal)
		if err != nil {
			return model.Party{}, sealBusinessError(err, "公章")
		}
		sealImage = normalized
	}
	if len(in.SignatureSeal) > 0 {
		normalized, err := seal.Normalize(in.SignatureSeal)
		if err != nil {
			return model.Party{}, sealBusinessError(err, "签名章")
		}
		sealSignature = normalized
	}
	matches, err := s.rd.FindPartiesByIDNumber(ctx, idNumber)
	if err != nil {
		return model.Party{}, err
	}
	if len(matches) > 0 {
		return model.Party{}, business("duplicate_id_number",
			"证件号 %s 已有档案（%s）；请联系出租方处理，不能重复建档", MaskIDNumber(idNumber), matches[0].Name)
	}
	front, err := NormalizeIDPhoto(in.IDFront)
	if err != nil {
		return model.Party{}, err
	}
	back, err := NormalizeIDPhoto(in.IDBack)
	if err != nil {
		return model.Party{}, err
	}

	party, _, err := s.SaveParty(ctx, PartyInput{
		Kind:            model.PartyPerson,
		Name:            name,
		IDNumber:        idNumber,
		Phone:           in.Phone,
		Address:         in.Address,
		RequireIDNumber: true,
		Source:          "self_enroll",
	})
	if err != nil {
		return model.Party{}, err
	}
	frontPath, err := s.writePartyIDPhotoFile(party.ID, model.PartyPhotoFront, front)
	if err != nil {
		return model.Party{}, err
	}
	backPath, err := s.writePartyIDPhotoFile(party.ID, model.PartyPhotoBack, back)
	if err != nil {
		return model.Party{}, err
	}
	var sig model.Signature
	if len(in.SignaturePNG) > 0 {
		sig, err = s.SaveSignature(ctx, SignatureInput{
			PartyID:    party.ID,
			SignerName: party.Name,
			Source:     model.SignatureSourceLANQR,
			ImagePNG:   in.SignaturePNG,
			SHA256:     in.SignatureSHA256,
			WidthPx:    in.SignatureWidth,
			HeightPx:   in.SignatureHeight,
		})
		if err != nil {
			return model.Party{}, err
		}
		party.SignatureID = sig.ID
	}
	if len(in.SignatureSeal) > 0 {
		signatureSeal, err := s.SaveSignature(ctx, SignatureInput{
			PartyID:    party.ID,
			SignerName: party.Name,
			Source:     model.PartyAssetSourceLANEnroll,
			ImagePNG:   sealSignature.PNG,
			SHA256:     sealSignature.SHA256,
			WidthPx:    sealSignature.Width,
			HeightPx:   sealSignature.Height,
		})
		if err != nil {
			return model.Party{}, err
		}
		party.SignatureSealID = signatureSeal.ID
		party.SignatureSealSource = model.PartyAssetSourceLANEnroll
	}
	if len(in.Seal) > 0 {
		record, err := s.ImportSeal(ctx, SealInput{
			Name:  truncateRunes("档案公章 · "+party.Name, 120),
			Image: sealImage.PNG,
		})
		if err != nil {
			return model.Party{}, err
		}
		party.SealAssetID = record.ID
		party.SealSource = model.PartyAssetSourceLANEnroll
	}
	party.IDFrontPath, party.IDFrontSHA256 = frontPath, front.SHA256
	party.IDBackPath, party.IDBackSHA256 = backPath, back.SHA256
	if err := s.st.PutParty(ctx, &party); err != nil {
		return model.Party{}, err
	}
	return party, nil
}

// sealBusinessError turns a seal package rejection into a user-facing error
// labelled with the field the tenant uploaded.
func sealBusinessError(err error, label string) error {
	var imgErr *seal.ImageError
	if asImageError(err, &imgErr) {
		return business(imgErr.Code, "%s：%s", label, imgErr.Message)
	}
	return err
}

func normalizePhotoSide(side string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(side)) {
	case model.PartyPhotoFront:
		return model.PartyPhotoFront, true
	case model.PartyPhotoBack:
		return model.PartyPhotoBack, true
	}
	return "", false
}

// writePartyIDPhotoFile writes the compressed JPEG and returns its path
// relative to the data directory.
func (s *Service) writePartyIDPhotoFile(partyID, side string, photo NormalizedIDPhoto) (string, error) {
	root := strings.TrimSpace(s.dataDir)
	if root == "" {
		return "", business("not_configured", "数据目录未配置，无法保存证件照")
	}
	rel := path.Join("assets", "party-id-photos", partyID+"-"+side+".jpg")
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		return "", fmt.Errorf("创建证件照目录: %w", err)
	}
	if err := writeFileAtomic(full, photo.JPEG, 0o600); err != nil {
		return "", fmt.Errorf("写入证件照: %w", err)
	}
	return filepath.ToSlash(rel), nil
}
