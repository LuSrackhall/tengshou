package doctopdf

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// Word converts .docx with a locally installed Microsoft Word via COM
// automation in Windows PowerShell. No cgo is involved: PowerShell is the
// bridge, so this file compiles everywhere and is simply unavailable off
// Windows. Word's export preserves the document exactly as the user sees it.
type Word struct {
	// PowerShell overrides the executable used to run the COM script.
	PowerShell string
}

// NewWord returns the Microsoft Word converter.
func NewWord() *Word { return &Word{} }

// Name implements Converter.
func (w *Word) Name() string { return "word" }

// Label implements Converter.
func (w *Word) Label() string { return "Microsoft Word" }

// Hint implements Converter.
func (w *Word) Hint() string {
	return "Windows 上安装 Microsoft Word 桌面版时，导出效果与 Word 中完全一致"
}

// Available implements Converter. Word itself is detected when converting;
// this only checks the platform and PowerShell.
func (w *Word) Available() (bool, string) {
	if runtime.GOOS != "windows" {
		return false, "仅 Windows 可用（当前系统 " + runtime.GOOS + "）"
	}
	ps := strings.TrimSpace(w.PowerShell)
	if ps == "" {
		ps = strings.TrimSpace(os.Getenv("TENGSHOU_POWERSHELL"))
	}
	if ps != "" {
		if info, err := os.Stat(ps); err == nil && !info.IsDir() {
			return true, ps
		}
	}
	if path, err := exec.LookPath("powershell.exe"); err == nil {
		return true, path
	}
	if path, err := exec.LookPath("powershell"); err == nil {
		return true, path
	}
	return false, "找不到 powershell.exe"
}

// wordScript opens the document read-only, exports it as PDF (format 17) and
// quits Word. Paths are passed through environment variables so quotes and
// non-ASCII characters in file names are safe.
const wordScript = `$ErrorActionPreference = 'Stop'
$word = New-Object -ComObject Word.Application
$word.Visible = $false
$word.DisplayAlerts = 0
try {
  $doc = $word.Documents.Open($env:TENGSHOU_DOCX, $false, $true)
  try {
    $doc.ExportAsFixedFormat($env:TENGSHOU_PDF, 17, $false, 0, 0)
  } finally {
    $doc.Close($false)
  }
} finally {
  $word.Quit()
}
`

// Convert implements Converter.
func (w *Word) Convert(ctx context.Context, docxPath, pdfPath string) error {
	ok, detail := w.Available()
	if !ok {
		return fmt.Errorf("%s", detail)
	}
	_ = os.Remove(pdfPath)

	cmd := exec.CommandContext(ctx, detail, "-NoProfile", "-NonInteractive",
		"-ExecutionPolicy", "Bypass", "-Command", wordScript)
	cmd.Env = append(os.Environ(),
		"TENGSHOU_DOCX="+docxPath,
		"TENGSHOU_PDF="+pdfPath,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := compactError(fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out))))
		return fmt.Errorf("Word COM 导出失败：%s", msg)
	}
	info, statErr := os.Stat(pdfPath)
	if statErr != nil || info.Size() == 0 {
		return errEmptyPDF
	}
	return nil
}
