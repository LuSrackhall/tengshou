// Package main 是腾手仓库唯一的构建 / 冒烟 / 打包编排入口。
//
// 三端（Linux / macOS / Windows）的开发与 CI 调用同一命令：
//
//	go run ./tools/tengshoubuild build [-goos GOOS] [-goarch GOARCH] [desktop|cli|all]
//	go run ./tools/tengshoubuild smoke-mcp <被测二进制> <tengshou-cli>
//	go run ./tools/tengshoubuild package-windows-zip [dist 目录]
//
// 历史背景：构建与冒烟曾由 build.sh / build-windows.ps1 /
// smoke-mcp-desktop.sh / smoke-mcp-desktop.ps1 / package-windows-zip.sh 两套脚本
// 维护。alpha.10 前 Windows CI 失败的根因正是双实现漂移（PowerShell 5.1 按 ANSI
// 解码无 BOM 的 UTF-8 文件，构建静默空跑）。现在构建 flags、冒烟断言与 zip
// 内容只在 Go 工具这一处维护；工具只用标准库，不被 cmd/tengshou 或
// cmd/tengshou-cli 引用。
package main

import (
	"fmt"
	"os"
)

const usage = `用法：
  go run ./tools/tengshoubuild build [-goos GOOS] [-goarch GOARCH] [desktop|cli|all]
  go run ./tools/tengshoubuild smoke-mcp <被测二进制> <tengshou-cli>
  go run ./tools/tengshoubuild package-windows-zip [dist 目录]

build 环境变量：
  VERSION       版本号（默认 git describe --tags --abbrev=0，否则 0.1.0-dev）
  COMMIT        提交号（默认 git rev-parse --short HEAD，否则 unknown）
  BUILD_DATE    构建时间（默认当前 UTC 时间）
  OUT           输出目录（默认 dist）
  GOOS/GOARCH   目标平台（默认本机；Windows 未指定架构时默认 amd64）
  CGO_ENABLED   cgo 开关（Windows 固定 0；macOS 桌面壳要求 1）
  NAME_SUFFIX   产物名后缀，例如 -darwin-arm64

交叉构建必须用 -goos / -goarch 参数：go run 自身会按 GOOS/GOARCH 编译工具，
环境变量里的交叉目标无法在本机执行。
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "错误：%v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("缺少子命令\n%s", usage)
	}
	switch args[0] {
	case "build":
		return runBuild(args[1:])
	case "smoke-mcp":
		return runSmokeMCP(args[1:])
	case "package-windows-zip":
		return runPackageWindowsZip(args[1:])
	case "-h", "--help", "help":
		fmt.Print(usage)
		return nil
	default:
		return fmt.Errorf("未知子命令 %q\n%s", args[0], usage)
	}
}
