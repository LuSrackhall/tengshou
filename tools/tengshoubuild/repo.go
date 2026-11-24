package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// repoRoot 定位仓库根目录（含 go.mod 的目录）。
//
// 优先从当前工作目录向上找：`go run ./tools/tengshoubuild ...` 通常在仓库根
// 执行。找不到时回退到源码位置（go run 编译进去的路径），这样从仓库外调用
// 也能工作。
func repoRoot() (string, error) {
	if dir, err := os.Getwd(); err == nil {
		for {
			if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
				return dir, nil
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	if _, file, _, ok := runtime.Caller(0); ok {
		root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			return root, nil
		}
	}
	return "", fmt.Errorf("无法定位仓库根目录（未找到 go.mod）")
}

// resolveOut 把 OUT 环境变量解析成绝对路径：相对路径按仓库根解释，
// 与旧脚本先 cd 到仓库根再使用 OUT 的行为一致。
func resolveOut(root, out string) string {
	if filepath.IsAbs(out) {
		return filepath.Clean(out)
	}
	return filepath.Join(root, out)
}
