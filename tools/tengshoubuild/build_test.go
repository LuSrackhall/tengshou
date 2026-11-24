package main

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// emptyEnv 模拟未设置任何环境变量的构建环境。
func emptyEnv(string) string { return "" }

func envFrom(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func failGit(...string) (string, error) { return "", errors.New("git 不可用") }

func TestResolveBuildConfigDefaults(t *testing.T) {
	now := time.Date(2026, 11, 24, 7, 0, 0, 0, time.UTC)
	cfg, err := resolveBuildConfig("all", "", "", emptyEnv, "darwin", "arm64", failGit, func() time.Time { return now })
	if err != nil {
		t.Fatalf("resolveBuildConfig: %v", err)
	}
	if cfg.target != "all" || cfg.goos != "darwin" || cfg.goarch != "arm64" {
		t.Fatalf("目标解析错误：%+v", cfg)
	}
	if cfg.cgoEnabled != "" {
		t.Fatalf("cgo 未显式设置时应为空，实际 %q", cfg.cgoEnabled)
	}
	if cfg.version != "0.1.0-dev" || cfg.commit != "unknown" {
		t.Fatalf("git 不可用时应回退默认值：version=%q commit=%q", cfg.version, cfg.commit)
	}
	if cfg.buildDate != "2026-11-24T07:00:00Z" {
		t.Fatalf("BUILD_DATE 默认值错误：%q", cfg.buildDate)
	}
	if cfg.outDir != "dist" || cfg.nameSuffix != "" {
		t.Fatalf("OUT / NAME_SUFFIX 默认值错误：%+v", cfg)
	}
}

func TestResolveBuildConfigEnvOverrides(t *testing.T) {
	env := envFrom(map[string]string{
		"VERSION":     "v1.2.3",
		"COMMIT":      "abc1234",
		"BUILD_DATE":  "2026-01-02T03:04:05Z",
		"OUT":         "out-dir",
		"NAME_SUFFIX": "-linux-amd64",
		"CGO_ENABLED": "0",
		"GOOS":        "linux",
		"GOARCH":      "amd64",
	})
	cfg, err := resolveBuildConfig("cli", "", "", env, "darwin", "arm64", failGit, time.Now)
	if err != nil {
		t.Fatalf("resolveBuildConfig: %v", err)
	}
	if cfg.version != "v1.2.3" || cfg.commit != "abc1234" || cfg.buildDate != "2026-01-02T03:04:05Z" {
		t.Fatalf("版本信息未按环境变量解析：%+v", cfg)
	}
	if cfg.goos != "linux" || cfg.goarch != "amd64" {
		t.Fatalf("GOOS/GOARCH 未按环境变量解析：%+v", cfg)
	}
	if cfg.outDir != "out-dir" || cfg.nameSuffix != "-linux-amd64" {
		t.Fatalf("OUT / NAME_SUFFIX 未按环境变量解析：%+v", cfg)
	}
}

func TestResolveBuildConfigFlagsBeatEnv(t *testing.T) {
	env := envFrom(map[string]string{"GOOS": "linux", "GOARCH": "amd64"})
	cfg, err := resolveBuildConfig("cli", "darwin", "arm64", env, "linux", "amd64", failGit, time.Now)
	if err != nil {
		t.Fatalf("resolveBuildConfig: %v", err)
	}
	if cfg.goos != "darwin" || cfg.goarch != "arm64" {
		t.Fatalf("参数应覆盖环境变量：%+v", cfg)
	}
}

func TestResolveBuildConfigGitDefaults(t *testing.T) {
	git := func(args ...string) (string, error) {
		switch args[0] {
		case "describe":
			return "v0.9.0", nil
		case "rev-parse":
			return "deadbee", nil
		}
		return "", errors.New("unexpected")
	}
	cfg, err := resolveBuildConfig("cli", "", "", emptyEnv, "darwin", "arm64", git, time.Now)
	if err != nil {
		t.Fatalf("resolveBuildConfig: %v", err)
	}
	if cfg.version != "v0.9.0" || cfg.commit != "deadbee" {
		t.Fatalf("git 默认值未采用：%+v", cfg)
	}
}

func TestResolveBuildConfigWindows(t *testing.T) {
	cfg, err := resolveBuildConfig("all", "windows", "", emptyEnv, "darwin", "arm64", failGit, time.Now)
	if err != nil {
		t.Fatalf("resolveBuildConfig: %v", err)
	}
	if cfg.goos != "windows" {
		t.Fatalf("GOOS 应为 windows：%+v", cfg)
	}
	// Windows 未指定架构时默认 amd64（与旧 build-windows.ps1 一致）。
	if cfg.goarch != "amd64" {
		t.Fatalf("Windows 默认架构应为 amd64：%+v", cfg)
	}
	// Windows 固定关闭 cgo，即使用户显式设置 1。
	if cfg.cgoEnabled != "0" {
		t.Fatalf("Windows 应固定 CGO_ENABLED=0：%+v", cfg)
	}
}

func TestResolveBuildConfigWindowsExplicitArch(t *testing.T) {
	cfg, err := resolveBuildConfig("all", "windows", "arm64", emptyEnv, "darwin", "arm64", failGit, time.Now)
	if err != nil {
		t.Fatalf("resolveBuildConfig: %v", err)
	}
	if cfg.goarch != "arm64" {
		t.Fatalf("显式架构应被采用：%+v", cfg)
	}
}

func TestResolveBuildConfigDarwinDesktopRequiresCGO(t *testing.T) {
	env := envFrom(map[string]string{"CGO_ENABLED": "0"})
	if _, err := resolveBuildConfig("desktop", "", "", env, "darwin", "arm64", failGit, time.Now); err == nil {
		t.Fatal("macOS 桌面壳 + CGO_ENABLED=0 应失败")
	}
	// CLI 不依赖 cgo，允许 CGO_ENABLED=0。
	if _, err := resolveBuildConfig("cli", "", "", env, "darwin", "arm64", failGit, time.Now); err != nil {
		t.Fatalf("darwin CLI 允许 CGO_ENABLED=0：%v", err)
	}
}

func TestResolveBuildConfigRejectsUnknownTarget(t *testing.T) {
	if _, err := resolveBuildConfig("server", "", "", emptyEnv, "darwin", "arm64", failGit, time.Now); err == nil {
		t.Fatal("未知构建目标应失败")
	}
}

func testConfig(t *testing.T, target, goos, goarch string) buildConfig {
	t.Helper()
	cfg, err := resolveBuildConfig(target, goos, goarch, envFrom(map[string]string{
		"VERSION":     "v1.0.0",
		"COMMIT":      "abc1234",
		"BUILD_DATE":  "2026-01-01T00:00:00Z",
		"NAME_SUFFIX": "-" + goos + "-" + goarch,
	}), "darwin", "arm64", failGit, time.Now)
	if err != nil {
		t.Fatalf("resolveBuildConfig: %v", err)
	}
	return cfg
}

func TestBuildJobsDarwin(t *testing.T) {
	cfg := testConfig(t, "all", "darwin", "arm64")
	jobs, err := buildJobs(cfg)
	if err != nil {
		t.Fatalf("buildJobs: %v", err)
	}
	if len(jobs) != 2 {
		t.Fatalf("all 应产出 2 个任务，实际 %d", len(jobs))
	}
	if jobs[0].output != "dist/tengshou-darwin-arm64" || jobs[0].pkg != "./cmd/tengshou" {
		t.Fatalf("桌面任务错误：%+v", jobs[0])
	}
	if jobs[1].output != "dist/tengshou-cli-darwin-arm64" || jobs[1].pkg != "./cmd/tengshou-cli" {
		t.Fatalf("CLI 任务错误：%+v", jobs[1])
	}
	for _, job := range jobs {
		if strings.Contains(job.ldflags, "windowsgui") {
			t.Fatalf("非 Windows 不应带 GUI 子系统：%+v", job)
		}
		if !strings.Contains(job.ldflags, "-s -w") ||
			!strings.Contains(job.ldflags, versionPkg+".Version=v1.0.0") ||
			!strings.Contains(job.ldflags, versionPkg+".Commit=abc1234") ||
			!strings.Contains(job.ldflags, versionPkg+".BuildDate=2026-01-01T00:00:00Z") {
			t.Fatalf("ldflags 缺少版本信息：%+v", job)
		}
		if job.goos != "darwin" || job.goarch != "arm64" {
			t.Fatalf("任务目标平台错误：%+v", job)
		}
	}
}

func TestBuildJobsWindowsSubsystems(t *testing.T) {
	cfg := testConfig(t, "all", "windows", "amd64")
	jobs, err := buildJobs(cfg)
	if err != nil {
		t.Fatalf("buildJobs: %v", err)
	}
	if len(jobs) != 2 {
		t.Fatalf("all 应产出 2 个任务，实际 %d", len(jobs))
	}
	desktop, cli := jobs[0], jobs[1]
	if desktop.output != "dist/tengshou-windows-amd64.exe" {
		t.Fatalf("Windows 桌面产物名错误：%s", desktop.output)
	}
	if !strings.Contains(desktop.ldflags, "-H windowsgui") {
		t.Fatalf("Windows 桌面必须带 -H windowsgui：%s", desktop.ldflags)
	}
	if cli.output != "dist/tengshou-cli-windows-amd64.exe" {
		t.Fatalf("Windows CLI 产物名错误：%s", cli.output)
	}
	if strings.Contains(cli.ldflags, "windowsgui") {
		t.Fatalf("Windows CLI 必须保留控制台子系统：%s", cli.ldflags)
	}
	for _, job := range jobs {
		if job.cgo != "0" {
			t.Fatalf("Windows 任务必须固定 cgo=0：%+v", job)
		}
	}
}

func TestBuildJobsTargetSelection(t *testing.T) {
	for _, test := range []struct {
		target string
		want   int
	}{
		{"desktop", 1},
		{"cli", 1},
		{"all", 2},
	} {
		jobs, err := buildJobs(testConfig(t, test.target, "linux", "amd64"))
		if err != nil {
			t.Fatalf("buildJobs(%s): %v", test.target, err)
		}
		if len(jobs) != test.want {
			t.Fatalf("目标 %s 期望 %d 个任务，实际 %d", test.target, test.want, len(jobs))
		}
	}
}

func TestEnvWithReplacesAndDrops(t *testing.T) {
	env := envWith([]string{"PATH=/bin", "GOOS=darwin", "CGO_ENABLED=1", "KEEP=1"}, map[string]string{
		"GOOS":        "windows",
		"CGO_ENABLED": "",
	})
	joined := strings.Join(env, "\n")
	if strings.Contains(joined, "GOOS=darwin") {
		t.Fatalf("旧 GOOS 未移除：\n%s", joined)
	}
	if !strings.Contains(joined, "GOOS=windows") {
		t.Fatalf("新 GOOS 未写入：\n%s", joined)
	}
	if strings.Contains(joined, "CGO_ENABLED=") {
		t.Fatalf("空值应删除变量：\n%s", joined)
	}
	if !strings.Contains(joined, "PATH=/bin") || !strings.Contains(joined, "KEEP=1") {
		t.Fatalf("无关变量被误删：\n%s", joined)
	}
}
