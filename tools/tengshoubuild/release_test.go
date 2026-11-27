package main

import (
	"slices"
	"strings"
	"testing"
)

func TestPlanReleaseStableTags(t *testing.T) {
	want := []string{
		"tengshou-windows-amd64.zip",
		"tengshou-windows-amd64.exe",
		"tengshou-darwin-arm64",
		"tengshou-darwin-arm64.tar.gz",
		"tengshou-tutorial-v1.2.3.zip",
	}
	for _, tag := range []string{"v1.2.3", "v0.1.0", "v10.20.30"} {
		plan, err := planRelease(tag)
		if err != nil {
			t.Fatalf("planRelease(%q): %v", tag, err)
		}
		if plan.prerelease {
			t.Fatalf("planRelease(%q) 不应判定为预发布", tag)
		}
		expected := slices.Clone(want)
		expected[4] = "tengshou-tutorial-" + tag + ".zip"
		if !slices.Equal(plan.files, expected) {
			t.Fatalf("planRelease(%q) 发布清单不符：\n得到 %v\n期望 %v", tag, plan.files, expected)
		}
		if got := plan.expectedTotal(); got != 9 {
			t.Fatalf("planRelease(%q) expectedTotal = %d，期望 9", tag, got)
		}
	}
}

func TestPlanReleasePrereleaseTags(t *testing.T) {
	// v1.2.3-alpha.0 明确允许（REQ-1 测试清单）。
	tags := []string{"v0.1.0-alpha.28", "v1.2.3-alpha.0", "v2.0.0-beta.10", "v3.1.4-rc.2"}
	for _, tag := range tags {
		plan, err := planRelease(tag)
		if err != nil {
			t.Fatalf("planRelease(%q): %v", tag, err)
		}
		if !plan.prerelease {
			t.Fatalf("planRelease(%q) 应判定为预发布", tag)
		}
		want := []string{
			"tengshou-windows-amd64.zip",
			"tengshou-windows-amd64.exe",
			"tengshou-darwin-arm64",
			"tengshou-darwin-arm64.tar.gz",
			"tengshou-tutorial-" + tag + ".zip",
			"tengshou-acceptance-" + tag + "-linux-chromium.zip",
			"tengshou-acceptance-" + tag + "-macos-webkit.zip",
			"tengshou-acceptance-" + tag + "-windows-chromium.zip",
		}
		if !slices.Equal(plan.files, want) {
			t.Fatalf("planRelease(%q) 发布清单不符：\n得到 %v\n期望 %v", tag, plan.files, want)
		}
		if got := plan.expectedTotal(); got != 12 {
			t.Fatalf("planRelease(%q) expectedTotal = %d，期望 12", tag, got)
		}
	}
}

func TestPlanReleaseRejectsInvalidTags(t *testing.T) {
	invalid := []string{
		"",
		"v1.2",
		"0.1.0",
		"V1.2.3",
		"v1.2.3.4",
		"v1.2.3-alpha",
		"v1.2.3-rc.",
		"v1.2.3-alpha.1.2",
		"v1.2.3-custom.1",
		"v1.2.3-alpha.1+build.2",
		"v1.2.3-beta.01x",
		"release-v1.2.3",
	}
	for _, tag := range invalid {
		if _, err := planRelease(tag); err == nil {
			t.Fatalf("planRelease(%q) 应报错", tag)
		} else if !strings.Contains(err.Error(), "非法版本 tag") {
			t.Fatalf("planRelease(%q) 错误信息应说明 tag 非法，实际：%v", tag, err)
		}
	}
}

func TestPlanReleaseNeverIncludesCLI(t *testing.T) {
	for _, tag := range []string{"v1.2.3", "v1.2.3-alpha.1"} {
		plan, err := planRelease(tag)
		if err != nil {
			t.Fatalf("planRelease(%q): %v", tag, err)
		}
		for _, file := range plan.files {
			if strings.HasPrefix(file, "tengshou-cli") {
				t.Fatalf("planRelease(%q) 发布清单不得包含 CLI，实际含 %q", tag, file)
			}
		}
	}
}

func TestReleasePlanOutputsGitHubFormat(t *testing.T) {
	plan, err := planRelease("v0.1.0-alpha.28")
	if err != nil {
		t.Fatalf("planRelease: %v", err)
	}
	want := strings.Join([]string{
		"prerelease=true",
		"expected_total=12",
		"release_files<<EOF",
		"tengshou-windows-amd64.zip",
		"tengshou-windows-amd64.exe",
		"tengshou-darwin-arm64",
		"tengshou-darwin-arm64.tar.gz",
		"tengshou-tutorial-v0.1.0-alpha.28.zip",
		"tengshou-acceptance-v0.1.0-alpha.28-linux-chromium.zip",
		"tengshou-acceptance-v0.1.0-alpha.28-macos-webkit.zip",
		"tengshou-acceptance-v0.1.0-alpha.28-windows-chromium.zip",
		"EOF",
		"",
	}, "\n")
	if got := plan.outputs(); got != want {
		t.Fatalf("outputs 不符：\n得到 %q\n期望 %q", got, want)
	}

	stable, err := planRelease("v0.1.0")
	if err != nil {
		t.Fatalf("planRelease: %v", err)
	}
	if got := stable.outputs(); !strings.Contains(got, "prerelease=false\nexpected_total=9\n") {
		t.Fatalf("正式版 outputs 不符：%q", got)
	}
}

func TestResolveReleaseTag(t *testing.T) {
	env := func(values map[string]string) func(string) string {
		return func(key string) string { return values[key] }
	}

	tag, err := resolveReleaseTag("v9.9.9", env(map[string]string{"GITHUB_REF_NAME": "v1.0.0", "VERSION": "v2.0.0"}))
	if err != nil || tag != "v9.9.9" {
		t.Fatalf("--tag 应优先，得到 %q, %v", tag, err)
	}
	tag, err = resolveReleaseTag("", env(map[string]string{"GITHUB_REF_NAME": "v1.0.0", "VERSION": "v2.0.0"}))
	if err != nil || tag != "v1.0.0" {
		t.Fatalf("GITHUB_REF_NAME 应优先于 VERSION，得到 %q, %v", tag, err)
	}
	tag, err = resolveReleaseTag("", env(map[string]string{"VERSION": "v2.0.0"}))
	if err != nil || tag != "v2.0.0" {
		t.Fatalf("应回退到 VERSION，得到 %q, %v", tag, err)
	}
	if _, err = resolveReleaseTag("", env(nil)); err == nil || !strings.Contains(err.Error(), "缺少版本 tag") {
		t.Fatalf("三者皆空应报错，实际：%v", err)
	}
}

func TestRunReleasePlanErrors(t *testing.T) {
	if err := runReleasePlan([]string{"--tag", "v1.2"}); err == nil || !strings.Contains(err.Error(), "非法版本 tag") {
		t.Fatalf("非法 tag 应报错，实际：%v", err)
	}
	if err := runReleasePlan([]string{"v1.2.3"}); err == nil || !strings.Contains(err.Error(), "位置参数") {
		t.Fatalf("位置参数应报错，实际：%v", err)
	}
	t.Setenv("GITHUB_REF_NAME", "")
	t.Setenv("VERSION", "")
	if err := runReleasePlan(nil); err == nil || !strings.Contains(err.Error(), "缺少版本 tag") {
		t.Fatalf("缺少 tag 应报错，实际：%v", err)
	}
}
