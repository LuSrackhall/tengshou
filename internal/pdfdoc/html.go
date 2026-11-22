package pdfdoc

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/net/html"
)

// Align is horizontal text alignment.
type Align int

// Alignment values.
const (
	AlignLeft Align = iota
	AlignCenter
	AlignRight
)

// Color is an RGB text colour.
type Color struct {
	R, G, B uint8
}

// Black is the default text colour.
var Black = Color{0, 0, 0}

// Style is the resolved style of a block element.
type Style struct {
	FontSize        float64 // points
	LineHeight      float64 // multiplier of font size
	Align           Align
	Color           Color
	Bold            bool    // inherited inline boldness
	Italic          bool    // inherited inline italics
	SpaceBefore     float64 // mm
	SpaceAfter      float64 // mm
	TextIndent      float64 // mm, first line
	PageBreakBefore bool
}

// Run is a styled span of inline text.
type Run struct {
	Text   string
	Bold   bool
	Italic bool
	Size   float64 // points; 0 = inherit block style
	Color  *Color
	Break  bool // forced line break
}

// BlockType enumerates supported block elements.
type BlockType int

// Block types.
const (
	BlockParagraph BlockType = iota
	BlockHeading
	BlockList
	BlockTable
	BlockImage
	BlockHorizontalRule
	BlockPageBreak
)

// Block is a parsed block-level element.
type Block struct {
	Type    BlockType
	Level   int // heading level 1..6
	Style   Style
	Runs    []Run
	Items   [][]Run
	Ordered bool
	Table   *Table
	Image   *Image
}

// Cell is one table cell.
type Cell struct {
	Runs   []Run
	Align  Align
	Header bool
}

// Row is one table row.
type Row struct {
	Cells []Cell
}

// Table is a parsed table.
type Table struct {
	Rows []Row
}

// Image is a block image.
type Image struct {
	Path     string // relative to the asset root
	WidthMM  float64
	HeightMM float64
	Align    Align
}

// ParserOptions supplies document defaults.
type ParserOptions struct {
	FontSize   float64 // points, default 11
	LineHeight float64 // multiplier, default 1.65
	Color      Color
}

func (o ParserOptions) withDefaults() ParserOptions {
	if o.FontSize <= 0 {
		o.FontSize = 11
	}
	if o.LineHeight <= 0 {
		o.LineHeight = 1.65
	}
	return o
}

// ParseHTML parses rendered HTML into the supported block subset.
//
// Supported: h1-h6, p, div/section/article containers, br, hr, ul/ol/li,
// table/thead/tbody/tr/th/td, img, and inline b/strong/i/em/span/a/u with
// inline style declarations for font-size, color, text-align, margins,
// line-height, font-weight, font-style, text-indent and page-break-before.
// CSS stylesheets and inline images inside paragraphs are not supported.
func ParseHTML(r io.Reader, opts ParserOptions) ([]Block, error) {
	root, err := html.Parse(r)
	if err != nil {
		return nil, fmt.Errorf("parse html: %w", err)
	}
	p := &parser{opts: opts.withDefaults()}
	base := Style{
		FontSize:   p.opts.FontSize,
		LineHeight: p.opts.LineHeight,
		Color:      p.opts.Color,
	}
	var blocks []Block
	if err := p.walk(root, base, &blocks); err != nil {
		return nil, err
	}
	return blocks, nil
}

type parser struct {
	opts ParserOptions
}

func (p *parser) walk(n *html.Node, style Style, blocks *[]Block) error {
	if n.Type == html.TextNode {
		if strings.TrimSpace(n.Data) != "" {
			runs, err := p.collectInline(n, style)
			if err != nil {
				return err
			}
			runs = trimRuns(runs)
			if len(runs) > 0 {
				*blocks = append(*blocks, Block{Type: BlockParagraph, Style: style, Runs: runs})
			}
		}
		return nil
	}
	if n.Type != html.ElementNode {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if err := p.walk(c, style, blocks); err != nil {
				return err
			}
		}
		return nil
	}

	switch n.Data {
	case "head", "script", "style", "title", "meta", "link":
		return nil
	case "html", "body", "div", "section", "article", "main", "header", "footer", "center":
		s := applyNodeStyle(n, style)
		if n.Data == "center" {
			s.Align = AlignCenter
		}
		if isPageBreak(n) {
			*blocks = append(*blocks, Block{Type: BlockPageBreak})
		}
		if s.PageBreakBefore && hasContent(n) {
			*blocks = append(*blocks, Block{Type: BlockPageBreak})
		}
		s.PageBreakBefore = false
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if err := p.walk(c, s, blocks); err != nil {
				return err
			}
		}
		return nil
	case "h1", "h2", "h3", "h4", "h5", "h6":
		s := headingStyle(n.Data, style)
		s = applyNodeStyle(n, s)
		runs, err := p.collectInline(n, s)
		if err != nil {
			return err
		}
		runs = trimRuns(runs)
		if len(runs) == 0 {
			return nil
		}
		*blocks = append(*blocks, Block{Type: BlockHeading, Level: int(n.Data[1] - '0'), Style: s, Runs: runs})
		return nil
	case "p":
		s := applyNodeStyle(n, style)
		if isPageBreak(n) {
			*blocks = append(*blocks, Block{Type: BlockPageBreak})
			return nil
		}
		img, err := p.onlyImage(n)
		if err != nil {
			return err
		}
		if img != nil {
			img.Align = s.Align
			*blocks = append(*blocks, Block{Type: BlockImage, Style: s, Image: img})
			return nil
		}
		runs, err := p.collectInline(n, s)
		if err != nil {
			return err
		}
		runs = trimRuns(runs)
		if len(runs) == 0 {
			return nil
		}
		*blocks = append(*blocks, Block{Type: BlockParagraph, Style: s, Runs: runs})
		return nil
	case "br":
		return nil
	case "hr":
		*blocks = append(*blocks, Block{Type: BlockHorizontalRule, Style: applyNodeStyle(n, style)})
		return nil
	case "ul", "ol":
		s := applyNodeStyle(n, style)
		block := Block{Type: BlockList, Style: s, Ordered: n.Data == "ol"}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.ElementNode || c.Data != "li" {
				continue
			}
			runs, err := p.collectInline(c, s)
			if err != nil {
				return err
			}
			runs = trimRuns(runs)
			if len(runs) > 0 {
				block.Items = append(block.Items, runs)
			}
		}
		if len(block.Items) > 0 {
			*blocks = append(*blocks, block)
		}
		return nil
	case "table":
		t, err := p.parseTable(n, style)
		if err != nil {
			return err
		}
		if t != nil {
			*blocks = append(*blocks, Block{Type: BlockTable, Style: applyNodeStyle(n, style), Table: t})
		}
		return nil
	case "img":
		img, err := p.imageFromNode(n, applyNodeStyle(n, style))
		if err != nil {
			return err
		}
		*blocks = append(*blocks, Block{Type: BlockImage, Style: applyNodeStyle(n, style), Image: img})
		return nil
	case "thead", "tbody", "tfoot", "tr", "td", "th":
		// Tables are handled as a whole; stray table parts are ignored.
		return nil
	default:
		// Unknown block element: treat as a transparent container.
		s := applyNodeStyle(n, style)
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if err := p.walk(c, s, blocks); err != nil {
				return err
			}
		}
		return nil
	}
}

func headingStyle(tag string, base Style) Style {
	s := base
	switch tag {
	case "h1":
		s.FontSize, s.SpaceBefore, s.SpaceAfter = 20, 6, 4
		s.Bold = true
		s.Align = AlignCenter
	case "h2":
		s.FontSize, s.SpaceBefore, s.SpaceAfter = 16, 5, 3
		s.Bold = true
	case "h3":
		s.FontSize, s.SpaceBefore, s.SpaceAfter = 14, 4, 2.5
		s.Bold = true
	case "h4":
		s.FontSize, s.SpaceBefore, s.SpaceAfter = 12, 3, 2
		s.Bold = true
	case "h5":
		s.FontSize, s.SpaceBefore, s.SpaceAfter = 11, 3, 2
		s.Bold = true
	default:
		s.FontSize, s.SpaceBefore, s.SpaceAfter = 11, 2, 2
		s.Bold = true
	}
	return s
}

func hasContent(n *html.Node) bool {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.TextNode && strings.TrimSpace(c.Data) != "" {
			return true
		}
		if c.Type == html.ElementNode && c.Data != "br" {
			if hasContent(c) {
				return true
			}
		}
	}
	return false
}

func isPageBreak(n *html.Node) bool {
	for _, a := range n.Attr {
		if a.Key != "class" {
			continue
		}
		for _, class := range strings.Fields(a.Val) {
			if class == "page-break" || class == "pagebreak" {
				return true
			}
		}
	}
	return false
}

// onlyImage returns an image if the paragraph contains exactly one image and
// no other visible content.
func (p *parser) onlyImage(n *html.Node) (*Image, error) {
	var img *html.Node
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		switch c.Type {
		case html.TextNode:
			if strings.TrimSpace(c.Data) != "" {
				return nil, nil
			}
		case html.ElementNode:
			if c.Data == "img" && img == nil {
				img = c
			} else if c.Data != "br" {
				return nil, nil
			}
		}
	}
	if img == nil {
		return nil, nil
	}
	return p.imageFromNode(img, Style{})
}

func (p *parser) imageFromNode(n *html.Node, style Style) (*Image, error) {
	var src string
	for _, a := range n.Attr {
		if a.Key == "src" {
			src = strings.TrimSpace(a.Val)
		}
	}
	if src == "" {
		return nil, fmt.Errorf("img element is missing src")
	}
	if strings.Contains(src, "://") || strings.HasPrefix(src, "//") || strings.HasPrefix(src, "data:") {
		return nil, fmt.Errorf("image %q: only local relative paths are supported", src)
	}
	if strings.HasPrefix(src, "/") {
		return nil, fmt.Errorf("image %q: absolute paths are not allowed", src)
	}
	img := &Image{Path: src, Align: style.Align}
	decls := declMap(n)
	if v, ok := decls["width"]; ok {
		if mm, ok := parseLengthMM(v, style.FontSize); ok {
			img.WidthMM = mm
		}
	}
	if v, ok := decls["height"]; ok {
		if mm, ok := parseLengthMM(v, style.FontSize); ok {
			img.HeightMM = mm
		}
	}
	return img, nil
}

func (p *parser) parseTable(n *html.Node, base Style) (*Table, error) {
	style := applyNodeStyle(n, base)
	if style.FontSize == base.FontSize {
		style.FontSize = 10
	}
	table := &Table{}
	var walkRows func(*html.Node) error
	walkRows = func(parent *html.Node) error {
		for c := parent.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.ElementNode {
				continue
			}
			switch c.Data {
			case "thead", "tbody", "tfoot":
				if err := walkRows(c); err != nil {
					return err
				}
			case "tr":
				row := Row{}
				for cell := c.FirstChild; cell != nil; cell = cell.NextSibling {
					if cell.Type != html.ElementNode || (cell.Data != "td" && cell.Data != "th") {
						continue
					}
					runs, err := p.collectInline(cell, style)
					if err != nil {
						return err
					}
					if cell.Data == "th" {
						for i := range runs {
							runs[i].Bold = true
						}
					}
					cellStyle := applyNodeStyle(cell, style)
					row.Cells = append(row.Cells, Cell{
						Runs:   trimRuns(runs),
						Align:  cellStyle.Align,
						Header: cell.Data == "th",
					})
				}
				if len(row.Cells) > 0 {
					table.Rows = append(table.Rows, row)
				}
			}
		}
		return nil
	}
	if err := walkRows(n); err != nil {
		return nil, err
	}
	if len(table.Rows) == 0 {
		return nil, nil
	}
	return table, nil
}

func (p *parser) collectInline(n *html.Node, style Style) ([]Run, error) {
	var runs []Run
	var walk func(*html.Node, Style) error
	walk = func(node *html.Node, current Style) error {
		switch node.Type {
		case html.TextNode:
			text := normalizeWhitespace(node.Data)
			if text == "" {
				return nil
			}
			run := Run{Text: text, Bold: current.Bold, Italic: current.Italic, Size: current.FontSize, Color: &current.Color}
			runs = append(runs, run)
			return nil
		case html.ElementNode:
		default:
			for c := node.FirstChild; c != nil; c = c.NextSibling {
				if err := walk(c, current); err != nil {
					return err
				}
			}
			return nil
		}
		switch node.Data {
		case "script", "style", "head", "title", "meta", "link":
			return nil
		case "br":
			runs = append(runs, Run{Break: true})
			return nil
		case "img":
			return fmt.Errorf("inline images are not supported; place <img> in its own <p> or directly in the document body")
		case "strong", "b":
			current.Bold = true
		case "em", "i":
			current.Italic = true
		case "small":
			current.FontSize *= 0.85
		case "big":
			current.FontSize *= 1.2
		}
		current = applyNodeStyle(node, current)
		decls := declMap(node)
		if v, ok := decls["font-weight"]; ok {
			switch strings.ToLower(strings.TrimSpace(v)) {
			case "bold", "600", "700", "800", "900":
				current.Bold = true
			case "normal", "400":
				current.Bold = false
			}
		}
		if v, ok := decls["font-style"]; ok {
			switch strings.ToLower(strings.TrimSpace(v)) {
			case "italic", "oblique":
				current.Italic = true
			case "normal":
				current.Italic = false
			}
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			if err := walk(c, current); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(n, style); err != nil {
		return nil, err
	}
	return runs, nil
}

func normalizeWhitespace(s string) string {
	var b strings.Builder
	space := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			if !space {
				b.WriteByte(' ')
				space = true
			}
			continue
		}
		space = false
		b.WriteRune(r)
	}
	return b.String()
}

func trimRuns(runs []Run) []Run {
	for len(runs) > 0 && runs[0].Break {
		runs = runs[1:]
	}
	for len(runs) > 0 && runs[len(runs)-1].Break {
		runs = runs[:len(runs)-1]
	}
	for i := range runs {
		if runs[i].Break {
			continue
		}
		if i == 0 {
			runs[i].Text = strings.TrimLeftFunc(runs[i].Text, unicode.IsSpace)
		}
		if i == len(runs)-1 {
			runs[i].Text = strings.TrimRightFunc(runs[i].Text, unicode.IsSpace)
		}
	}
	out := runs[:0]
	for _, r := range runs {
		if r.Text == "" && !r.Break {
			continue
		}
		out = append(out, r)
	}
	return out
}

func applyNodeStyle(n *html.Node, base Style) Style {
	s := base
	for _, a := range n.Attr {
		switch a.Key {
		case "align":
			if al, ok := parseAlign(a.Val); ok {
				s.Align = al
			}
		case "class":
			for _, class := range strings.Fields(a.Val) {
				if class == "center" {
					s.Align = AlignCenter
				}
			}
		}
	}
	decls := declMap(n)
	if len(decls) == 0 {
		return s
	}
	if v, ok := decls["font-size"]; ok {
		if pt, ok := parseFontSize(v, s.FontSize); ok {
			s.FontSize = pt
		}
	}
	if v, ok := decls["line-height"]; ok {
		if lh, ok := parseLineHeight(v, s.FontSize); ok {
			s.LineHeight = lh
		}
	}
	if v, ok := decls["color"]; ok {
		if c, ok := parseColor(v); ok {
			s.Color = c
		}
	}
	if v, ok := decls["text-align"]; ok {
		if al, ok := parseAlign(v); ok {
			s.Align = al
		}
	}
	if v, ok := decls["margin-top"]; ok {
		if mm, ok := parseLengthMM(v, s.FontSize); ok {
			s.SpaceBefore = mm
		}
	}
	if v, ok := decls["margin-bottom"]; ok {
		if mm, ok := parseLengthMM(v, s.FontSize); ok {
			s.SpaceAfter = mm
		}
	}
	if v, ok := decls["text-indent"]; ok {
		if mm, ok := parseLengthMM(v, s.FontSize); ok {
			s.TextIndent = mm
		}
	}
	if v, ok := decls["font-weight"]; ok {
		v = strings.ToLower(strings.TrimSpace(v))
		if v == "bold" || v == "600" || v == "700" || v == "800" || v == "900" {
			// Represented on runs by the caller for inline elements; block
			// headings already render with their own size.
		}
	}
	switch strings.ToLower(strings.TrimSpace(decls["page-break-before"])) {
	case "always", "page", "left", "right":
		s.PageBreakBefore = true
	}
	return s
}

func declMap(n *html.Node) map[string]string {
	out := map[string]string{}
	for _, a := range n.Attr {
		if a.Key != "style" {
			continue
		}
		for _, decl := range strings.Split(a.Val, ";") {
			parts := strings.SplitN(decl, ":", 2)
			if len(parts) != 2 {
				continue
			}
			key := strings.ToLower(strings.TrimSpace(parts[0]))
			val := strings.TrimSpace(parts[1])
			if key != "" && val != "" {
				out[key] = val
			}
		}
	}
	return out
}

func parseAlign(v string) (Align, bool) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "left", "start":
		return AlignLeft, true
	case "center", "middle":
		return AlignCenter, true
	case "right", "end":
		return AlignRight, true
	}
	return AlignLeft, false
}

var ptPerMM = 72.0 / 25.4

func parseFontSize(v string, basePt float64) (float64, bool) {
	v = strings.ToLower(strings.TrimSpace(v))
	if strings.HasSuffix(v, "em") {
		n, err := strconv.ParseFloat(strings.TrimSuffix(v, "em"), 64)
		if err != nil {
			return 0, false
		}
		return basePt * n, true
	}
	mm, ok := parseLengthMM(v, basePt)
	if !ok {
		return 0, false
	}
	return mm * ptPerMM, true
}

func parseLineHeight(v string, fontSizePt float64) (float64, bool) {
	v = strings.ToLower(strings.TrimSpace(v))
	if strings.HasSuffix(v, "%") {
		n, err := strconv.ParseFloat(strings.TrimSuffix(v, "%"), 64)
		if err != nil {
			return 0, false
		}
		return n / 100, true
	}
	if n, err := strconv.ParseFloat(v, 64); err == nil {
		if n > 0 && n < 6 {
			return n, true
		}
		// A larger unitless value is treated as points.
		if n >= 6 && fontSizePt > 0 {
			return n / fontSizePt, true
		}
	}
	mm, ok := parseLengthMM(v, fontSizePt)
	if !ok || fontSizePt <= 0 {
		return 0, false
	}
	return mm * ptPerMM / fontSizePt, true
}

// parseLengthMM parses a CSS length into millimetres. Unitless values are
// treated as points for font sizes and as pixels for image dimensions.
func parseLengthMM(v string, fontSizePt float64) (float64, bool) {
	v = strings.TrimSpace(strings.ToLower(v))
	units := []struct {
		suffix string
		mm     float64
	}{
		{"mm", 1},
		{"cm", 10},
		{"in", 25.4},
		{"pt", 25.4 / 72},
		{"px", 25.4 / 96},
	}
	for _, u := range units {
		if strings.HasSuffix(v, u.suffix) {
			n, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(v, u.suffix)), 64)
			if err != nil {
				return 0, false
			}
			return n * u.mm, true
		}
	}
	if strings.HasSuffix(v, "em") {
		n, err := strconv.ParseFloat(strings.TrimSuffix(v, "em"), 64)
		if err != nil {
			return 0, false
		}
		return n * fontSizePt * 25.4 / 72, true
	}
	n, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, false
	}
	return n * 25.4 / 96, true
}

func parseColor(v string) (Color, bool) {
	v = strings.ToLower(strings.TrimSpace(v))
	named := map[string]Color{
		"black":  {0, 0, 0},
		"white":  {255, 255, 255},
		"gray":   {128, 128, 128},
		"grey":   {128, 128, 128},
		"red":    {200, 0, 0},
		"blue":   {0, 0, 200},
		"green":  {0, 128, 0},
		"silver": {192, 192, 192},
	}
	if c, ok := named[v]; ok {
		return c, true
	}
	if strings.HasPrefix(v, "#") {
		hex := strings.TrimPrefix(v, "#")
		switch len(hex) {
		case 3:
			r, ok1 := hexNibble(hex[0])
			g, ok2 := hexNibble(hex[1])
			b, ok3 := hexNibble(hex[2])
			if !ok1 || !ok2 || !ok3 {
				return Color{}, false
			}
			return Color{r * 17, g * 17, b * 17}, true
		case 6, 8:
			rr, err1 := strconv.ParseUint(hex[0:2], 16, 8)
			gg, err2 := strconv.ParseUint(hex[2:4], 16, 8)
			bb, err3 := strconv.ParseUint(hex[4:6], 16, 8)
			if err1 != nil || err2 != nil || err3 != nil {
				return Color{}, false
			}
			return Color{uint8(rr), uint8(gg), uint8(bb)}, true
		}
	}
	if strings.HasPrefix(v, "rgb(") && strings.HasSuffix(v, ")") {
		parts := strings.Split(strings.TrimSuffix(strings.TrimPrefix(v, "rgb("), ")"), ",")
		if len(parts) != 3 {
			return Color{}, false
		}
		var out [3]uint8
		for i, part := range parts {
			n, err := strconv.Atoi(strings.TrimSpace(part))
			if err != nil || n < 0 || n > 255 {
				return Color{}, false
			}
			out[i] = uint8(n)
		}
		return Color{out[0], out[1], out[2]}, true
	}
	return Color{}, false
}

func hexNibble(b byte) (uint8, bool) {
	switch {
	case b >= '0' && b <= '9':
		return b - '0', true
	case b >= 'a' && b <= 'f':
		return b - 'a' + 10, true
	}
	return 0, false
}
