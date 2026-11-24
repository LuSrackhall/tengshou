package main

import (
	"archive/zip"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestPackageWindowsZipCreatesExactlyTwoFiles(t *testing.T) {
	dir := t.TempDir()
	exePath := filepath.Join(dir, windowsExeName)
	if err := writeTestFile(exePath, "fake windows exe"); err != nil {
		t.Fatal(err)
	}
	notesPath := filepath.Join(dir, windowsNotesName)
	if err := writeTestFile(notesPath, "MCP 接入说明"); err != nil {
		t.Fatal(err)
	}

	if err := packageWindowsZip(dir, notesPath); err != nil {
		t.Fatalf("packageWindowsZip: %v", err)
	}

	reader, err := zip.OpenReader(filepath.Join(dir, windowsZipName))
	if err != nil {
		t.Fatalf("打开生成的 zip: %v", err)
	}
	defer reader.Close()

	names := make([]string, 0, len(reader.File))
	for _, file := range reader.File {
		names = append(names, file.Name)
	}
	sort.Strings(names)
	want := []string{windowsNotesName, windowsExeName}
	sort.Strings(want)
	if len(names) != 2 || names[0] != want[0] || names[1] != want[1] {
		t.Fatalf("zip 内容不符：%v != %v", names, want)
	}

	// 中文文件名必须置 UTF-8 通用标志位（0x800），否则 Windows 资源管理器
	// 会按本地代码页显示成乱码。
	for _, file := range reader.File {
		if file.Name == windowsNotesName && file.Flags&0x800 == 0 {
			t.Fatalf("%s 缺少 UTF-8 标志位 0x800（flags=%#x）", file.Name, file.Flags)
		}
	}
}

func TestPackageWindowsZipMissingInputs(t *testing.T) {
	dir := t.TempDir()
	notesPath := filepath.Join(dir, windowsNotesName)
	if err := writeTestFile(notesPath, "说明"); err != nil {
		t.Fatal(err)
	}
	err := packageWindowsZip(dir, notesPath)
	if err == nil || !strings.Contains(err.Error(), "缺少打包输入") {
		t.Fatalf("缺少桌面 exe 应报错，实际：%v", err)
	}

	if err := writeTestFile(filepath.Join(dir, windowsExeName), "exe"); err != nil {
		t.Fatal(err)
	}
	err = packageWindowsZip(dir, filepath.Join(dir, "missing.txt"))
	if err == nil || !strings.Contains(err.Error(), "缺少打包输入") {
		t.Fatalf("缺少接入说明应报错，实际：%v", err)
	}
}

func TestVerifyZipRejectsWrongContents(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "one.bin")
	if err := writeTestFile(src, "data"); err != nil {
		t.Fatal(err)
	}
	zipPath := filepath.Join(dir, "wrong.zip")
	if err := writeZip(zipPath, []zipEntry{{src: src, name: "one.bin"}}); err != nil {
		t.Fatalf("writeZip: %v", err)
	}

	if err := verifyZip(zipPath, []string{windowsExeName, windowsNotesName}); err == nil {
		t.Fatal("单文件 zip 应被拒绝（必须恰好 2 个文件）")
	}
	if err := verifyZip(zipPath, []string{"one.bin"}); err == nil {
		t.Fatal("文件名不符时应被拒绝，而不是按数量放行")
	}

	if _, err := os.Stat(zipPath); err != nil {
		t.Fatalf("zip 文件应存在：%v", err)
	}
}
