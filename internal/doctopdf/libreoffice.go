package doctopdf

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// LibreOffice converts .docx with a headless LibreOffice (soffice) process.
// It runs with an isolated user profile in a temporary directory, so it can
// run alongside a LibreOffice the user has open.
type LibreOffice struct {
	// Binary overrides the detected soffice path.
	Binary string
}

// NewLibreOffice returns the LibreOffice headless converter.
func NewLibreOffice() *LibreOffice { return &LibreOffice{} }

// Name implements Converter.
func (l *LibreOffice) Name() string { return "libreoffice" }

// Label implements Converter.
func (l *LibreOffice) Label() string { return "LibreOffice" }

// Hint implements Converter.
func (l *LibreOffice) Hint() string {
	return "免费安装 LibreOffice 即可在 macOS / Windows / Linux 转换；结果与 Word 可能有细微排版差异"
}

// Available implements Converter.
func (l *LibreOffice) Available() (bool, string) {
	if path, ok := l.binary(); ok {
		return true, path
	}
	return false, "未找到 soffice（已检查 PATH 与常见安装目录）"
}

func (l *LibreOffice) binary() (string, bool) {
	if bin := strings.TrimSpace(l.Binary); bin != "" {
		if info, err := os.Stat(bin); err == nil && !info.IsDir() {
			return bin, true
		}
	}
	if env := strings.TrimSpace(os.Getenv("TENGHSHOU_SOFFICE")); env != "" {
		if info, err := os.Stat(env); err == nil && !info.IsDir() {
			return env, true
		}
	}
	return findExecutable(
		[]string{"soffice", "libreoffice", "soffice.exe", "libreoffice.exe"},
		libreOfficeCandidates(),
	)
}

func libreOfficeCandidates() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{
			"/Applications/LibreOffice.app/Contents/MacOS/soffice",
			filepath.Join(os.Getenv("HOME"), "Applications", "LibreOffice.app", "Contents", "MacOS", "soffice"),
		}
	case "windows":
		var out []string
		for _, env := range []string{"ProgramFiles", "ProgramFiles(x86)", "LOCALAPPDATA"} {
			base := os.Getenv(env)
			if base == "" {
				continue
			}
			path := filepath.Join(base, "LibreOffice", "program", "soffice.exe")
			if env == "LOCALAPPDATA" {
				path = filepath.Join(base, "Programs", "LibreOffice", "program", "soffice.exe")
			}
			out = append(out, path)
		}
		return out
	default:
		return []string{
			"/usr/bin/soffice",
			"/usr/bin/libreoffice",
			"/usr/lib/libreoffice/program/soffice",
			"/snap/bin/libreoffice",
			"/opt/libreoffice/program/soffice",
		}
	}
}

// Convert implements Converter. The document is copied into a temporary
// directory under a fixed name so the produced file is predictable and
// unrelated files in the user's folders are never touched.
func (l *LibreOffice) Convert(ctx context.Context, docxPath, pdfPath string) error {
	bin, ok := l.binary()
	if !ok {
		return fmt.Errorf("未找到 LibreOffice（soffice）")
	}
	temp, err := os.MkdirTemp("", "tengshou-lo-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)
	profileDir := filepath.Join(temp, "profile")
	outDir := filepath.Join(temp, "out")
	for _, dir := range []string{profileDir, outDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	input := filepath.Join(temp, "source.docx")
	if err := copyFile(docxPath, input); err != nil {
		return fmt.Errorf("读取 Word 文件失败：%w", err)
	}
	profileURL := (&url.URL{Scheme: "file", Path: filepath.ToSlash(profileDir)}).String()
	cmd := exec.CommandContext(ctx, bin,
		"--headless", "--norestore", "--nolockcheck", "--nodefault", "--nofirststartwizard",
		"-env:UserInstallation="+profileURL,
		"--convert-to", "pdf:writer_pdf_Export",
		"--outdir", outDir,
		input,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("LibreOffice 转换失败：%s",
			compactError(fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))))
	}
	entries, err := os.ReadDir(outDir)
	if err != nil {
		return err
	}
	generated := ""
	for _, entry := range entries {
		if !entry.IsDir() && strings.EqualFold(filepath.Ext(entry.Name()), ".pdf") {
			generated = filepath.Join(outDir, entry.Name())
			break
		}
	}
	if generated == "" {
		return errEmptyPDF
	}
	data, err := os.ReadFile(generated)
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return errEmptyPDF
	}
	return writeFileAtomic(pdfPath, data)
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return writeFileAtomic(dst, data)
}

func writeFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
