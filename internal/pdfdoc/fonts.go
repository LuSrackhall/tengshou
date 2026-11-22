package pdfdoc

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/tdewolff/canvas"
)

// FontSpec identifies the font file used for a document.
type FontSpec struct {
	Path  string
	Index int
	Name  string
}

// FontOptions selects the CJK-capable font used by the PDF pipeline.
type FontOptions struct {
	// Path is an explicit font file. Empty means auto-detect.
	Path string
	// Index selects a face inside a .ttc/.otc collection.
	Index int
	// DataFontsDir is the per-installation fonts directory. Fonts placed there
	// by the user take precedence over system fonts.
	DataFontsDir string
}

const cjkProbe = "腾手合同甲方乙方签名日期金额，。：；（）"

// Disable typographic substitutions that map CJK runes to alternate glyphs
// without a reverse cmap entry. Without this, PDF ToUnicode tables lose some
// Chinese characters and text extraction/search returns U+FFFD.
const safeFontFeatures = "-ccmp,-locl,-vert,-vrt2,-fwid,-hwid,-pwid,-twid,-jp78,-jp83,-jp90,-jp04"

// LoadFont resolves a CJK-capable font and returns its family and spec.
//
// Resolution order:
//  1. explicit path (config or TENGHSHOU_FONT)
//  2. <data>/fonts (user supplied)
//  3. platform system fonts known to cover Chinese
func LoadFont(opts FontOptions) (*canvas.FontFamily, FontSpec, error) {
	if opts.Path != "" {
		family, spec, err := loadCandidate(opts.Path, opts.Index)
		if err != nil {
			return nil, FontSpec{}, fmt.Errorf("load configured font %s: %w", opts.Path, err)
		}
		return family, spec, nil
	}
	for _, path := range discoverDataFonts(opts.DataFontsDir) {
		if family, spec, err := loadCandidate(path, 0); err == nil {
			return family, spec, nil
		}
	}
	var tried []string
	for _, path := range systemFontCandidates() {
		if _, err := os.Stat(path); err != nil {
			continue
		}
		tried = append(tried, path)
		if family, spec, err := loadCandidate(path, 0); err == nil {
			return family, spec, nil
		}
	}
	return nil, FontSpec{}, fmt.Errorf(
		"no CJK-capable font found; install a Chinese font or set pdf.font_path / %s (tried: %s)",
		"TENGHSHOU_FONT", strings.Join(tried, ", "))
}

func discoverDataFonts(dir string) []string {
	if dir == "" {
		return nil
	}
	var out []string
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		switch strings.ToLower(filepath.Ext(path)) {
		case ".ttf", ".ttc", ".otf", ".otc":
			out = append(out, path)
		}
		return nil
	})
	sort.Strings(out)
	return out
}

func systemFontCandidates() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{
			"/System/Library/Fonts/Supplemental/Arial Unicode.ttf",
			"/System/Library/Fonts/Hiragino Sans GB.ttc",
			"/System/Library/Fonts/STHeiti Light.ttc",
			"/System/Library/Fonts/STHeiti Medium.ttc",
			"/System/Library/Fonts/PingFang.ttc",
			"/System/Library/Fonts/Supplemental/Songti.ttc",
			"/System/Library/PrivateFrameworks/FontServices.framework/Versions/A/Resources/Reserved/PingFangUI.ttc",
		}
	case "windows":
		windowsDir := os.Getenv("WINDIR")
		if windowsDir == "" {
			windowsDir = `C:\Windows`
		}
		fonts := filepath.Join(windowsDir, "Fonts")
		return []string{
			filepath.Join(fonts, "msyh.ttc"),
			filepath.Join(fonts, "msyhbd.ttc"),
			filepath.Join(fonts, "simhei.ttf"),
			filepath.Join(fonts, "simsun.ttc"),
			filepath.Join(fonts, "arialuni.ttf"),
			filepath.Join(fonts, "Deng.ttf"),
		}
	default:
		return []string{
			"/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc",
			"/usr/share/fonts/opentype/noto/NotoSansCJKsc-Regular.otf",
			"/usr/share/fonts/truetype/noto/NotoSansCJK-Regular.ttc",
			"/usr/share/fonts/noto-cjk/NotoSansCJK-Regular.ttc",
			"/usr/share/fonts/truetype/noto/NotoSansSC-Regular.ttf",
			"/usr/share/fonts/truetype/arphic/uming.ttc",
			"/usr/share/fonts/truetype/wqy/wqy-zenhei.ttc",
			"/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
		}
	}
}

func loadCandidate(path string, preferredIndex int) (*canvas.FontFamily, FontSpec, error) {
	indices := []int{preferredIndex}
	maxIndex := 0
	switch strings.ToLower(filepath.Ext(path)) {
	case ".ttc", ".otc":
		maxIndex = 15
	}
	for i := 0; i <= maxIndex; i++ {
		if i != preferredIndex {
			indices = append(indices, i)
		}
	}
	var lastErr error
	for _, index := range indices {
		family := canvas.NewFontFamily("tengshou-cjk")
		if err := family.LoadFontCollection(path, index, canvas.FontRegular); err != nil {
			lastErr = err
			continue
		}
		family.SetFeatures(safeFontFeatures)
		if !coversCJK(family) {
			lastErr = fmt.Errorf("font face %d does not cover Chinese glyphs", index)
			continue
		}
		face := family.Face(12)
		return family, FontSpec{Path: path, Index: index, Name: face.Font.Name()}, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no usable font face")
	}
	return nil, FontSpec{}, lastErr
}

func coversCJK(family *canvas.FontFamily) bool {
	face := family.Face(12)
	if face == nil || face.Font == nil {
		return false
	}
	for _, r := range cjkProbe {
		if r == ' ' {
			continue
		}
		if face.Font.SFNT.GlyphIndex(r) == 0 {
			return false
		}
	}
	return true
}

// MissingGlyphs returns runes in s that the face cannot render.
func MissingGlyphs(face *canvas.FontFace, s string) []rune {
	missing := []rune{}
	seen := map[rune]bool{}
	for _, r := range s {
		if r == '\n' || r == '\r' || r == '\t' || r == ' ' || seen[r] {
			continue
		}
		seen[r] = true
		if face.Font.SFNT.GlyphIndex(r) == 0 {
			missing = append(missing, r)
		}
	}
	return missing
}
