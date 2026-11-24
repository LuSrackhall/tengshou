package main

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
)

const (
	windowsZipName   = "tengshou-windows-amd64.zip"
	windowsExeName   = "tengshou-windows-amd64.exe"
	windowsNotesName = "MCP-接入.txt"
)

// runPackageWindowsZip 组装 Windows 分发包：桌面应用 + 接入说明，共 2 个文件。
//
// 客户只拿桌面客户端一个 exe：它同时提供 GUI 与 `mcp` stdio 子命令。
// 完整 CLI 仍是独立发布资产，不放进 zip。
func runPackageWindowsZip(args []string) error {
	if len(args) > 1 {
		return errors.New("用法：go run ./tools/tengshoubuild package-windows-zip [dist 目录]")
	}
	root, err := repoRoot()
	if err != nil {
		return err
	}
	// 与旧脚本一致：默认 dist 目录在仓库根；显式参数按调用方工作目录解析。
	outDir := filepath.Join(root, "dist")
	if len(args) == 1 {
		outDir = args[0]
	}
	// 接入说明是仓库内的固定输入。
	notesPath := filepath.Join(root, "packaging", "windows", windowsNotesName)
	return packageWindowsZip(outDir, notesPath)
}

// packageWindowsZip 写出 zip 并断言其恰好包含桌面 exe 与接入说明 2 个文件。
// 用 archive/zip 而不是 zip(1)：中文文件名需要 UTF-8 通用标志位（0x800），
// archive/zip 对非 CP437 的合法 UTF-8 名称会自动置位。
func packageWindowsZip(outDir, notesPath string) error {
	exePath := filepath.Join(outDir, windowsExeName)
	for _, input := range []string{exePath, notesPath} {
		info, err := os.Stat(input)
		if err != nil || info.IsDir() {
			return fmt.Errorf("缺少打包输入 %s", input)
		}
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("创建输出目录 %s: %w", outDir, err)
	}
	zipPath := filepath.Join(outDir, windowsZipName)
	if err := os.Remove(zipPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("删除旧 zip %s: %w", zipPath, err)
	}

	entries := []zipEntry{
		{src: exePath, name: windowsExeName},
		{src: notesPath, name: windowsNotesName},
	}
	if err := writeZip(zipPath, entries); err != nil {
		return err
	}
	if err := verifyZip(zipPath, []string{windowsExeName, windowsNotesName}); err != nil {
		return err
	}
	fmt.Printf("已生成 %s\n包含文件：\n  %s\n  %s\n", zipPath, windowsExeName, windowsNotesName)
	return nil
}

// zipEntry 描述 zip 内一个文件：来源路径与包内文件名。
type zipEntry struct {
	src  string
	name string
}

func writeZip(zipPath string, entries []zipEntry) error {
	file, err := os.Create(zipPath)
	if err != nil {
		return fmt.Errorf("创建 zip %s: %w", zipPath, err)
	}
	writer := zip.NewWriter(file)
	for _, entry := range entries {
		if err := addZipFile(writer, entry); err != nil {
			writer.Close()
			file.Close()
			return err
		}
	}
	if err := writer.Close(); err != nil {
		file.Close()
		return fmt.Errorf("写入 zip %s: %w", zipPath, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("关闭 zip %s: %w", zipPath, err)
	}
	return nil
}

func addZipFile(writer *zip.Writer, entry zipEntry) error {
	src, err := os.Open(entry.src)
	if err != nil {
		return fmt.Errorf("打开 %s: %w", entry.src, err)
	}
	defer src.Close()
	info, err := src.Stat()
	if err != nil {
		return fmt.Errorf("读取 %s 信息: %w", entry.src, err)
	}
	header, err := zip.FileInfoHeader(info)
	if err != nil {
		return fmt.Errorf("构造 %s 的 zip 头: %w", entry.name, err)
	}
	header.Name = entry.name
	header.Method = zip.Deflate
	dst, err := writer.CreateHeader(header)
	if err != nil {
		return fmt.Errorf("写入 %s: %w", entry.name, err)
	}
	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("复制 %s 到 zip: %w", entry.name, err)
	}
	return nil
}

// verifyZip 重新打开 zip，断言恰好 2 个文件且文件名与预期一致。
func verifyZip(zipPath string, wantNames []string) error {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("打开 zip 校验 %s: %w", zipPath, err)
	}
	defer reader.Close()

	got := make([]string, 0, len(reader.File))
	for _, file := range reader.File {
		got = append(got, file.Name)
	}
	sort.Strings(got)
	if len(got) != 2 {
		return fmt.Errorf("zip 必须恰好 2 个文件，实际 %d 个：%v", len(got), got)
	}
	want := slices.Clone(wantNames)
	sort.Strings(want)
	if !slices.Equal(got, want) {
		return fmt.Errorf("zip 内容不符：%v != %v", got, want)
	}
	return nil
}
