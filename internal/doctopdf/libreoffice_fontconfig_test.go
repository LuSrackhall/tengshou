package doctopdf

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestMacOSFontsConfListsSystemDirs(t *testing.T) {
	conf := macOSFontsConf("/tmp/cache & dir")
	for _, want := range []string{
		"<dir>/System/Library/Fonts</dir>",
		"<dir>/System/Library/Fonts/Supplemental</dir>",
		"<dir>/Library/Fonts</dir>",
		"<dir>/System/Library/AssetsV2</dir>",
		"<cachedir>/tmp/cache &amp; dir</cachedir>",
	} {
		if !strings.Contains(conf, want) {
			t.Fatalf("fonts.conf 缺少 %q：\n%s", want, conf)
		}
	}
	if !strings.HasPrefix(conf, "<?xml version=\"1.0\"?>\n<fontconfig>\n") {
		t.Fatalf("fonts.conf 头部不正确：\n%s", conf)
	}
}

func TestFontConfigEnvRespectsUserConfig(t *testing.T) {
	t.Setenv("FONTCONFIG_FILE", "/custom/fonts.conf")
	if got := fontConfigEnv(t.TempDir()); len(got) != 0 {
		t.Fatalf("用户已设置 FONTCONFIG_FILE 时不得覆盖，got %v", got)
	}
}

func TestFontConfigEnvFallbacksWhenUnwritable(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS 专用：只有 darwin 需要显式字体配置")
	}
	t.Setenv("FONTCONFIG_FILE", "")

	// 缓存目录无法创建：tempDir 的父路径是一个普通文件。
	base := t.TempDir()
	blocker := filepath.Join(base, "blocked")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := fontConfigEnv(filepath.Join(blocker, "child")); len(got) != 0 {
		t.Fatalf("缓存目录创建失败时应回退默认配置，got %v", got)
	}

	// 配置写入失败：tempDir/fonts.conf 已被一个目录占用。
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "fonts.conf"), 0o700); err != nil {
		t.Fatal(err)
	}
	if got := fontConfigEnv(dir); len(got) != 0 {
		t.Fatalf("配置写入失败时应回退默认配置，got %v", got)
	}
}

func TestFontConfigEnvDarwin(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS 专用：只有 darwin 需要显式字体配置")
	}
	t.Setenv("FONTCONFIG_FILE", "")
	dir := t.TempDir()
	env := fontConfigEnv(dir)
	if len(env) != 1 || !strings.HasPrefix(env[0], "FONTCONFIG_FILE=") {
		t.Fatalf("env = %v", env)
	}
	path := strings.TrimPrefix(env[0], "FONTCONFIG_FILE=")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取生成的 fonts.conf：%v", err)
	}
	if !strings.Contains(string(data), "<dir>/System/Library/Fonts</dir>") {
		t.Fatalf("生成的 fonts.conf 缺少系统字体目录：\n%s", data)
	}
	if _, err := os.Stat(dir + "/fontconfig-cache"); err != nil {
		t.Fatalf("字体缓存目录未创建：%v", err)
	}
}
