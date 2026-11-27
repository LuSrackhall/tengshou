package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// 发版资产策略：版本号（git tag）是 Release 内容的唯一事实源。
//
// 只允许两类 tag，其余一律发版失败：
//   - 正式版 vX.Y.Z
//   - 预发布 vX.Y.Z-(alpha|beta|rc).N
//
// 正式版发布 5 个资产（4 个客户端 + 1 个教程包）；预发布再加三端各一个
// 业务验收包，共 8 个。planRelease 的产物同时供 CI 的 acceptance 与
// release 两个 job 使用，CI 不再手写第二份资产清单。
const (
	stableTagPattern     = `^v[0-9]+\.[0-9]+\.[0-9]+$`
	prereleaseTagPattern = `^v[0-9]+\.[0-9]+\.[0-9]+-(alpha|beta|rc)\.[0-9]+$`

	// buildArtifactTotal 是 release job 组装完成后 dist 里的构建产物总数：
	// 3 个 CLI + 4 个桌面文件 + Windows 分发 zip + 教程包 = 9。
	buildArtifactTotal = 9
	// acceptancePackageTotal 是预发布额外带来的验收包数（三端各一个）。
	acceptancePackageTotal = 3
)

// acceptancePlatforms 与 e2e 矩阵的 artifact 后缀一致，顺序即发布清单顺序。
var acceptancePlatforms = []string{"linux-chromium", "macos-webkit", "windows-chromium"}

var (
	stableTagRE     = regexp.MustCompile(stableTagPattern)
	prereleaseTagRE = regexp.MustCompile(prereleaseTagPattern)
)

// releasePlan 是一次发布应产出的资产清单（文件名相对 dist/）。
type releasePlan struct {
	tag        string
	prerelease bool
	files      []string
}

// planRelease 校验 tag 并推导发布清单。非法 tag 返回错误（发版必须失败）。
func planRelease(tag string) (releasePlan, error) {
	prerelease := false
	switch {
	case stableTagRE.MatchString(tag):
	case prereleaseTagRE.MatchString(tag):
		prerelease = true
	default:
		return releasePlan{}, fmt.Errorf(
			"非法版本 tag %q：只允许 vX.Y.Z（正式）或 vX.Y.Z-(alpha|beta|rc).N（预发布）", tag)
	}

	files := []string{
		windowsZipName,
		windowsExeName,
		"tengshou-darwin-arm64",
		"tengshou-darwin-arm64.tar.gz",
		"tengshou-tutorial-" + tag + ".zip",
	}
	if prerelease {
		for _, platform := range acceptancePlatforms {
			files = append(files, "tengshou-acceptance-"+tag+"-"+platform+".zip")
		}
	}
	return releasePlan{tag: tag, prerelease: prerelease, files: files}, nil
}

// expectedTotal 返回 release job 断言的 dist 文件总数：正式 9，预发布 12。
func (p releasePlan) expectedTotal() int {
	if p.prerelease {
		return buildArtifactTotal + acceptancePackageTotal
	}
	return buildArtifactTotal
}

// outputs 渲染成可直接追加到 $GITHUB_OUTPUT 的格式。heredoc 分隔符固定为
// EOF：清单全部是仓库可控的 ASCII 文件名，不会与分隔符冲突。
func (p releasePlan) outputs() string {
	var b strings.Builder
	fmt.Fprintf(&b, "prerelease=%t\n", p.prerelease)
	fmt.Fprintf(&b, "expected_total=%d\n", p.expectedTotal())
	b.WriteString("release_files<<EOF\n")
	for _, file := range p.files {
		b.WriteString(file)
		b.WriteString("\n")
	}
	b.WriteString("EOF\n")
	return b.String()
}

// resolveReleaseTag 解析版本 tag：--tag 优先，其次 GITHUB_REF_NAME，最后 VERSION。
// 三者都为空时返回明确的失败原因，避免 CI 静默发布错误版本。
func resolveReleaseTag(flagValue string, getenv func(string) string) (string, error) {
	tag := firstNonEmpty(flagValue, getenv("GITHUB_REF_NAME"), getenv("VERSION"))
	if tag == "" {
		return "", errors.New("缺少版本 tag：请用 --tag 指定，或设置 GITHUB_REF_NAME / VERSION")
	}
	return tag, nil
}

// runReleasePlan 输出发布计划（GITHUB_OUTPUT 格式到 stdout，人类可读摘要到
// stderr），供 CI 追加到 $GITHUB_OUTPUT 或本地核对。
func runReleasePlan(args []string) error {
	fs := flag.NewFlagSet("release-plan", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	tagFlag := fs.String("tag", "", "版本 tag（默认读 GITHUB_REF_NAME / VERSION）")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "用法：go run ./tools/tengshoubuild release-plan [--tag vX.Y.Z]")
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return errors.New("release-plan 不接受位置参数")
	}
	tag, err := resolveReleaseTag(*tagFlag, os.Getenv)
	if err != nil {
		return err
	}
	plan, err := planRelease(tag)
	if err != nil {
		return err
	}
	fmt.Print(plan.outputs())
	kind := "正式"
	if plan.prerelease {
		kind = "预发布"
	}
	fmt.Fprintf(os.Stderr, "发布计划：%s（%s），发布资产 %d 个，dist 预计 %d 个文件\n",
		plan.tag, kind, len(plan.files), plan.expectedTotal())
	return nil
}
