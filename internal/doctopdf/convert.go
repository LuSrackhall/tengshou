// Package doctopdf converts user-provided Word documents (.docx) into PDF.
//
// The user only ever edits a Word document; 腾手 stores the original file and
// uses the PDF for faithful preview and for overlaying contract data. On
// Windows an installed Microsoft Word is preferred because its export is
// pixel-faithful to what the user saw. LibreOffice headless is the fallback on
// every platform. The converters are interfaces so tests (and CI machines with
// neither program) can substitute a fake and skip real conversions.
package doctopdf

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Converter turns one .docx file into a PDF at pdfPath.
type Converter interface {
	// Name is the stable machine name recorded on the template ("word",
	// "libreoffice").
	Name() string
	// Label is the human-facing name shown in errors and the UI.
	Label() string
	// Available reports whether this converter can run here, with a short
	// reason/path for diagnostics.
	Available() (bool, string)
	// Hint is one sentence of installation guidance for the UI.
	Hint() string
	// Convert writes the PDF. The caller supplies a writable destination path
	// and a context that bounds the conversion time.
	Convert(ctx context.Context, docxPath, pdfPath string) error
}

// Availability describes one converter for the desktop UI.
type Availability struct {
	Name      string `json:"name"`
	Label     string `json:"label"`
	Available bool   `json:"available"`
	Detail    string `json:"detail,omitempty"`
	Hint      string `json:"hint,omitempty"`
}

// Chain tries converters in order and reports the one that succeeded.
type Chain struct {
	Converters []Converter
}

// DefaultChain prefers Microsoft Word (Windows only) and falls back to
// LibreOffice headless.
func DefaultChain() *Chain {
	return &Chain{Converters: []Converter{NewWord(), NewLibreOffice()}}
}

// Availability lists every converter's state for the UI.
func (c *Chain) Availability() []Availability {
	out := make([]Availability, 0, len(c.Converters))
	for _, conv := range c.Converters {
		ok, detail := conv.Available()
		out = append(out, Availability{
			Name:      conv.Name(),
			Label:     conv.Label(),
			Available: ok,
			Detail:    detail,
			Hint:      conv.Hint(),
		})
	}
	return out
}

// Convert runs the first available converter. It returns the converter name.
// When nothing works the error lists what was tried and how to recover.
func (c *Chain) Convert(ctx context.Context, docxPath, pdfPath string) (string, error) {
	var attempts []string
	for _, conv := range c.Converters {
		ok, detail := conv.Available()
		if !ok {
			attempts = append(attempts, fmt.Sprintf("%s 不可用（%s）", conv.Label(), detail))
			continue
		}
		if err := conv.Convert(ctx, docxPath, pdfPath); err == nil {
			if info, statErr := os.Stat(pdfPath); statErr == nil && info.Size() > 0 {
				return conv.Name(), nil
			}
			attempts = append(attempts, fmt.Sprintf("%s 未生成 PDF", conv.Label()))
			continue
		} else {
			attempts = append(attempts, fmt.Sprintf("%s 失败：%s", conv.Label(), compactError(err)))
		}
	}
	return "", &UnavailableError{Attempts: attempts}
}

// UnavailableError means no converter could produce a PDF.
type UnavailableError struct {
	Attempts []string
}

func (e *UnavailableError) Error() string {
	tried := "没有可用的转换程序"
	if len(e.Attempts) > 0 {
		tried = strings.Join(e.Attempts, "；")
	}
	return "无法把 Word 文档转换为 PDF（" + tried + "）。请安装 Microsoft Word（Windows）或 " +
		"LibreOffice（https://www.libreoffice.org/download/ ，macOS / Windows / Linux 均可），安装后重试；" +
		"也可以先用 Word / WPS 将文档另存为 PDF，再直接导入 PDF。"
}

// IsUnavailable reports whether err means no converter is usable.
func IsUnavailable(err error) bool {
	var target *UnavailableError
	return errors.As(err, &target)
}

// findExecutable looks for the first existing executable among names and
// absolute candidates.
func findExecutable(names []string, candidates []string) (string, bool) {
	for _, name := range names {
		if p, err := exec.LookPath(name); err == nil {
			return p, true
		}
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() {
			return candidate, true
		}
	}
	return "", false
}

// compactError trims multi-line command output for one-line warnings.
func compactError(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.ReplaceAll(err.Error(), "\r", " ")
	msg = strings.ReplaceAll(msg, "\n", " ")
	msg = strings.TrimSpace(msg)
	if len([]rune(msg)) > 400 {
		msg = string([]rune(msg)[:400]) + "…"
	}
	return msg
}

var errEmptyPDF = errors.New("转换程序没有生成 PDF 文件")
