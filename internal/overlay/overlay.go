// Package overlay fills an imported PDF template with contract data.
//
// The template PDF (converted from the user's Word document) is never edited
// directly. Instead Build renders a transparent overlay PDF with the same page
// sizes, containing text at calibrated boxes plus signature and seal images;
// Compose then applies that overlay to every template page with pdfcpu. The
// result keeps the Word layout untouched (same fonts, page breaks and images)
// while adding only the extracted contract values.
package overlay

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"math"
	"os"
	"strings"
	"unicode"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	pdfmodel "github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/tdewolff/canvas"
	"github.com/tdewolff/canvas/renderers/pdf"

	"github.com/LuSrackhall/tengshou/internal/model"
)

// mmPerPt converts PDF points (1/72 inch) to canvas millimetres.
const mmPerPt = 25.4 / 72.0

// DefaultFontSize is the preferred text size when a placement does not set
// one. It matches the body size used by the HTML layout engine.
const DefaultFontSize = 10.5

// minFontSize is the smallest text size auto-shrink will use before reporting
// an overflow.
const minFontSize = 4.0

// PageSize is the visible page size in PDF points: rotation is already applied,
// so it is the size the editor shows and the user draws boxes on.
type PageSize struct {
	Width  float64
	Height float64
}

// PageSizes reads the per-page visible dimensions of a PDF file.
func PageSizes(pdfPath string) ([]PageSize, error) {
	ctx, err := api.ReadContextFile(pdfPath)
	if err != nil {
		return nil, fmt.Errorf("读取 PDF 失败：%w", err)
	}
	if err := ctx.EnsurePageCount(); err != nil {
		return nil, fmt.Errorf("读取 PDF 页数失败：%w", err)
	}
	if ctx.PageCount == 0 {
		return nil, errors.New("PDF 没有页面")
	}
	out := make([]PageSize, 0, ctx.PageCount)
	for i := 1; i <= ctx.PageCount; i++ {
		_, _, attrs, err := ctx.PageDict(i, false)
		if err != nil {
			return nil, fmt.Errorf("读取第 %d 页尺寸失败：%w", i, err)
		}
		rect := attrs.MediaBox
		if attrs.CropBox != nil {
			rect = attrs.CropBox
		}
		if rect == nil || rect.Width() <= 0 || rect.Height() <= 0 {
			return nil, fmt.Errorf("第 %d 页尺寸无效", i)
		}
		w, h := rect.Width(), rect.Height()
		switch attrs.Rotate {
		case 90, -90, 270, -270:
			w, h = h, w
		}
		out = append(out, PageSize{Width: w, Height: h})
	}
	return out, nil
}

// Resolver supplies the values and images referenced by placements.
type Resolver interface {
	// Text returns the display value for a field placement. The bool is false
	// when the field holds no value so the builder can warn about it.
	Text(p model.Placement) (string, bool)
	// Image returns the picture for a signature or seal placement. A nil image
	// with a nil error means the asset is not available yet (for example no
	// signature was collected), which is reported as a warning.
	Image(p model.Placement) (image.Image, bool, error)
}

// Options configures one overlay build.
type Options struct {
	// Pages are the template page sizes, in order.
	Pages []PageSize
	// Placements are the calibrated boxes, already validated.
	Placements []model.Placement
	// Font is a CJK-capable family (pdfdoc.LoadFont).
	Font *canvas.FontFamily
	// Resolver provides field values and images.
	Resolver Resolver
	// Placeholder draws a light box where a signature or seal is missing.
	// Preview uses it; final generation leaves the gap empty.
	Placeholder bool
	// MaskFieldValues fills each field box with white before the value is
	// drawn, covering 【…】 placeholders left in the template artwork. Off by
	// default so existing templates render exactly as before.
	MaskFieldValues bool
}

// Warning describes a non-fatal issue found while filling a placement.
type Warning struct {
	Code        string `json:"code"`
	PlacementID string `json:"placement_id,omitempty"`
	Message     string `json:"message"`
}

// Result is the built overlay PDF plus warnings.
type Result struct {
	PDF      []byte
	Warnings []Warning
}

// Build renders the transparent overlay PDF for the given template pages.
func Build(opts Options) (Result, error) {
	if opts.Font == nil {
		return Result{}, errors.New("缺少中文字体，无法生成套印内容")
	}
	if opts.Resolver == nil {
		return Result{}, errors.New("缺少数据解析器")
	}
	if len(opts.Pages) == 0 {
		return Result{}, errors.New("模板没有页面")
	}
	byPage := make(map[int][]model.Placement, len(opts.Pages))
	for _, p := range opts.Placements {
		if p.Page < 1 || p.Page > len(opts.Pages) {
			return Result{}, fmt.Errorf("标定位置 %s 的页码 %d 超出模板页数 %d", p.ID, p.Page, len(opts.Pages))
		}
		byPage[p.Page] = append(byPage[p.Page], p)
	}

	var buf bytes.Buffer
	first := opts.Pages[0]
	r := pdf.New(&buf, first.Width*mmPerPt, first.Height*mmPerPt,
		&pdf.Options{Compress: true, SubsetFonts: true})
	r.SetInfo("腾手套印内容", "", "合同套印", "腾手", "腾手")
	res := Result{Warnings: []Warning{}}
	for i, size := range opts.Pages {
		if i > 0 {
			r.NewPage(size.Width*mmPerPt, size.Height*mmPerPt)
		}
		warnings := renderPage(r, size, byPage[i+1], opts)
		res.Warnings = append(res.Warnings, warnings...)
	}
	if err := r.Close(); err != nil {
		return Result{}, fmt.Errorf("生成套印 PDF 失败：%w", err)
	}
	res.PDF = buf.Bytes()
	return res, nil
}

func renderPage(r *pdf.PDF, size PageSize, placements []model.Placement, opts Options) []Warning {
	if len(placements) == 0 {
		// Even an empty page must exist: pdfcpu maps source page n to
		// destination page n for a multi-page overlay.
		return nil
	}
	warnings := []Warning{}
	wMM := size.Width * mmPerPt
	hMM := size.Height * mmPerPt
	c := canvas.New(wMM, hMM)
	ctx := canvas.NewContext(c)
	for _, p := range placements {
		switch p.Type {
		case model.PlacementField:
			warnings = append(warnings, drawText(ctx, size, p, opts)...)
		case model.PlacementSignature, model.PlacementSeal:
			warnings = append(warnings, drawImage(ctx, size, p, opts)...)
		default:
			warnings = append(warnings, Warning{
				Code: "unknown_binding", PlacementID: p.ID,
				Message: fmt.Sprintf("位置 %s 的绑定类型 %q 不受支持", p.ID, p.Type),
			})
		}
	}
	c.RenderTo(r)
	return warnings
}

// boxMM converts a placement's top-left, point-based box into canvas
// millimetres with a bottom-left origin.
func boxMM(size PageSize, p model.Placement) (x, y, w, h float64) {
	x = p.X * mmPerPt
	w = p.W * mmPerPt
	h = p.H * mmPerPt
	y = (size.Height - p.Y - p.H) * mmPerPt
	return x, y, w, h
}

func drawText(ctx *canvas.Context, size PageSize, p model.Placement, opts Options) []Warning {
	value, ok := opts.Resolver.Text(p)
	value = strings.TrimSpace(value)
	if !ok || value == "" {
		return []Warning{{
			Code: "missing_value", PlacementID: p.ID,
			Message: fmt.Sprintf("位置 %s 绑定的字段没有值（%s），已跳过", p.ID, p.Field),
		}}
	}
	x, y, w, h := boxMM(size, p)
	if w <= 0 || h <= 0 {
		return []Warning{{Code: "invalid_box", PlacementID: p.ID, Message: "文本框尺寸无效"}}
	}
	base := p.FontSize
	if base <= 0 {
		base = DefaultFontSize
	}
	if base > 72 {
		base = 72
	}
	if base < minFontSize {
		base = minFontSize
	}

	var face *canvas.FontFace
	var lines []string
	var lineHeight float64
	used := base
	for used = base; used >= minFontSize; used -= 0.5 {
		face = opts.Font.Face(used, color.RGBA{R: 0, G: 0, B: 0, A: 255}, canvas.FontRegular)
		if face == nil {
			return []Warning{{Code: "font_error", PlacementID: p.ID, Message: "字体加载失败"}}
		}
		lines = wrapText(face, value, w)
		lineHeight = used * 1.32 * mmPerPt
		if float64(len(lines))*lineHeight <= h+0.01 {
			break
		}
		if used-0.5 < minFontSize {
			// Use the smallest size and report the overflow below.
			face = opts.Font.Face(minFontSize, color.RGBA{R: 0, G: 0, B: 0, A: 255}, canvas.FontRegular)
			lines = wrapText(face, value, w)
			lineHeight = minFontSize * 1.32 * mmPerPt
			used = minFontSize
			break
		}
	}

	warnings := []Warning{}
	if float64(len(lines))*lineHeight > h+0.01 {
		warnings = append(warnings, Warning{
			Code: "text_overflow", PlacementID: p.ID,
			Message: fmt.Sprintf("位置 %s 的文字超出文本框（已缩到 %.1fpt 仍放不下，请加大框或缩短内容）", p.ID, minFontSize),
		})
	}
	if face == nil {
		return warnings
	}
	ascent := lineHeight * 0.80
	if m := face.Metrics(); m.Ascent > 0 {
		ascent = m.Ascent
	}
	if ascent > lineHeight || ascent <= 0 {
		ascent = lineHeight * 0.80
	}
	// Draw top-down inside the box; the first baseline sits one ascent below
	// the box top.
	baseline := y + h - ascent
	if opts.MaskFieldValues {
		// 白底遮盖：先铺满标定框，再写字，用来盖住模板里保留的【…】占位符。
		ctx.SetFillColor(color.RGBA{R: 255, G: 255, B: 255, A: 255})
		ctx.DrawPath(x, y, canvas.Rectangle(w, h))
		ctx.SetFill(nil)
	}
	align := strings.TrimSpace(strings.ToLower(p.Align))
	for i, line := range lines {
		if float64(i)*lineHeight > h+0.01 {
			break
		}
		drawX := x
		switch align {
		case "center":
			drawX += (w - face.TextWidth(line)) / 2
		case "right":
			drawX += w - face.TextWidth(line)
		}
		ctx.DrawText(drawX, baseline-float64(i)*lineHeight, canvas.NewTextLine(face, line, canvas.Left))
	}
	return warnings
}

func drawImage(ctx *canvas.Context, size PageSize, p model.Placement, opts Options) []Warning {
	img, ok, err := opts.Resolver.Image(p)
	kind := "签名"
	if p.Type == model.PlacementSeal {
		kind = "公章"
	}
	if err != nil {
		return []Warning{{
			Code: "image_load_failed", PlacementID: p.ID,
			Message: fmt.Sprintf("读取%s图片失败：%s", kind, err),
		}}
	}
	x, y, w, h := boxMM(size, p)
	if w <= 0 || h <= 0 {
		return []Warning{{Code: "invalid_box", PlacementID: p.ID, Message: kind + "框尺寸无效"}}
	}
	if !ok || img == nil {
		msg := fmt.Sprintf("位置 %s 的%s图片尚未提供，已跳过", p.ID, kind)
		if opts.Placeholder {
			drawPlaceholder(ctx, x, y, w, h, "（"+kind+"）", opts)
			msg = fmt.Sprintf("位置 %s 的%s图片尚未提供，预览中以占位框显示", p.ID, kind)
		}
		return []Warning{{Code: "missing_image", PlacementID: p.ID, Message: msg}}
	}
	bounds := img.Bounds()
	pxW, pxH := float64(bounds.Dx()), float64(bounds.Dy())
	if pxW <= 0 || pxH <= 0 {
		return []Warning{{Code: "image_empty", PlacementID: p.ID, Message: kind + "图片没有像素"}}
	}
	aspect := pxW / pxH
	iw, ih := w, w/aspect
	if ih > h {
		ih = h
		iw = h * aspect
	}
	ctx.DrawImage(x+(w-iw)/2, y+(h-ih)/2, img, canvas.Resolution(pxW/iw))
	return nil
}

func drawPlaceholder(ctx *canvas.Context, x, y, w, h float64, label string, opts Options) {
	gray := color.RGBA{R: 150, G: 150, B: 150, A: 255}
	ctx.SetFill(nil)
	ctx.SetStrokeColor(gray)
	ctx.SetStrokeWidth(0.3)
	ctx.DrawPath(x, y, canvas.Rectangle(w, h))
	face := opts.Font.Face(8, gray, canvas.FontRegular)
	if face == nil {
		return
	}
	width := face.TextWidth(label)
	ctx.DrawText(x+(w-width)/2, y+h/2-face.Metrics().Ascent/2, canvas.NewTextLine(face, label, canvas.Left))
}

// wrapText breaks value into lines that fit maxWidth (millimetres) at the
// given face. CJK characters may break anywhere; Latin text breaks on spaces.
func wrapText(face *canvas.FontFace, value string, maxWidth float64) []string {
	if maxWidth <= 0 {
		return []string{value}
	}
	var lines []string
	for _, para := range strings.Split(strings.ReplaceAll(value, "\r\n", "\n"), "\n") {
		lines = append(lines, wrapParagraph(face, para, maxWidth)...)
	}
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}

type textToken struct {
	text          string
	width         float64
	space         bool
	noBreakBefore bool
}

func wrapParagraph(face *canvas.FontFace, para string, maxWidth float64) []string {
	if strings.TrimSpace(para) == "" {
		return []string{""}
	}
	tokens := tokenizeLine(face, para)
	var lines []string
	var cur strings.Builder
	curWidth := 0.0
	flush := func() {
		line := strings.TrimRightFunc(cur.String(), unicode.IsSpace)
		lines = append(lines, line)
		cur.Reset()
		curWidth = 0
	}
	appendToken := func(t textToken) {
		cur.WriteString(t.text)
		curWidth += t.width
	}
	for _, t := range tokens {
		if t.space {
			if curWidth == 0 {
				continue
			}
			if curWidth+t.width > maxWidth {
				flush()
				continue
			}
			appendToken(t)
			continue
		}
		if curWidth+t.width <= maxWidth || curWidth == 0 {
			if curWidth == 0 && t.width > maxWidth {
				for _, chunk := range splitToken(face, t.text, maxWidth) {
					lines = append(lines, chunk)
				}
				continue
			}
			appendToken(t)
			continue
		}
		if t.noBreakBefore {
			// Closing punctuation may hang slightly past the box edge.
			appendToken(t)
			continue
		}
		flush()
		if t.width > maxWidth {
			for _, chunk := range splitToken(face, t.text, maxWidth) {
				lines = append(lines, chunk)
			}
			continue
		}
		appendToken(t)
	}
	if curWidth > 0 {
		flush()
	}
	if len(lines) == 0 {
		lines = []string{""}
	}
	return lines
}

func tokenizeLine(face *canvas.FontFace, s string) []textToken {
	var out []textToken
	var word strings.Builder
	flushWord := func() {
		if word.Len() == 0 {
			return
		}
		text := word.String()
		out = append(out, textToken{text: text, width: face.TextWidth(text)})
		word.Reset()
	}
	for _, runeValue := range s {
		switch {
		case unicode.IsSpace(runeValue):
			flushWord()
			if len(out) > 0 && out[len(out)-1].space {
				continue
			}
			out = append(out, textToken{text: " ", width: face.TextWidth(" "), space: true})
		case isCJK(runeValue):
			flushWord()
			out = append(out, textToken{
				text:          string(runeValue),
				width:         face.TextWidth(string(runeValue)),
				noBreakBefore: isClosingPunct(runeValue),
			})
		default:
			word.WriteRune(runeValue)
		}
	}
	flushWord()
	return out
}

// splitToken hard-splits one token that is wider than the line.
func splitToken(face *canvas.FontFace, text string, maxWidth float64) []string {
	var out []string
	var cur strings.Builder
	width := 0.0
	for _, r := range text {
		w := face.TextWidth(string(r))
		if width+w > maxWidth && cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
			width = 0
		}
		cur.WriteRune(r)
		width += w
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	if len(out) == 0 {
		out = []string{text}
	}
	return out
}

func isCJK(r rune) bool {
	return unicode.Is(unicode.Han, r) ||
		unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) ||
		unicode.Is(unicode.Hangul, r)
}

func isClosingPunct(r rune) bool {
	switch r {
	case '，', '。', '、', '；', '：', '！', '？', '）', '》', '」', '』', '】', '…', '”', '’',
		'.', ',', ';', ':', '!', '?', ')', ']', '}':
		return true
	}
	return false
}

// Compose stamps the overlay onto the template and returns the composed PDF.
func Compose(templatePath string, overlayPDF []byte) ([]byte, error) {
	if len(overlayPDF) == 0 {
		return nil, errors.New("套印内容为空")
	}
	wm, err := api.PDFMultiWatermarkForReadSeeker(
		bytes.NewReader(overlayPDF), 1, 1, "", true, false, types.POINTS)
	if err != nil {
		return nil, fmt.Errorf("准备套印失败：%w", err)
	}
	// Fill the destination page exactly: the overlay pages use the same size
	// and orientation as the template pages, anchored at the bottom-left.
	wm.ScaleAbs = false
	wm.Scale = 1
	wm.Pos = types.BottomLeft
	wm.Dx = 0
	wm.Dy = 0
	// pdfcpu applies a 45° diagonal rotation to watermarks by default. The
	// overlay must be stamped upright and unrotated.
	wm.Rotation = 0
	wm.Diagonal = pdfmodel.NoDiagonal
	wm.UserRotOrDiagonal = true
	wm.Opacity = 1

	conf := pdfmodel.NewDefaultConfiguration()
	conf.Cmd = pdfmodel.ADDWATERMARKS

	in, err := os.Open(templatePath)
	if err != nil {
		return nil, fmt.Errorf("打开模板 PDF 失败：%w", err)
	}
	defer in.Close()

	var buf bytes.Buffer
	if err := api.AddWatermarks(in, &buf, nil, wm, conf); err != nil {
		return nil, fmt.Errorf("套印生成 PDF 失败：%w", err)
	}
	return buf.Bytes(), nil
}

// RoundToPt rounds a coordinate to 0.1 pt, keeping stored mappings small.
func RoundToPt(v float64) float64 {
	return math.Round(v*10) / 10
}
