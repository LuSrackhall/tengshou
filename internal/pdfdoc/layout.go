package pdfdoc

import (
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io/fs"
	"math"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/tdewolff/canvas"
)

// Options configures the layout engine.
type Options struct {
	Font       *canvas.FontFamily
	FontSpec   FontSpec
	PageWidth  float64 // mm
	PageHeight float64 // mm
	MarginMM   float64
	AssetRoot  string
	Header     string // repeated on pages after the first
	ShowFooter bool
}

func (o Options) withDefaults() Options {
	if o.PageWidth <= 0 {
		o.PageWidth = 210
	}
	if o.PageHeight <= 0 {
		o.PageHeight = 297
	}
	if o.MarginMM <= 0 {
		o.MarginMM = 20
	}
	return o
}

// Document is a laid-out, paginated document ready for PDF rendering.
type Document struct {
	Pages     []*canvas.Canvas
	Font      FontSpec
	Missing   []rune
	Width     float64
	Height    float64
	PageCount int
}

type renderer struct {
	opts       Options
	faces      map[faceKey]*canvas.FontFace
	pages      []*canvas.Canvas
	ctx        *canvas.Context
	cursor     float64 // mm from content top
	hasContent bool
	assets     map[string]image.Image
}

type faceKey struct {
	size    int // 0.1pt
	bold    bool
	italic  bool
	r, g, b uint8
}

const mmPerPt = 25.4 / 72.0

// Render lays out blocks onto A4 pages.
func Render(blocks []Block, opts Options) (*Document, error) {
	opts = opts.withDefaults()
	if opts.Font == nil {
		return nil, fmt.Errorf("pdf: font family is required")
	}
	r := &renderer{
		opts:   opts,
		faces:  map[faceKey]*canvas.FontFace{},
		assets: map[string]image.Image{},
	}
	r.newPage()
	for _, block := range blocks {
		if err := r.renderBlock(block); err != nil {
			return nil, err
		}
	}
	if len(r.pages) == 0 {
		r.newPage()
	}
	r.drawPageDecorations()
	missing := r.collectMissing(blocks)
	doc := &Document{
		Pages:     r.pages,
		Font:      opts.FontSpec,
		Missing:   missing,
		Width:     opts.PageWidth,
		Height:    opts.PageHeight,
		PageCount: len(r.pages),
	}
	return doc, nil
}

func (r *renderer) collectMissing(blocks []Block) []rune {
	probe := r.faceFor(12, false, false, Black)
	seen := map[rune]bool{}
	out := []rune{}
	add := func(runs []Run) {
		for _, run := range runs {
			for _, ru := range run.Text {
				if seen[ru] || unicode.IsSpace(ru) {
					continue
				}
				seen[ru] = true
				if probe.Font.SFNT.GlyphIndex(ru) == 0 {
					out = append(out, ru)
				}
			}
		}
	}
	for _, block := range blocks {
		add(block.Runs)
		for _, item := range block.Items {
			add(item)
		}
		if block.Table != nil {
			for _, row := range block.Table.Rows {
				for _, cell := range row.Cells {
					add(cell.Runs)
				}
			}
		}
	}
	return out
}

func (r *renderer) newPage() {
	c := canvas.New(r.opts.PageWidth, r.opts.PageHeight)
	r.pages = append(r.pages, c)
	r.ctx = canvas.NewContext(c)
	r.cursor = 0
	r.hasContent = false
}

func (r *renderer) contentWidth() float64 {
	return r.opts.PageWidth - 2*r.opts.MarginMM
}

func (r *renderer) contentHeight() float64 {
	return r.opts.PageHeight - 2*r.opts.MarginMM - 6 // leave room for footer
}

func (r *renderer) ensureSpace(h float64) error {
	if r.cursor+h <= r.contentHeight() {
		return nil
	}
	if !r.hasContent {
		if h > r.contentHeight() {
			return fmt.Errorf("pdf: block of height %.1fmm does not fit on a page", h)
		}
		return nil
	}
	r.newPage()
	if h > r.contentHeight() {
		return fmt.Errorf("pdf: block of height %.1fmm does not fit on a page", h)
	}
	return nil
}

func (r *renderer) renderBlock(block Block) error {
	switch block.Type {
	case BlockPageBreak:
		if r.hasContent {
			r.newPage()
		}
		return nil
	case BlockParagraph, BlockHeading:
		before := block.Style.SpaceBefore
		if before > 0 && r.hasContent {
			r.cursor = math.Min(r.cursor+before, r.contentHeight())
		}
		after := block.Style.SpaceAfter
		if block.Type == BlockParagraph && after == 0 {
			after = 2
		}
		if err := r.renderTextBlock(block.Runs, block.Style, 0); err != nil {
			return err
		}
		r.cursor += after
		return nil
	case BlockList:
		for i, item := range block.Items {
			marker := "• "
			if block.Ordered {
				marker = fmt.Sprintf("%d. ", i+1)
			}
			runs := append([]Run{{Text: marker, Size: block.Style.FontSize, Color: &block.Style.Color}}, item...)
			if err := r.renderTextBlock(runs, block.Style, 6); err != nil {
				return err
			}
			r.cursor += 1
		}
		return nil
	case BlockTable:
		if block.Table == nil {
			return nil
		}
		return r.renderTable(block)
	case BlockImage:
		return r.renderImage(block)
	case BlockHorizontalRule:
		if err := r.ensureSpace(4); err != nil {
			return err
		}
		y := r.opts.PageHeight - r.opts.MarginMM - r.cursor - 2
		r.ctx.SetFill(nil)
		r.ctx.SetStrokeColor(color.RGBA{180, 180, 180, 255})
		r.ctx.SetStrokeWidth(0.25)
		var p canvas.Path
		p.MoveTo(0, 0)
		p.LineTo(r.contentWidth(), 0)
		r.ctx.DrawPath(r.opts.MarginMM, y, &p)
		r.cursor += 4
		r.hasContent = true
		return nil
	}
	return nil
}

// renderTextBlock wraps and draws runs, breaking across pages as needed.
func (r *renderer) renderTextBlock(runs []Run, style Style, indent float64) error {
	lines := r.wrap(runs, style, r.contentWidth(), indent)
	if len(lines) == 0 {
		return nil
	}
	for i, line := range lines {
		lineHeight, ascent := r.lineMetrics(line, style)
		if err := r.ensureSpace(lineHeight); err != nil {
			return err
		}
		x := r.opts.MarginMM
		width := r.contentWidth()
		if i == 0 && style.TextIndent > 0 && indent == 0 {
			x += style.TextIndent
			width -= style.TextIndent
		}
		x += indent
		width -= indent
		if style.Align == AlignCenter {
			x += (width - line.width) / 2
		} else if style.Align == AlignRight {
			x += width - line.width
		}
		baseline := r.opts.PageHeight - r.opts.MarginMM - r.cursor - ascent
		for _, item := range line.items {
			r.ctx.DrawText(x, baseline, canvas.NewTextLine(item.face, item.text, canvas.Left))
			x += item.face.TextWidth(item.text)
		}
		r.cursor += lineHeight
		r.hasContent = true
	}
	return nil
}

func (r *renderer) lineMetrics(line wrappedLine, style Style) (lineHeight, ascent float64) {
	lineHeight = style.FontSize * style.LineHeight * mmPerPt
	if style.LineHeight <= 0 {
		lineHeight = style.FontSize * 1.65 * mmPerPt
	}
	ascent = lineHeight * 0.78
	for _, item := range line.items {
		m := item.face.Metrics()
		if m.Ascent > ascent {
			ascent = m.Ascent
		}
		if m.LineHeight > lineHeight {
			lineHeight = m.LineHeight
		}
	}
	return lineHeight, ascent
}

func (r *renderer) faceFor(size float64, bold, italic bool, col Color) *canvas.FontFace {
	key := faceKey{
		size:   int(math.Round(size * 10)),
		bold:   bold,
		italic: italic,
		r:      col.R,
		g:      col.G,
		b:      col.B,
	}
	if face, ok := r.faces[key]; ok {
		return face
	}
	style := canvas.FontRegular
	if bold {
		style = canvas.FontBold
	}
	if italic {
		style |= canvas.FontItalic
	}
	face := r.opts.Font.Face(size, color.RGBA{R: col.R, G: col.G, B: col.B, A: 255}, style)
	r.faces[key] = face
	return face
}

// wrappedLine is one measured line of styled segments.
type wrappedLine struct {
	items []lineItem
	width float64
}

type lineItem struct {
	text string
	face *canvas.FontFace
}

// wrap converts runs into aligned-width lines.
func (r *renderer) wrap(runs []Run, style Style, availWidth, indent float64) []wrappedLine {
	tokens := r.tokenize(runs, style)
	var lines []wrappedLine
	cur := wrappedLine{}
	first := true
	limit := func() float64 {
		w := availWidth - indent
		if first && style.TextIndent > 0 && indent == 0 {
			w -= style.TextIndent
		}
		return w
	}
	flush := func() {
		// Trim trailing spaces.
		for len(cur.items) > 0 {
			last := &cur.items[len(cur.items)-1]
			trimmed := strings.TrimRightFunc(last.text, unicode.IsSpace)
			if trimmed == last.text {
				break
			}
			cur.width -= last.face.TextWidth(last.text) - last.face.TextWidth(trimmed)
			last.text = trimmed
			if last.text != "" {
				break
			}
			cur.items = cur.items[:len(cur.items)-1]
		}
		if len(cur.items) > 0 || len(lines) == 0 {
			lines = append(lines, cur)
			first = false
		}
		cur = wrappedLine{}
	}
	appendToken := func(t token) {
		if t.kind == tokSpace && len(cur.items) == 0 {
			return
		}
		if len(cur.items) > 0 {
			last := &cur.items[len(cur.items)-1]
			if last.face == t.face {
				last.text += t.text
				cur.width += t.width
				return
			}
		}
		cur.items = append(cur.items, lineItem{text: t.text, face: t.face})
		cur.width += t.width
	}
	for i, t := range tokens {
		switch t.kind {
		case tokBreak:
			flush()
			continue
		case tokSpace:
			if len(cur.items) == 0 {
				continue
			}
			// Drop spaces between two CJK characters.
			lastRune := lastRune(cur.items)
			nextRune := nextNonSpaceRune(tokens[i+1:])
			if isCJK(lastRune) && isCJK(nextRune) {
				continue
			}
			if cur.width+t.width > limit() {
				flush()
				continue
			}
			appendToken(t)
			continue
		}
		if t.noBreakAfter && i+1 < len(tokens) {
			// Opening punctuation must not end a line: attach the next token.
			appendToken(t)
			continue
		}
		if cur.width+t.width <= limit() || len(cur.items) == 0 && t.width <= limit() {
			appendToken(t)
			continue
		}
		if t.noBreakBefore && len(cur.items) > 0 {
			// Closing punctuation may hang slightly past the margin.
			appendToken(t)
			continue
		}
		if len(cur.items) > 0 {
			flush()
			if t.width <= limit() {
				appendToken(t)
				continue
			}
		}
		// A single token is wider than the line: hard split it.
		chunks := splitToWidth(t.face, t.text, limit())
		for _, chunk := range chunks {
			if cur.width+chunk.width > limit() && len(cur.items) > 0 {
				flush()
			}
			appendToken(chunk)
		}
	}
	if len(cur.items) > 0 {
		flush()
	}
	if len(lines) == 0 {
		return nil
	}
	return lines
}

func lastRune(items []lineItem) rune {
	if len(items) == 0 {
		return 0
	}
	rs := []rune(items[len(items)-1].text)
	if len(rs) == 0 {
		return 0
	}
	return rs[len(rs)-1]
}

func nextNonSpaceRune(tokens []token) rune {
	for _, t := range tokens {
		if t.kind == tokSpace || t.kind == tokBreak {
			continue
		}
		for _, r := range t.text {
			return r
		}
	}
	return 0
}

type tokenKind int

const (
	tokWord tokenKind = iota
	tokSpace
	tokCJK
	tokBreak
)

type token struct {
	text          string
	face          *canvas.FontFace
	kind          tokenKind
	width         float64
	noBreakBefore bool
	noBreakAfter  bool
}

func (r *renderer) tokenize(runs []Run, style Style) []token {
	var tokens []token
	for _, run := range runs {
		size := run.Size
		if size <= 0 {
			size = style.FontSize
		}
		col := style.Color
		if run.Color != nil {
			col = *run.Color
		}
		face := r.faceFor(size, run.Bold, run.Italic, col)
		if run.Break {
			tokens = append(tokens, token{kind: tokBreak, face: face})
			continue
		}
		var word strings.Builder
		flushWord := func() {
			if word.Len() == 0 {
				return
			}
			text := word.String()
			tokens = append(tokens, token{text: text, face: face, kind: tokWord, width: face.TextWidth(text)})
			word.Reset()
		}
		for _, r := range run.Text {
			switch {
			case unicode.IsSpace(r):
				flushWord()
				if len(tokens) > 0 && tokens[len(tokens)-1].kind == tokSpace {
					continue
				}
				tokens = append(tokens, token{text: " ", face: face, kind: tokSpace, width: face.TextWidth(" ")})
			case isCJK(r):
				flushWord()
				tokens = append(tokens, token{
					text:          string(r),
					face:          face,
					kind:          tokCJK,
					width:         face.TextWidth(string(r)),
					noBreakBefore: isClosingPunct(r),
					noBreakAfter:  isOpeningPunct(r),
				})
			default:
				word.WriteRune(r)
			}
		}
		flushWord()
	}
	return tokens
}

func splitToWidth(face *canvas.FontFace, text string, max float64) []token {
	var out []token
	var b strings.Builder
	width := 0.0
	flush := func() {
		if b.Len() == 0 {
			return
		}
		s := b.String()
		out = append(out, token{text: s, face: face, kind: tokWord, width: face.TextWidth(s)})
		b.Reset()
		width = 0
	}
	for _, r := range text {
		w := face.TextWidth(string(r))
		if width+w > max && b.Len() > 0 {
			flush()
		}
		b.WriteRune(r)
		width += w
	}
	flush()
	return out
}

func isCJK(r rune) bool {
	return unicode.Is(unicode.Han, r) ||
		unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) ||
		unicode.Is(unicode.Hangul, r) ||
		unicode.Is(unicode.Common, r) && isCJKPunct(r)
}

func isCJKPunct(r rune) bool {
	switch r {
	case '，', '。', '、', '；', '：', '！', '？', '（', '）', '《', '》', '「', '」', '『', '』', '【', '】', '…', '—', '·', '“', '”', '‘', '’', '％', '＃':
		return true
	}
	return false
}

func isClosingPunct(r rune) bool {
	switch r {
	case '，', '。', '、', '；', '：', '！', '？', '）', '》', '」', '』', '】', '…', '”', '’', '％', '.', ',', ';', ':', '!', '?', ')', ']', '}':
		return true
	}
	return false
}

func isOpeningPunct(r rune) bool {
	switch r {
	case '（', '《', '「', '『', '【', '“', '‘', '(', '[', '{':
		return true
	}
	return false
}

func (r *renderer) renderTable(block Block) error {
	rows := block.Table.Rows
	cols := 0
	for _, row := range rows {
		if len(row.Cells) > cols {
			cols = len(row.Cells)
		}
	}
	if cols == 0 {
		return nil
	}
	style := block.Style
	if style.FontSize <= 0 {
		style.FontSize = 10
	}
	colWidth := r.contentWidth() / float64(cols)
	const padding = 2.0
	lineHeight := style.FontSize * 1.45 * mmPerPt
	header := len(rows) > 0 && allHeader(rows[0])
	headerHeight := 0.0
	if header {
		headerHeight = r.measureRow(rows[0], cols, colWidth, padding, lineHeight, style)
	}
	for i, row := range rows {
		h := r.measureRow(row, cols, colWidth, padding, lineHeight, style)
		if r.cursor+h > r.contentHeight() && r.hasContent {
			r.newPage()
		}
		if header && i > 0 && !r.hasContent {
			// Repeat the header when a table continues on a new page.
			if headerHeight > r.contentHeight() {
				return fmt.Errorf("pdf: table header does not fit on a page")
			}
			r.drawRow(rows[0], cols, colWidth, padding, lineHeight, style, true)
			r.cursor += headerHeight
			r.hasContent = true
		}
		if r.cursor+h > r.contentHeight() {
			return fmt.Errorf("pdf: table row does not fit on a page")
		}
		r.drawRow(row, cols, colWidth, padding, lineHeight, style, header && i == 0)
		r.cursor += h
		r.hasContent = true
	}
	r.cursor += 2
	return nil
}

func allHeader(row Row) bool {
	if len(row.Cells) == 0 {
		return false
	}
	for _, c := range row.Cells {
		if !c.Header {
			return false
		}
	}
	return true
}

func (r *renderer) measureRow(row Row, cols int, colWidth, padding, lineHeight float64, style Style) float64 {
	maxLines := 1
	for i := 0; i < cols; i++ {
		if i >= len(row.Cells) {
			continue
		}
		lines := r.wrap(row.Cells[i].Runs, style, colWidth-2*padding, 0)
		if len(lines) > maxLines {
			maxLines = len(lines)
		}
	}
	return float64(maxLines)*lineHeight + 2*padding
}

func (r *renderer) drawRow(row Row, cols int, colWidth, padding, lineHeight float64, style Style, isHeader bool) {
	top := r.opts.PageHeight - r.opts.MarginMM - r.cursor
	h := r.measureRow(row, cols, colWidth, padding, lineHeight, style)
	bottom := top - h
	if isHeader {
		r.ctx.SetFillColor(color.RGBA{242, 242, 242, 255})
		r.ctx.SetStroke(nil)
		r.ctx.DrawPath(r.opts.MarginMM, bottom, canvas.Rectangle(r.contentWidth(), h))
	}
	r.ctx.SetFill(nil)
	r.ctx.SetStrokeColor(color.RGBA{150, 150, 150, 255})
	r.ctx.SetStrokeWidth(0.2)
	r.ctx.DrawPath(r.opts.MarginMM, bottom, canvas.Rectangle(r.contentWidth(), h))
	for c := 1; c < cols; c++ {
		var p canvas.Path
		p.MoveTo(0, 0)
		p.LineTo(0, h)
		r.ctx.DrawPath(r.opts.MarginMM+float64(c)*colWidth, bottom, &p)
	}
	for i := 0; i < cols; i++ {
		if i >= len(row.Cells) {
			continue
		}
		cell := row.Cells[i]
		lines := r.wrap(cell.Runs, style, colWidth-2*padding, 0)
		x := r.opts.MarginMM + float64(i)*colWidth + padding
		y := top - padding
		for _, line := range lines {
			y -= lineHeight
			baseline := y + lineHeight*0.78
			drawX := x
			switch cell.Align {
			case AlignCenter:
				drawX += (colWidth - 2*padding - line.width) / 2
			case AlignRight:
				drawX += colWidth - 2*padding - line.width
			}
			for _, item := range line.items {
				r.ctx.DrawText(drawX, baseline, canvas.NewTextLine(item.face, item.text, canvas.Left))
				drawX += item.face.TextWidth(item.text)
			}
		}
	}
}

func (r *renderer) renderImage(block Block) error {
	if block.Image == nil {
		return nil
	}
	img, err := r.loadImage(block.Image.Path)
	if err != nil {
		return err
	}
	bounds := img.Bounds()
	pxW, pxH := float64(bounds.Dx()), float64(bounds.Dy())
	if pxW <= 0 || pxH <= 0 {
		return fmt.Errorf("image %s has no pixels", block.Image.Path)
	}
	widthMM, heightMM := block.Image.WidthMM, block.Image.HeightMM
	switch {
	case widthMM > 0 && heightMM > 0:
	case widthMM > 0:
		heightMM = widthMM * pxH / pxW
	case heightMM > 0:
		widthMM = heightMM * pxW / pxH
	default:
		widthMM = pxW * 25.4 / 96
		heightMM = pxH * 25.4 / 96
	}
	maxW := r.contentWidth()
	if widthMM > maxW {
		heightMM *= maxW / widthMM
		widthMM = maxW
	}
	maxH := r.contentHeight()
	if heightMM > maxH {
		widthMM *= maxH / heightMM
		heightMM = maxH
	}
	if err := r.ensureSpace(heightMM + 3); err != nil {
		return err
	}
	x := r.opts.MarginMM
	switch block.Image.Align {
	case AlignCenter:
		x += (r.contentWidth() - widthMM) / 2
	case AlignRight:
		x += r.contentWidth() - widthMM
	}
	bottom := r.opts.PageHeight - r.opts.MarginMM - r.cursor - heightMM
	res := canvas.Resolution(pxW / widthMM)
	r.ctx.DrawImage(x, bottom, img, res)
	r.cursor += heightMM + 3
	r.hasContent = true
	return nil
}

func (r *renderer) loadImage(rel string) (image.Image, error) {
	rel = strings.TrimPrefix(rel, "./")
	if !fs.ValidPath(rel) {
		return nil, fmt.Errorf("image path %q is not a safe relative path", rel)
	}
	if img, ok := r.assets[rel]; ok {
		return img, nil
	}
	if r.opts.AssetRoot == "" {
		return nil, fmt.Errorf("image %q: asset root is not configured", rel)
	}
	full := filepath.Join(r.opts.AssetRoot, filepath.FromSlash(path.Clean(rel)))
	rootAbs, err := filepath.Abs(r.opts.AssetRoot)
	if err != nil {
		return nil, err
	}
	fullAbs, err := filepath.Abs(full)
	if err != nil {
		return nil, err
	}
	if fullAbs != rootAbs && !strings.HasPrefix(fullAbs, rootAbs+string(os.PathSeparator)) {
		return nil, fmt.Errorf("image %q escapes the asset root", rel)
	}
	f, err := os.Open(fullAbs)
	if err != nil {
		return nil, fmt.Errorf("open image %q: %w", rel, err)
	}
	defer f.Close()
	img, format, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("decode image %q (%s): %w", rel, format, err)
	}
	r.assets[rel] = img
	return img, nil
}

// drawPageDecorations stamps headers and page numbers on every page.
func (r *renderer) drawPageDecorations() {
	total := len(r.pages)
	for i, page := range r.pages {
		ctx := canvas.NewContext(page)
		if r.opts.Header != "" && i > 0 {
			face := r.faceFor(8, false, false, Color{120, 120, 120})
			ctx.DrawText(r.opts.MarginMM, r.opts.PageHeight-r.opts.MarginMM/2, canvas.NewTextLine(face, r.opts.Header, canvas.Left))
		}
		if r.opts.ShowFooter {
			face := r.faceFor(9, false, false, Color{90, 90, 90})
			label := fmt.Sprintf("第 %d 页 / 共 %d 页", i+1, total)
			width := face.TextWidth(label)
			ctx.DrawText((r.opts.PageWidth-width)/2, r.opts.MarginMM/2, canvas.NewTextLine(face, label, canvas.Left))
		}
	}
}
