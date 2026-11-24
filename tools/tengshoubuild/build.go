package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

// buildDateLayout 与旧脚本一致：UTC、秒级、字面量 Z 结尾。
const buildDateLayout = "2006-01-02T15:04:05Z"

// versionPkg 是三个构建信息变量所在的包，ldflags 只在这里一处注入。
const versionPkg = "github.com/LuSrackhall/tengshou/internal/version"

// buildConfig 是解析完环境变量与参数后的构建参数。字段与旧脚本的环境变量
// 契约一一对应，行为等价是实现目标（不改变产物名、ldflags 与子系统）。
type buildConfig struct {
	target     string // desktop / cli / all
	goos       string
	goarch     string
	cgoEnabled string // 空表示不显式设置，交给 go 决定
	version    string
	commit     string
	buildDate  string
	outDir     string
	nameSuffix string
}

// buildJob 描述一次 go build：目标包、产物路径与完整的 ldflags。
type buildJob struct {
	label   string
	pkg     string
	output  string
	ldflags string
	goos    string
	goarch  string
	cgo     string
}

// gitCommand 抽象只读 git 查询，便于测试注入。
type gitCommand func(args ...string) (string, error)

func runBuild(args []string) error {
	fs := flag.NewFlagSet("build", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	goosFlag := fs.String("goos", "", "目标 GOOS（默认本机或环境变量 GOOS）")
	goarchFlag := fs.String("goarch", "", "目标 GOARCH（默认本机或环境变量 GOARCH）")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "用法：go run ./tools/tengshoubuild build [-goos GOOS] [-goarch GOARCH] [desktop|cli|all]")
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 1 {
		return fmt.Errorf("build 只接受一个目标（desktop|cli|all），收到 %d 个参数", fs.NArg())
	}
	target := "all"
	if fs.NArg() == 1 {
		target = fs.Arg(0)
	}

	cfg, err := resolveBuildConfig(target, *goosFlag, *goarchFlag, os.Getenv, runtime.GOOS, runtime.GOARCH, gitOutput, time.Now)
	if err != nil {
		return err
	}
	// 与旧脚本先 cd 到仓库根一致：OUT 的相对路径按仓库根解释，go build 也在
	// 仓库根执行，这样从子目录调用也能得到同样的产物位置。
	root, err := repoRoot()
	if err != nil {
		return err
	}
	cfg.outDir = resolveOut(root, cfg.outDir)
	jobs, err := buildJobs(cfg)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(cfg.outDir, 0o755); err != nil {
		return fmt.Errorf("创建输出目录 %s: %w", cfg.outDir, err)
	}
	for _, job := range jobs {
		if err := runBuildJob(root, job); err != nil {
			return err
		}
	}
	printBuildResult(cfg, jobs)
	return nil
}

// resolveBuildConfig 合并参数与环境变量，产出确定的构建参数。
// 规则沿用旧脚本：VERSION/COMMIT/BUILD_DATE 有默认回退，OUT 默认 dist；
// Windows 固定 CGO_ENABLED=0 且默认 amd64；macOS 桌面壳要求 cgo。
func resolveBuildConfig(
	target, flagGOOS, flagGOARCH string,
	getenv func(string) string,
	hostGOOS, hostGOARCH string,
	git gitCommand,
	now func() time.Time,
) (buildConfig, error) {
	switch target {
	case "desktop", "cli", "all":
	default:
		return buildConfig{}, fmt.Errorf("未知构建目标 %q（可选 desktop|cli|all）", target)
	}

	goos := firstNonEmpty(flagGOOS, getenv("GOOS"), hostGOOS)
	goarch := firstNonEmpty(flagGOARCH, getenv("GOARCH"))
	if goarch == "" {
		goarch = hostGOARCH
		if goos == "windows" {
			// 与旧 build-windows.ps1 一致：Windows 未指定架构时默认 amd64，
			// 不随构建机架构变化（发布资产名固定 -windows-amd64）。
			goarch = "amd64"
		}
	}

	cgoEnabled := getenv("CGO_ENABLED")
	if goos == "windows" {
		// Wails v3 在 Windows 使用纯 Go WebView2 桥，不需要 cgo；
		// 与旧 build-windows.ps1 一致强制关闭，避免 mingw 工具链依赖。
		cgoEnabled = "0"
	}
	if target != "cli" && goos == "darwin" && cgoEnabled == "0" {
		return buildConfig{}, errors.New("macOS 桌面壳需要 CGO_ENABLED=1（Wails v3 依赖 cgo）；请设置 CGO_ENABLED=1 或改用 build cli")
	}

	version := getenv("VERSION")
	if version == "" {
		if v, err := git("describe", "--tags", "--abbrev=0"); err == nil {
			version = v
		} else {
			version = "0.1.0-dev"
		}
	}
	commit := getenv("COMMIT")
	if commit == "" {
		if c, err := git("rev-parse", "--short", "HEAD"); err == nil {
			commit = c
		} else {
			commit = "unknown"
		}
	}
	buildDate := getenv("BUILD_DATE")
	if buildDate == "" {
		buildDate = now().UTC().Format(buildDateLayout)
	}
	outDir := getenv("OUT")
	if outDir == "" {
		outDir = "dist"
	}

	return buildConfig{
		target:     target,
		goos:       goos,
		goarch:     goarch,
		cgoEnabled: cgoEnabled,
		version:    version,
		commit:     commit,
		buildDate:  buildDate,
		outDir:     outDir,
		nameSuffix: getenv("NAME_SUFFIX"),
	}, nil
}

// buildJobs 生成具体的 go build 任务。构建 flags 只在这一处维护。
func buildJobs(cfg buildConfig) ([]buildJob, error) {
	desktopOutput, cliOutput := artifactNames(cfg)
	var jobs []buildJob
	if cfg.target == "desktop" || cfg.target == "all" {
		extra := ""
		if cfg.goos == "windows" {
			// 桌面壳用 Windows GUI 子系统。
			extra = "-H windowsgui"
		}
		jobs = append(jobs, buildJob{
			label:   "桌面应用 " + cfg.goos + "/" + cfg.goarch,
			pkg:     "./cmd/tengshou",
			output:  filepath.Join(cfg.outDir, desktopOutput),
			ldflags: linkFlags(cfg, extra),
			goos:    cfg.goos,
			goarch:  cfg.goarch,
			cgo:     cfg.cgoEnabled,
		})
	}
	if cfg.target == "cli" || cfg.target == "all" {
		// CLI 必须保留控制台子系统（Windows 上不能加 -H windowsgui），
		// 否则 `tengshou-cli-windows-amd64.exe version` 在 cmd/PowerShell
		// 里看不到输出。
		jobs = append(jobs, buildJob{
			label:   "tengshou-cli " + cfg.goos + "/" + cfg.goarch,
			pkg:     "./cmd/tengshou-cli",
			output:  filepath.Join(cfg.outDir, cliOutput),
			ldflags: linkFlags(cfg, ""),
			goos:    cfg.goos,
			goarch:  cfg.goarch,
			cgo:     cfg.cgoEnabled,
		})
	}
	if len(jobs) == 0 {
		return nil, fmt.Errorf("构建目标 %q 没有可执行的构建任务", cfg.target)
	}
	return jobs, nil
}

// linkFlags 拼出完整的 -ldflags 值：版本信息只在 versionPkg 注入。
// extra 用于追加 -H windowsgui 这类子系统 flags。
func linkFlags(cfg buildConfig, extra string) string {
	flags := "-s -w"
	if extra != "" {
		flags += " " + extra
	}
	flags += " -X " + versionPkg + ".Version=" + cfg.version
	flags += " -X " + versionPkg + ".Commit=" + cfg.commit
	flags += " -X " + versionPkg + ".BuildDate=" + cfg.buildDate
	return flags
}

// artifactNames 返回桌面与 CLI 产物文件名。Windows 沿用旧
// build-windows.ps1 的固定命名 tengshou-windows-<arch>.exe /
// tengshou-cli-windows-<arch>.exe（该脚本不识别 NAME_SUFFIX，保持一致）；
// 其他平台是 tengshou<NAME_SUFFIX> / tengshou-cli<NAME_SUFFIX>。
func artifactNames(cfg buildConfig) (desktop, cli string) {
	if cfg.goos == "windows" {
		return "tengshou-windows-" + cfg.goarch + ".exe",
			"tengshou-cli-windows-" + cfg.goarch + ".exe"
	}
	return "tengshou" + cfg.nameSuffix, "tengshou-cli" + cfg.nameSuffix
}

func runBuildJob(root string, job buildJob) error {
	fmt.Printf("构建%s ...\n", job.label)
	cmd := exec.Command("go", "build", "-trimpath",
		"-ldflags", job.ldflags, "-o", job.output, job.pkg)
	cmd.Dir = root
	cmd.Env = envWith(os.Environ(), map[string]string{
		"GOOS":        job.goos,
		"GOARCH":      job.goarch,
		"CGO_ENABLED": job.cgo,
	})
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("构建%s失败: %w", job.label, err)
	}
	// go build 非零退出已被上面捕获；这里再断言产物存在，防止“成功”空跑
	// （Windows PowerShell 5.1 曾出现同类静默失败）。
	if info, err := os.Stat(job.output); err != nil || info.IsDir() {
		return fmt.Errorf("构建产物缺失：%s", job.output)
	}
	return nil
}

// envWith 用 overrides 覆盖环境变量列表；值为空表示删除该变量。
func envWith(base []string, overrides map[string]string) []string {
	keys := make([]string, 0, len(overrides))
	for key := range overrides {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	env := make([]string, 0, len(base)+len(overrides))
	for _, kv := range base {
		key, _, _ := strings.Cut(kv, "=")
		if _, ok := overrides[key]; ok {
			continue
		}
		env = append(env, kv)
	}
	for _, key := range keys {
		if overrides[key] == "" {
			continue
		}
		env = append(env, key+"="+overrides[key])
	}
	return env
}

// gitOutput 执行只读 git 命令并返回去掉首尾空白的单行输出。
func gitOutput(args ...string) (string, error) {
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return "", err
	}
	line := strings.TrimSpace(string(out))
	if line == "" {
		return "", errors.New("git 输出为空")
	}
	return line, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func printBuildResult(cfg buildConfig, jobs []buildJob) {
	fmt.Println("完成：")
	if entries, err := os.ReadDir(cfg.outDir); err == nil {
		for _, entry := range entries {
			info, err := entry.Info()
			if err != nil || info.IsDir() {
				continue
			}
			fmt.Printf("  %s（%d 字节）\n", entry.Name(), info.Size())
		}
	}
	desktop, cli := artifactNames(cfg)
	fmt.Println()
	fmt.Println("运行示例：")
	fmt.Printf("  %s demo --data-dir ./data\n", filepath.Join(cfg.outDir, cli))
	fmt.Printf("  %s serve --data-dir ./data\n", filepath.Join(cfg.outDir, cli))
	fmt.Printf("  %s mcp --data-dir ./data\n", filepath.Join(cfg.outDir, desktop))
}
