package doctopdf

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
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
	profileURL := fileURL(profileDir)
	cmd := libreOfficeCommand(ctx, bin, profileURL, outDir, input)
	if extra := fontConfigEnv(temp); len(extra) > 0 {
		cmd.Env = append(os.Environ(), extra...)
	}
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

// libreOfficeCommand builds the headless conversion command. It is factored
// out so tests can verify the profile URL and wait behaviour without starting
// LibreOffice.
func libreOfficeCommand(ctx context.Context, bin, profileURL, outDir, input string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, bin,
		"--headless", "--norestore", "--nolockcheck", "--nodefault", "--nofirststartwizard",
		"-env:UserInstallation="+profileURL,
		"--convert-to", "pdf:writer_pdf_Export",
		"--outdir", outDir,
		input,
	)
	// Context cancellation kills only the direct child. A leftover
	// soffice.bin can keep the output pipes open, so CombinedOutput would
	// wait forever; WaitDelay bounds that wait after the kill.
	cmd.WaitDelay = 10 * time.Second
	return cmd
}

// fileURL renders an absolute filesystem path as a file:// URL. Windows
// paths such as C:/Users/... must gain a slash after the scheme
// (file:///C:/Users/...): without it the drive letter parses as the URL
// host, and LibreOffice hangs on the invalid profile URL instead of
// converting. POSIX paths already start with "/" and are unaffected.
func fileURL(path string) string {
	p := filepath.ToSlash(path)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return (&url.URL{Scheme: "file", Path: p}).String()
}

// fontConfigEnv 为 LibreOffice 转换进程准备 fontconfig 环境。
//
// macOS 上 LibreOffice 自带的 fontconfig 默认配置不会扫描系统字体目录
// （/System/Library/Fonts 等），headless 转换只剩随包附带的少量西文字体：
// 中文整体丢失——PDF 显示为空白，文字层退化成同一个字符。三端业务验收
// 在 macOS 上实测如此。这里显式提供一个包含 macOS 字体目录的配置；用户
// 自己设置了 FONTCONFIG_FILE 时保持用户配置不变。
func fontConfigEnv(tempDir string) []string {
	if runtime.GOOS != "darwin" {
		return nil
	}
	if strings.TrimSpace(os.Getenv("FONTCONFIG_FILE")) != "" {
		return nil
	}
	cacheDir := filepath.Join(tempDir, "fontconfig-cache")
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		slog.Warn("创建 macOS 字体缓存目录失败，将回退默认字体配置（中文可能显示为空白）",
			"dir", cacheDir, "error", err)
		return nil
	}
	confPath := filepath.Join(tempDir, "fonts.conf")
	if err := os.WriteFile(confPath, []byte(macOSFontsConf(cacheDir)), 0o600); err != nil {
		slog.Warn("写入 macOS 字体配置失败，将回退默认字体配置（中文可能显示为空白）",
			"path", confPath, "error", err)
		return nil
	}
	return []string{"FONTCONFIG_FILE=" + confPath}
}

// macOSFontsConf 列出 macOS 上字体可能出现的目录（fontconfig 会忽略不存在
// 的目录），缓存放进本次转换的临时目录，避免污染用户缓存。
func macOSFontsConf(cacheDir string) string {
	home := os.Getenv("HOME")
	dirs := []string{
		"/System/Library/Fonts",
		"/System/Library/Fonts/Supplemental",
		"/Library/Fonts",
		"/System/Library/Assets",
		"/System/Library/AssetsV2",
		"/usr/local/share/fonts",
		"/opt/homebrew/share/fonts",
	}
	if home != "" {
		dirs = append(dirs, filepath.Join(home, "Library", "Fonts"))
	}
	if dataHome := strings.TrimSpace(os.Getenv("XDG_DATA_HOME")); dataHome != "" {
		dirs = append(dirs, filepath.Join(dataHome, "fonts"))
	} else if home != "" {
		dirs = append(dirs, filepath.Join(home, ".local", "share", "fonts"))
	}
	var b strings.Builder
	b.WriteString("<?xml version=\"1.0\"?>\n<fontconfig>\n")
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		b.WriteString("  <dir>" + xmlEscape(dir) + "</dir>\n")
	}
	b.WriteString("  <cachedir>" + xmlEscape(cacheDir) + "</cachedir>\n</fontconfig>\n")
	return b.String()
}

// xmlEscape 转义 fontconfig 配置里的路径（HOME 可能含 & 等字符）。
func xmlEscape(value string) string {
	var buf bytes.Buffer
	if err := xml.EscapeText(&buf, []byte(value)); err != nil {
		return value
	}
	return buf.String()
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
