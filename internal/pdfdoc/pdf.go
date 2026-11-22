package pdfdoc

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html/template"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/LuSrackhall/tengshou/internal/model"
	"github.com/tdewolff/canvas"
	"github.com/tdewolff/canvas/renderers/pdf"
)

// TemplateData is the data model handed to HTML templates.
type TemplateData struct {
	Contract   model.Contract
	PartyA     *model.Party
	PartyB     *model.Party
	Signatures []model.Signature
	Data       map[string]any
	Now        time.Time
}

// Generator renders HTML templates into paginated PDF documents.
type Generator struct {
	Font       *canvas.FontFamily
	Spec       FontSpec
	PageWidth  float64
	PageHeight float64
	MarginMM   float64
	ShowFooter bool
}

// Result describes a generated PDF.
type Result struct {
	Path      string
	SHA256    string
	PageCount int
	Missing   []rune
	FontPath  string
	FontIndex int
	SizeBytes int64
}

// NewGenerator resolves the CJK font once and returns a reusable generator.
func NewGenerator(fontOpts FontOptions, marginMM float64, showFooter bool) (*Generator, error) {
	family, spec, err := LoadFont(fontOpts)
	if err != nil {
		return nil, err
	}
	if marginMM <= 0 {
		marginMM = 20
	}
	return &Generator{
		Font:       family,
		Spec:       spec,
		PageWidth:  210,
		PageHeight: 297,
		MarginMM:   marginMM,
		ShowFooter: showFooter,
	}, nil
}

// RenderHTML renders template source with data, parses the supported HTML
// subset and paginates it.
func (g *Generator) RenderHTML(htmlSource string, data TemplateData, assetRoot, header string) (*Document, error) {
	rendered, err := ExecuteTemplate(htmlSource, data)
	if err != nil {
		return nil, err
	}
	blocks, err := ParseHTML(strings.NewReader(rendered), ParserOptions{
		FontSize:   11,
		LineHeight: 1.65,
		Color:      Black,
	})
	if err != nil {
		return nil, err
	}
	return Render(blocks, Options{
		Font:       g.Font,
		FontSpec:   g.Spec,
		PageWidth:  g.PageWidth,
		PageHeight: g.PageHeight,
		MarginMM:   g.MarginMM,
		AssetRoot:  assetRoot,
		Header:     header,
		ShowFooter: g.ShowFooter,
	})
}

// RenderFile renders to outPath atomically and returns artifact metadata.
func (g *Generator) RenderFile(htmlSource string, data TemplateData, assetRoot, header, outPath string) (Result, error) {
	doc, err := g.RenderHTML(htmlSource, data, assetRoot, header)
	if err != nil {
		return Result{}, err
	}
	sha, size, err := WritePDFFile(doc, outPath, data.Contract.Title)
	if err != nil {
		return Result{}, err
	}
	return Result{
		Path:      outPath,
		SHA256:    sha,
		PageCount: doc.PageCount,
		Missing:   doc.Missing,
		FontPath:  g.Spec.Path,
		FontIndex: g.Spec.Index,
		SizeBytes: size,
	}, nil
}

// ExecuteTemplate renders an html/template with 腾手's helper functions.
func ExecuteTemplate(source string, data TemplateData) (string, error) {
	funcs := template.FuncMap{
		"cnDate": cnDate,
		"date":   cnDate,
		"money":  money,
		"default": func(def any, value any) any {
			if isEmpty(value) {
				return def
			}
			return value
		},
		// signaturePath resolves the latest retained signature for a party so
		// templates can place it with <img src="{{signaturePath .Signatures .PartyB}}">.
		"signaturePath": signaturePath,
	}
	t, err := template.New("contract").Funcs(funcs).Option("missingkey=error").Parse(source)
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}
	return buf.String(), nil
}

// signaturePath returns the asset path of the newest signature for a party.
// party may be a *model.Party, a model.Party or a party id string; nil means
// "any party". It returns "" when no signature is available so templates can
// use {{with}} to omit the image block.
func signaturePath(sigs []model.Signature, party any) string {
	partyID := ""
	switch p := party.(type) {
	case *model.Party:
		if p != nil {
			partyID = p.ID
		}
	case model.Party:
		partyID = p.ID
	case string:
		partyID = p
	}
	best := -1
	for i, sig := range sigs {
		if strings.TrimSpace(sig.AssetPath) == "" {
			continue
		}
		if partyID != "" && sig.PartyID != partyID {
			continue
		}
		if best < 0 {
			best = i
			continue
		}
		cur := sigs[best]
		if sig.CapturedAt.After(cur.CapturedAt) ||
			(sig.CapturedAt.Equal(cur.CapturedAt) && sig.CreatedAt.After(cur.CreatedAt)) {
			best = i
		}
	}
	if best < 0 {
		return ""
	}
	return sigs[best].AssetPath
}

func isEmpty(v any) bool {
	if v == nil {
		return true
	}
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x) == ""
	case []any:
		return len(x) == 0
	case map[string]any:
		return len(x) == 0
	}
	return false
}

var dateLayouts = []string{
	time.RFC3339,
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05",
	"2006-01-02",
	"2006/01/02",
	"2006年1月2日",
}

func cnDate(v any) string {
	switch x := v.(type) {
	case time.Time:
		if x.IsZero() {
			return ""
		}
		return fmt.Sprintf("%d年%d月%d日", x.Year(), int(x.Month()), x.Day())
	case string:
		s := strings.TrimSpace(x)
		if s == "" {
			return ""
		}
		for _, layout := range dateLayouts {
			if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
				return fmt.Sprintf("%d年%d月%d日", t.Year(), int(t.Month()), t.Day())
			}
		}
		return s
	}
	return fmt.Sprint(v)
}

func money(v any) string {
	var f float64
	switch x := v.(type) {
	case float64:
		f = x
	case float32:
		f = float64(x)
	case int:
		f = float64(x)
	case int64:
		f = float64(x)
	case jsonNumber:
		f, _ = strconv.ParseFloat(x.String(), 64)
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		if err != nil {
			return x
		}
		f = parsed
	default:
		return fmt.Sprint(v)
	}
	negative := f < 0
	if negative {
		f = -f
	}
	rounded := fmt.Sprintf("%.2f", f)
	parts := strings.SplitN(rounded, ".", 2)
	intPart := parts[0]
	var grouped strings.Builder
	for i, r := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			grouped.WriteByte(',')
		}
		grouped.WriteRune(r)
	}
	out := grouped.String() + "." + parts[1]
	if negative {
		out = "-" + out
	}
	return out
}

// jsonNumber avoids importing encoding/json just for the interface.
type jsonNumber interface{ String() string }

// ExtractVariables returns the .Data keys referenced by a template.
func ExtractVariables(source string) []string {
	re := regexp.MustCompile(`\{\{[^}]*?\.Data\.([A-Za-z_][A-Za-z0-9_]*)[^}]*\}\}`)
	seen := map[string]bool{}
	for _, match := range re.FindAllStringSubmatch(source, -1) {
		seen[match[1]] = true
	}
	out := make([]string, 0, len(seen))
	for key := range seen {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// WritePDFFile renders doc to path atomically and returns its SHA-256 and size.
func WritePDFFile(doc *Document, path, title string) (string, int64, error) {
	if len(doc.Pages) == 0 {
		return "", 0, fmt.Errorf("pdf: document has no pages")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", 0, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return "", 0, err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	hash := sha256.New()
	mw := io.MultiWriter(tmp, hash)
	r := pdf.New(mw, doc.Width, doc.Height, &pdf.Options{Compress: true, SubsetFonts: true})
	r.SetLang("zh-CN")
	r.SetInfo(title, "", "腾手合同", "腾手", "腾手")
	for i, page := range doc.Pages {
		if i > 0 {
			r.NewPage(doc.Width, doc.Height)
		}
		page.RenderTo(r)
	}
	if err := r.Close(); err != nil {
		tmp.Close()
		return "", 0, fmt.Errorf("close pdf: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return "", 0, err
	}
	if err := tmp.Close(); err != nil {
		return "", 0, err
	}
	stat, err := os.Stat(tmpName)
	if err != nil {
		return "", 0, err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(hash.Sum(nil)), stat.Size(), nil
}
