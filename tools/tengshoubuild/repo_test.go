package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRepoRootFindsGoMod(t *testing.T) {
	root, err := repoRoot()
	if err != nil {
		t.Fatalf("repoRoot: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("repoRoot 未返回包含 go.mod 的目录：%s（%v）", root, err)
	}
	if _, err := os.Stat(filepath.Join(root, "tools", "tengshoubuild")); err != nil {
		t.Fatalf("repoRoot 指向的位置不是仓库根：%s（%v）", root, err)
	}
}

func TestResolveOut(t *testing.T) {
	root := string(filepath.Separator) + filepath.Join("repo", "tengshou")
	if got := resolveOut(root, "dist"); got != filepath.Join(root, "dist") {
		t.Fatalf("相对 OUT 应按仓库根解析：%s", got)
	}
	abs := filepath.Join(string(filepath.Separator), "tmp", "out")
	if got := resolveOut(root, abs); got != abs {
		t.Fatalf("绝对 OUT 应原样返回：%s", got)
	}
}
