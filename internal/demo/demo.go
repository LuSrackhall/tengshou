package demo

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/LuSrackhall/tengshou/internal/config"
	"github.com/LuSrackhall/tengshou/internal/model"
	"github.com/LuSrackhall/tengshou/internal/pdfdoc"
	"github.com/LuSrackhall/tengshou/internal/store"
)

//go:embed assets/contract_basic.html
var assets embed.FS

// Result summarizes a generated demo.
type Result struct {
	Contract  model.Contract
	PartyA    model.Party
	PartyB    model.Party
	Signature model.Signature
	Template  model.Template
	Document  model.OutputDocument
	PDFPath   string
	FontPath  string
}

// Run seeds one contract (parties, signature image, HTML template), renders it
// to PDF and records the output. It is idempotent: fixed demo ids are upserted.
func Run(ctx context.Context, st *store.Store, gen *pdfdoc.Generator, paths config.Paths, now time.Time) (Result, error) {
	if err := paths.EnsureDirs(); err != nil {
		return Result{}, err
	}
	templateHTML, err := assets.ReadFile("assets/contract_basic.html")
	if err != nil {
		return Result{}, err
	}
	userTemplate := filepath.Join(paths.Templates, "contract_basic.html")
	if _, err := os.Stat(userTemplate); os.IsNotExist(err) {
		if err := os.WriteFile(userTemplate, templateHTML, 0o600); err != nil {
			return Result{}, err
		}
	}

	partyA := model.Party{
		ID:      "demo-party-a",
		Kind:    model.PartyOrganization,
		Name:    "幸福村股份经济合作社",
		Address: "幸福村村民委员会办公楼",
		Data: map[string]any{
			"legal_representative": "李国强",
			"contact_phone":        "13800000001",
		},
	}
	partyB := model.Party{
		ID:       "demo-party-b",
		Kind:     model.PartyPerson,
		Name:     "张三",
		IDNumber: "11010119900101001X",
		Phone:    "13800000002",
		Address:  "幸福村 3 组 12 号",
		Data: map[string]any{
			"emergency_contact": "李四 13800000003",
		},
	}
	if err := st.PutParty(ctx, &partyA); err != nil {
		return Result{}, err
	}
	if err := st.PutParty(ctx, &partyB); err != nil {
		return Result{}, err
	}

	sigRel := filepath.ToSlash(filepath.Join("assets", "signatures", "demo-signature.png"))
	sigPath := filepath.Join(paths.Root, filepath.FromSlash(sigRel))
	sigSHA, sigW, sigH, err := ensureSignature(sigPath)
	if err != nil {
		return Result{}, err
	}
	signature := model.Signature{
		ID:         "demo-signature-1",
		PartyID:    partyB.ID,
		SignerName: partyB.Name,
		AssetPath:  sigRel,
		SHA256:     sigSHA,
		WidthPx:    sigW,
		HeightPx:   sigH,
		CapturedAt: now,
		CreatedAt:  now,
	}
	if err := st.PutSignature(ctx, &signature); err != nil {
		return Result{}, err
	}

	template := model.Template{
		ID:        "demo-template-basic",
		Name:      "房屋租赁合同（示例）",
		Version:   1,
		HTML:      string(templateHTML),
		Variables: pdfdoc.ExtractVariables(string(templateHTML)),
		IsActive:  true,
	}
	if err := st.PutTemplate(ctx, &template); err != nil {
		return Result{}, err
	}

	contract := model.Contract{
		ID:         "demo-contract-1",
		Title:      "幸福村 3 组 12 号房屋租赁合同",
		TemplateID: template.ID,
		PartyAID:   partyA.ID,
		PartyBID:   partyB.ID,
		Status:     model.ContractActive,
		SignedOn:   "2026-11-22",
		ValidFrom:  "2026-12-01",
		ValidTo:    "2027-11-30",
		Data: map[string]any{
			"address":    "幸福村 3 组 12 号",
			"area":       "120 平方米",
			"usage":      "居住",
			"rent":       1200,
			"deposit":    2400,
			"pay_method": "按季度支付，每季度首月 5 日前付清",
			"clause_1":   "甲方将位于幸福村 3 组 12 号的房屋出租给乙方居住使用，租赁期限自 2026 年 12 月 1 日起至 2027 年 11 月 30 日止。租赁期满，乙方享有同等条件下的优先承租权。",
			"clause_2":   "乙方应爱护房屋及附属设施，不得擅自改变房屋结构。因乙方使用不当造成损坏的，由乙方负责修复或赔偿。乙方需要装修的，应事先取得甲方书面同意。",
			"clause_3":   "租赁期间的水费、电费、燃气费、网络费等由乙方承担；房屋本体及原有设施的维修费用由甲方承担。双方应本着诚实信用原则履行本合同。",
			"clause_4":   "任何一方违反本合同约定，应当承担违约责任并赔偿对方因此遭受的损失。本合同未尽事宜，双方可另行协商签订补充协议，补充协议与本合同具有同等法律效力。",
			"signed_on":  "2026-11-22",
			"signature":  "assets/signatures/demo-signature.png",
		},
	}
	if err := st.PutContract(ctx, &contract); err != nil {
		return Result{}, err
	}

	data := pdfdoc.TemplateData{
		Contract:   contract,
		PartyA:     &partyA,
		PartyB:     &partyB,
		Signatures: []model.Signature{signature},
		Data:       contract.Data,
		Now:        now,
	}
	outName := fmt.Sprintf("%s-%s.pdf", contract.ID, now.Format("20060102-150405"))
	outPath := filepath.Join(paths.Outputs, outName)
	res, err := gen.RenderFile(template.HTML, data, paths.Root, contract.Title, outPath)
	if err != nil {
		return Result{}, err
	}
	document := model.OutputDocument{
		ID:              "demo-document-1",
		ContractID:      contract.ID,
		TemplateID:      template.ID,
		TemplateVersion: template.Version,
		FilePath:        filepath.ToSlash(filepath.Join("outputs", outName)),
		SHA256:          res.SHA256,
		PageCount:       res.PageCount,
		CreatedAt:       now,
	}
	if err := st.PutOutputDocument(ctx, &document); err != nil {
		return Result{}, err
	}
	return Result{
		Contract:  contract,
		PartyA:    partyA,
		PartyB:    partyB,
		Signature: signature,
		Template:  template,
		Document:  document,
		PDFPath:   outPath,
		FontPath:  res.FontPath,
	}, nil
}

// ensureSignature writes a generated handwritten-style signature PNG if it
// does not exist and returns its digest and dimensions.
func ensureSignature(path string) (string, int, int, error) {
	if raw, err := os.ReadFile(path); err == nil {
		img, err := png.Decode(bytes.NewReader(raw))
		if err == nil {
			sum := sha256.Sum256(raw)
			b := img.Bounds()
			return hex.EncodeToString(sum[:]), b.Dx(), b.Dy(), nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", 0, 0, err
	}
	const w, h = 480, 160
	mask := image.NewAlpha(image.Rect(0, 0, w, h))
	stroke(mask, []point{{40, 105}, {90, 45}, {140, 120}, {190, 60}, {240, 110}}, 3.2)
	stroke(mask, []point{{210, 55}, {250, 125}, {300, 50}, {350, 115}, {410, 70}}, 3.0)
	stroke(mask, []point{{70, 135}, {180, 128}, {300, 136}, {420, 130}}, 1.6)

	img := image.NewRGBA(image.Rect(0, 0, w, h))
	ink := image.NewUniform(color.RGBA{24, 32, 64, 255})
	draw.DrawMask(img, img.Bounds(), ink, image.Point{}, mask, image.Point{}, draw.Over)

	f, err := os.Create(path)
	if err != nil {
		return "", 0, 0, err
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		return "", 0, 0, err
	}
	if err := f.Close(); err != nil {
		return "", 0, 0, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", 0, 0, err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), w, h, nil
}

type point struct{ x, y float64 }

// stroke stamps a soft round brush along a sampled polyline.
func stroke(mask *image.Alpha, pts []point, radius float64) {
	for i := 0; i < len(pts)-1; i++ {
		p0, p1 := pts[i], pts[i+1]
		dist := math.Hypot(p1.x-p0.x, p1.y-p0.y)
		steps := int(dist/0.8) + 1
		for s := 0; s <= steps; s++ {
			t := float64(s) / float64(steps)
			cx := p0.x + (p1.x-p0.x)*t
			cy := p0.y + (p1.y-p0.y)*t
			stampDisc(mask, cx, cy, radius)
		}
	}
}

func stampDisc(mask *image.Alpha, cx, cy, radius float64) {
	x0, x1 := int(cx-radius-1), int(cx+radius+2)
	y0, y1 := int(cy-radius-1), int(cy+radius+2)
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			if !(image.Point{x, y}).In(mask.Bounds()) {
				continue
			}
			d := math.Hypot(float64(x)-cx, float64(y)-cy)
			if d > radius {
				continue
			}
			alpha := uint8(255 * math.Min(1, (radius-d)/0.9))
			i := mask.PixOffset(x, y)
			if alpha > mask.Pix[i] {
				mask.Pix[i] = alpha
			}
		}
	}
}
