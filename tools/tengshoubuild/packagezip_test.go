package main

import (
	"archive/zip"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestPackageWindowsZipCreatesExactlyOneFile(t *testing.T) {
	dir := t.TempDir()
	exePath := filepath.Join(dir, windowsExeName)
	if err := writeTestFile(exePath, "fake windows exe"); err != nil {
		t.Fatal(err)
	}

	if err := packageWindowsZip(dir); err != nil {
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
	if len(names) != 1 || names[0] != windowsExeName {
		t.Fatalf("zip 必须只含桌面 exe，实际：%v", names)
	}
}

func TestPackageWindowsZipMissingInputs(t *testing.T) {
	dir := t.TempDir()
	err := packageWindowsZip(dir)
	if err == nil || !strings.Contains(err.Error(), "缺少打包输入") {
		t.Fatalf("缺少桌面 exe 应报错，实际：%v", err)
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

	if err := verifyZip(zipPath, []string{windowsExeName}); err == nil {
		t.Fatal("文件名不符时应被拒绝")
	}
	if err := verifyZip(zipPath, []string{"one.bin", windowsExeName}); err == nil {
		t.Fatal("文件数量不符时应被拒绝")
	}
	if err := verifyZip(zipPath, []string{"one.bin"}); err != nil {
		t.Fatalf("内容相符时应通过：%v", err)
	}

	if _, err := os.Stat(zipPath); err != nil {
		t.Fatalf("zip 文件应存在：%v", err)
	}
}
