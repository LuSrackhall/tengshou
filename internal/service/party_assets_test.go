package service_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LuSrackhall/tengshou/internal/model"
	"github.com/LuSrackhall/tengshou/internal/service"
)

func testPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{uint8(x % 255), uint8(y % 255), 120, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// v0.2: new archives created through the archive surface must carry an ID
// number; the rule is opt-in so bulk imports keep working.
func TestSavePartyRequiresIDNumberWhenRequested(t *testing.T) {
	svc, _, _ := openService(t)
	ctx := context.Background()
	_, _, err := svc.SaveParty(ctx, service.PartyInput{
		Kind: model.PartyPerson, Name: "无证件号", RequireIDNumber: true,
	})
	if err == nil || !strings.Contains(err.Error(), "证件号") {
		t.Fatalf("missing id_number should fail, got %v", err)
	}
	// Existing party may stay empty when edited.
	party, _, err := svc.SaveParty(ctx, service.PartyInput{Kind: model.PartyPerson, Name: "存量档案"})
	if err != nil {
		t.Fatalf("legacy create without flag: %v", err)
	}
	if _, _, err := svc.SaveParty(ctx, service.PartyInput{
		ID: party.ID, Kind: model.PartyPerson, Name: "存量档案", RequireIDNumber: true,
	}); err != nil {
		t.Fatalf("editing a legacy archive must not require id_number: %v", err)
	}
}

func TestSavePartyWarnsOnDuplicateIDNumber(t *testing.T) {
	svc, _, _ := openService(t)
	ctx := context.Background()
	if _, _, err := svc.SaveParty(ctx, service.PartyInput{
		Kind: model.PartyPerson, Name: "甲", IDNumber: "110101199001011234",
	}); err != nil {
		t.Fatalf("create first: %v", err)
	}
	_, warnings, err := svc.SaveParty(ctx, service.PartyInput{
		Kind: model.PartyPerson, Name: "乙", IDNumber: "110101199001011234",
	})
	if err != nil {
		t.Fatalf("duplicate id should warn, not block: %v", err)
	}
	joined := strings.Join(warnings, "；")
	if !strings.Contains(joined, "证件号") || !strings.Contains(joined, "甲") {
		t.Fatalf("warnings = %v", warnings)
	}
}

func TestPartyIDPhotoRoundTrip(t *testing.T) {
	svc, _, _ := openService(t)
	dir := t.TempDir()
	svc.SetDataDir(dir)
	ctx := context.Background()
	party, _, err := svc.SaveParty(ctx, service.PartyInput{
		Kind: model.PartyPerson, Name: "证件照用户", IDNumber: "110101199001011234", RequireIDNumber: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	raw := testPNG(t, 2000, 1000)
	updated, err := svc.SavePartyIDPhoto(ctx, party.ID, model.PartyPhotoFront, raw)
	if err != nil {
		t.Fatalf("save photo: %v", err)
	}
	if updated.IDFrontPath == "" || updated.IDFrontSHA256 == "" {
		t.Fatalf("party photo not recorded: %+v", updated)
	}
	full := filepath.Join(dir, filepath.FromSlash(updated.IDFrontPath))
	info, err := os.Stat(full)
	if err != nil {
		t.Fatalf("stat stored photo: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("photo perm = %#o, want 0600", info.Mode().Perm())
	}
	// The asset directory is private as well.
	dirInfo, err := os.Stat(filepath.Join(dir, "assets", "party-id-photos"))
	if err != nil {
		t.Fatalf("stat photo dir: %v", err)
	}
	if dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("photo dir perm = %#o, want 0700", dirInfo.Mode().Perm())
	}
	data, err := os.ReadFile(full)
	if err != nil {
		t.Fatal(err)
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || format != "jpeg" {
		t.Fatalf("stored photo format = %q err=%v", format, err)
	}
	if cfg.Width > 1600 || cfg.Height > 1600 {
		t.Fatalf("stored photo not scaled: %dx%d", cfg.Width, cfg.Height)
	}
	served, err := svc.PartyIDPhoto(ctx, party.ID, model.PartyPhotoFront)
	if err != nil {
		t.Fatalf("read photo: %v", err)
	}
	sum := sha256.Sum256(served)
	if hex.EncodeToString(sum[:]) != updated.IDFrontSHA256 {
		t.Fatalf("digest mismatch")
	}
	if _, err := svc.PartyIDPhoto(ctx, party.ID, model.PartyPhotoBack); err == nil {
		t.Fatal("missing back photo should fail")
	}
}

func TestAttachPartySignatureAndLookup(t *testing.T) {
	svc, _, _ := openService(t)
	dir := t.TempDir()
	svc.SetDataDir(dir)
	ctx := context.Background()
	party, _, err := svc.SaveParty(ctx, service.PartyInput{
		Kind: model.PartyPerson, Name: "签名用户", IDNumber: "110101199001011234", RequireIDNumber: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	raw := testPNG(t, 8, 4)
	sum := sha256.Sum256(raw)
	sig, err := svc.SaveSignature(ctx, service.SignatureInput{
		PartyID: party.ID, SignerName: party.Name,
		ImagePNG: raw, SHA256: hex.EncodeToString(sum[:]), WidthPx: 8, HeightPx: 4,
	})
	if err != nil {
		t.Fatalf("save signature: %v", err)
	}
	if err := svc.AttachPartySignature(ctx, party.ID, sig.ID); err != nil {
		t.Fatalf("attach: %v", err)
	}
	stored, err := svc.PartySignatureImage(ctx, party.ID)
	if err != nil {
		t.Fatalf("read party signature: %v", err)
	}
	if !bytes.Equal(stored, raw) {
		t.Fatal("party signature bytes changed")
	}
}

func TestEnrollPartyCreatesArchiveWithAssets(t *testing.T) {
	svc, _, _ := openService(t)
	dir := t.TempDir()
	svc.SetDataDir(dir)
	ctx := context.Background()
	sigPNG := testPNG(t, 30, 10)
	sum := sha256.Sum256(sigPNG)
	party, err := svc.EnrollParty(ctx, service.EnrollPartyInput{
		Name:            "自助租户",
		IDNumber:        "330102198001010011",
		Phone:           "13800001111",
		IDFront:         testPNG(t, 40, 30),
		IDBack:          testPNG(t, 40, 30),
		SignaturePNG:    sigPNG,
		SignatureSHA256: hex.EncodeToString(sum[:]),
		SignatureWidth:  30,
		SignatureHeight: 10,
	})
	if err != nil {
		t.Fatalf("enroll: %v", err)
	}
	if party.IDFrontPath == "" || party.IDBackPath == "" || party.SignatureID == "" {
		t.Fatalf("enrollment assets missing: %+v", party)
	}
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(party.IDFrontPath))); err != nil {
		t.Fatalf("front photo file: %v", err)
	}
	if _, err := svc.PartySignatureImage(ctx, party.ID); err != nil {
		t.Fatalf("party signature: %v", err)
	}
	// A second enrollment with the same ID number is rejected with a
	// machine-readable business error the phone page can show.
	_, err = svc.EnrollParty(ctx, service.EnrollPartyInput{
		Name:            "重复租户",
		IDNumber:        "330102198001010011",
		IDFront:         testPNG(t, 40, 30),
		IDBack:          testPNG(t, 40, 30),
		SignaturePNG:    sigPNG,
		SignatureSHA256: hex.EncodeToString(sum[:]),
		SignatureWidth:  30,
		SignatureHeight: 10,
	})
	if code := businessCode(t, err); code != "duplicate_id_number" {
		t.Fatalf("duplicate code = %q, want duplicate_id_number", code)
	}
}

func TestReusePartySignatureOnContract(t *testing.T) {
	svc, _, _ := openService(t)
	dir := t.TempDir()
	svc.SetDataDir(dir)
	ctx := context.Background()
	party := mustParty(t, svc, "复用签名用户")
	contract := mustContract(t, svc, service.ContractInput{Title: "复用签名合同", PartyBID: party.ID})

	raw := testPNG(t, 8, 4)
	sum := sha256.Sum256(raw)
	sig, err := svc.SaveSignature(ctx, service.SignatureInput{
		PartyID: party.ID, SignerName: party.Name,
		ImagePNG: raw, SHA256: hex.EncodeToString(sum[:]), WidthPx: 8, HeightPx: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.AttachPartySignature(ctx, party.ID, sig.ID); err != nil {
		t.Fatal(err)
	}
	reused, err := svc.ReusePartySignature(ctx, contract.ID, party.ID)
	if err != nil {
		t.Fatalf("reuse: %v", err)
	}
	if reused.ContractID != contract.ID || reused.PartyID != party.ID {
		t.Fatalf("reused signature links = %+v", reused)
	}
	if reused.Source != model.SignatureSourceArchive || reused.AssetPath != sig.AssetPath {
		t.Fatalf("reused signature = %+v", reused)
	}
	if reused.ContentSHA256 == "" {
		t.Fatal("reused signature missing content version")
	}
	again, err := svc.ReusePartySignature(ctx, contract.ID, party.ID)
	if err != nil {
		t.Fatalf("reuse again: %v", err)
	}
	if again.ID != reused.ID {
		t.Fatalf("reuse should be idempotent: %s vs %s", again.ID, reused.ID)
	}
	// A party without an archive signature cannot be reused.
	other := mustParty(t, svc, "无档案签名")
	if _, err := svc.ReusePartySignature(ctx, contract.ID, other.ID); err == nil {
		t.Fatal("party without archive signature should fail")
	}
}

func TestNormalizeIDPhotoRejectsBadInput(t *testing.T) {
	if _, err := service.NormalizeIDPhoto(nil); err == nil {
		t.Fatal("empty image should fail")
	}
	if _, err := service.NormalizeIDPhoto([]byte("not an image")); err == nil {
		t.Fatal("invalid image should fail")
	}
	// A valid JPEG also works (phone capture path).
	img := image.NewRGBA(image.Rect(0, 0, 20, 20))
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	photo, err := service.NormalizeIDPhoto(buf.Bytes())
	if err != nil {
		t.Fatalf("jpeg input: %v", err)
	}
	if len(photo.JPEG) == 0 || photo.SHA256 == "" {
		t.Fatalf("normalized photo = %+v", photo)
	}
}

// v0.2: every required field of a self-enrollment submission is validated with
// a Chinese, field-specific message before any archive is created.
func TestEnrollPartyValidation(t *testing.T) {
	svc, _, _ := openService(t)
	svc.SetDataDir(t.TempDir())
	ctx := context.Background()
	sigPNG := testPNG(t, 12, 6)
	sum := sha256.Sum256(sigPNG)
	base := func() service.EnrollPartyInput {
		return service.EnrollPartyInput{
			Name:            "待补全",
			IDNumber:        "330102198001019999",
			Phone:           "13800001111",
			IDFront:         testPNG(t, 20, 12),
			IDBack:          testPNG(t, 20, 12),
			SignaturePNG:    sigPNG,
			SignatureSHA256: hex.EncodeToString(sum[:]),
			SignatureWidth:  12,
			SignatureHeight: 6,
		}
	}
	cases := []struct {
		name   string
		want   string
		mutate func(*service.EnrollPartyInput)
	}{
		{"姓名缺失", "姓名", func(in *service.EnrollPartyInput) { in.Name = "  " }},
		{"证件号缺失", "证件号", func(in *service.EnrollPartyInput) { in.IDNumber = "" }},
		{"证件照正面缺失", "证件照正面", func(in *service.EnrollPartyInput) { in.IDFront = nil }},
		{"证件照反面缺失", "证件照反面", func(in *service.EnrollPartyInput) { in.IDBack = nil }},
		{"手写签名缺失", "手写签名", func(in *service.EnrollPartyInput) { in.SignaturePNG = nil }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := base()
			tc.mutate(&in)
			_, err := svc.EnrollParty(ctx, in)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("EnrollParty error = %v, want message containing %q", err, tc.want)
			}
		})
	}
}
