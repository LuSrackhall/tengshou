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

	"github.com/LuSrackhall/tengshou/internal/app"
	"github.com/LuSrackhall/tengshou/internal/model"
	"github.com/LuSrackhall/tengshou/internal/pdfdoc"
	"github.com/LuSrackhall/tengshou/internal/service"
)

//go:embed assets/contract_basic.html
var assets embed.FS

// Result summarizes a generated demo.
type Result struct {
	Contract           model.Contract
	HistoricalContract model.Contract
	PartyA             model.Party
	PartyB             model.Party
	Signature          model.Signature
	Template           model.Template
	Document           model.OutputDocument
	Fields             []model.FieldDefinition
	PDFPath            string
	FontPath           string
}

// Run seeds the demo dataset entirely through the product's own service layer:
// every sample field is created with CreateField, every party and contract goes
// through SaveParty / SaveContract, so there are no hard-coded sample columns.
// It is idempotent: records are matched by contract number and archive name.
func Run(ctx context.Context, a *app.App, now time.Time) (Result, error) {
	gen, err := a.Generator()
	if err != nil {
		return Result{}, err
	}
	paths := a.Paths
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

	svc := a.Service
	contractFields := []service.FieldInput{
		fieldInput(model.FieldEntityContract, "address", "房屋地址", model.FieldTypeText, "租赁房屋的坐落位置"),
		fieldInput(model.FieldEntityContract, "area_sqm", "平方数", model.FieldTypeNumber, "建筑面积（平方米）"),
		fieldInput(model.FieldEntityContract, "usage", "用途", model.FieldTypeText, "房屋用途"),
		fieldInput(model.FieldEntityContract, "rent", "月租金", model.FieldTypeMoney, "每月租金（元）"),
		fieldInput(model.FieldEntityContract, "deposit", "押金", model.FieldTypeMoney, "押金金额（元）"),
		fieldInput(model.FieldEntityContract, "pay_method", "付款方式", model.FieldTypeLongText, "租金支付周期与方式"),
		fieldInput(model.FieldEntityContract, "clause_1", "第一条", model.FieldTypeLongText, "租赁期限条款正文"),
		fieldInput(model.FieldEntityContract, "clause_2", "第二条", model.FieldTypeLongText, "房屋使用与维修条款正文"),
		fieldInput(model.FieldEntityContract, "clause_3", "第三条", model.FieldTypeLongText, "费用承担条款正文"),
		fieldInput(model.FieldEntityContract, "clause_4", "第四条", model.FieldTypeLongText, "违约责任条款正文"),
		imageFieldInput(model.FieldEntityContract, "signature", "签名图片", "assets/signatures/demo-signature.png"),
	}
	partyFields := []service.FieldInput{
		fieldInput(model.FieldEntityParty, "legal_representative", "法定代表人", model.FieldTypeText, ""),
		fieldInput(model.FieldEntityParty, "contact_phone", "联系电话", model.FieldTypeText, ""),
		fieldInput(model.FieldEntityParty, "emergency_contact", "紧急联系人", model.FieldTypeText, ""),
	}
	createdFields := []model.FieldDefinition{}
	for _, in := range append(append([]service.FieldInput{}, contractFields...), partyFields...) {
		field, err := ensureField(ctx, svc, in)
		if err != nil {
			return Result{}, err
		}
		createdFields = append(createdFields, field)
	}

	partyA, err := upsertParty(ctx, svc, service.PartyInput{
		Kind:    model.PartyOrganization,
		Name:    "幸福村股份经济合作社",
		Address: "幸福村村民委员会办公楼",
		Data: map[string]any{
			"legal_representative": "李国强",
			"contact_phone":        "13800000001",
		},
	})
	if err != nil {
		return Result{}, err
	}
	partyB, err := upsertParty(ctx, svc, service.PartyInput{
		Kind:     model.PartyPerson,
		Name:     "张三",
		IDNumber: "11010119900101001X",
		Phone:    "13800000002",
		Address:  "幸福村 3 组 12 号",
		Data: map[string]any{
			"emergency_contact": "李四 13800000003",
		},
	})
	if err != nil {
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
	if err := a.Store.PutSignature(ctx, &signature); err != nil {
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
	if err := a.Store.PutTemplate(ctx, &template); err != nil {
		return Result{}, err
	}

	mainInput := service.ContractInput{
		ContractNo: "DEMO-2026-001",
		Year:       2026,
		Title:      "幸福村 3 组 12 号房屋租赁合同",
		Type:       "房屋租赁",
		TemplateID: template.ID,
		PartyAID:   partyA.ID,
		PartyBID:   partyB.ID,
		Status:     model.ContractActive,
		SignedOn:   "2026-11-22",
		ValidFrom:  "2026-12-01",
		ValidTo:    "2027-11-30",
		Amount:     1200,
		Data: map[string]any{
			"address":    "幸福村 3 组 12 号",
			"area_sqm":   120,
			"usage":      "居住",
			"rent":       1200,
			"deposit":    2400,
			"pay_method": "按季度支付，每季度首月 5 日前付清",
			"clause_1":   "甲方将位于幸福村 3 组 12 号的房屋出租给乙方居住使用，租赁期限自 2026 年 12 月 1 日起至 2027 年 11 月 30 日止。租赁期满，乙方享有同等条件下的优先承租权。",
			"clause_2":   "乙方应爱护房屋及附属设施，不得擅自改变房屋结构。因乙方使用不当造成损坏的，由乙方负责修复或赔偿。乙方需要装修的，应事先取得甲方书面同意。",
			"clause_3":   "租赁期间的水费、电费、燃气费、网络费等由乙方承担；房屋本体及原有设施的维修费用由甲方承担。双方应本着诚实信用原则履行本合同。",
			"clause_4":   "任何一方违反本合同约定，应当承担违约责任并赔偿对方因此遭受的损失。本合同未尽事宜，双方可另行协商签订补充协议，补充协议与本合同具有同等法律效力。",
			"signature":  sigRel,
		},
		Source: "demo",
	}
	contract, _, err := upsertContract(ctx, svc, mainInput)
	if err != nil {
		return Result{}, err
	}

	historicalInput := service.ContractInput{
		ContractNo: "DEMO-2025-001",
		Year:       2025,
		Title:      "幸福村 2 组 8 号房屋租赁合同（2025 年）",
		Type:       "房屋租赁",
		TemplateID: template.ID,
		PartyAID:   partyA.ID,
		PartyBID:   partyB.ID,
		Status:     model.ContractActive,
		SignedOn:   "2025-01-05",
		ValidFrom:  "2025-01-10",
		ValidTo:    "2025-12-31",
		Amount:     1000,
		Data: map[string]any{
			"address":  "幸福村 2 组 8 号",
			"area_sqm": 96,
			"usage":    "居住",
			"rent":     1000,
		},
		Source: "demo",
	}
	historical, _, err := upsertContract(ctx, svc, historicalInput)
	if err != nil {
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
	if err := a.Store.PutOutputDocument(ctx, &document); err != nil {
		return Result{}, err
	}
	return Result{
		Contract:           contract,
		HistoricalContract: historical,
		PartyA:             partyA,
		PartyB:             partyB,
		Signature:          signature,
		Template:           template,
		Document:           document,
		Fields:             createdFields,
		PDFPath:            outPath,
		FontPath:           res.FontPath,
	}, nil
}

func fieldInput(entity, key, label, fieldType, note string) service.FieldInput {
	inLedger := true
	inTemplate := true
	return service.FieldInput{
		Entity:     entity,
		Key:        key,
		Label:      label,
		Type:       fieldType,
		InLedger:   &inLedger,
		InTemplate: &inTemplate,
		Note:       &note,
	}
}

func imageFieldInput(entity, key, label, note string) service.FieldInput {
	inLedger := false
	inTemplate := true
	return service.FieldInput{
		Entity:     entity,
		Key:        key,
		Label:      label,
		Type:       model.FieldTypeImage,
		InLedger:   &inLedger,
		InTemplate: &inTemplate,
		Note:       &note,
	}
}

func ensureField(ctx context.Context, svc *service.Service, in service.FieldInput) (model.FieldDefinition, error) {
	fields, err := svc.ListFields(ctx, in.Entity, true)
	if err != nil {
		return model.FieldDefinition{}, err
	}
	for _, field := range fields {
		if field.Key == in.Key {
			return field, nil
		}
	}
	return svc.CreateField(ctx, in)
}

func upsertParty(ctx context.Context, svc *service.Service, in service.PartyInput) (model.Party, error) {
	list, err := svc.ListParties(ctx, model.ListFilter{Limit: 50, Query: in.Name})
	if err != nil {
		return model.Party{}, err
	}
	for _, party := range list.Items {
		if party.Name == in.Name {
			in.ID = party.ID
			break
		}
	}
	party, _, err := svc.SaveParty(ctx, in)
	return party, err
}

func upsertContract(ctx context.Context, svc *service.Service, in service.ContractInput) (model.Contract, []string, error) {
	list, err := svc.ListContracts(ctx, model.ListFilter{Limit: 50, Query: in.ContractNo})
	if err != nil {
		return model.Contract{}, nil, err
	}
	for _, contract := range list.Items {
		if contract.ContractNo == in.ContractNo {
			in.ID = contract.ID
			break
		}
	}
	return svc.SaveContract(ctx, in)
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
