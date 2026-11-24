// Package mcpconn runs the MCP stdio connector shared by the desktop and CLI
// entry points: `tengshou mcp` and `tengshou-cli mcp` both call Run, so the
// release-shaped desktop binary and the headless CLI execute the same code with
// the same flags, environment variables and write gate.
package mcpconn

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/LuSrackhall/tengshou/internal/config"
	"github.com/LuSrackhall/tengshou/internal/mcp"
	"github.com/LuSrackhall/tengshou/internal/version"
)

// IsMCPCommand reports whether args (os.Args without the program name) selects
// the stdio connector. The desktop entry checks this before touching Wails, so
// `tengshou-windows-amd64.exe mcp` behaves exactly like `tengshou-cli mcp`.
func IsMCPCommand(args []string) bool {
	return len(args) > 0 && args[0] == "mcp"
}

// Run starts the B6 stdio MCP connector. It never opens the database: the
// connector talks to the running local HTTP service over loopback, so all
// reads, validation and audit logging stay in the one application process.
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("mcp", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dataDir := fs.String("data-dir", "", "数据目录（读取端口与令牌，默认使用系统应用数据目录）")
	apiURL := fs.String("api-url", os.Getenv("TENGHSHOU_API_URL"), "本机服务地址（默认 http://127.0.0.1:<配置端口>）")
	token := fs.String("token", os.Getenv("TENGHSHOU_API_TOKEN"), "数据接口令牌（默认读取 config.json）")
	if err := fs.Parse(args); err != nil {
		return err
	}
	bases, tokenValue, err := targets(*dataDir, *apiURL, *token)
	if err != nil {
		return err
	}
	api, err := mcp.NewAPI(mcp.APIOptions{BaseURLs: bases, Token: tokenValue})
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	logger.Info("腾手 MCP 服务已启动（stdio）", "api", strings.Join(bases, "、"), "data_dir", *dataDir)
	opts := mcp.Options{API: api, Version: version.String(), Logger: logger}
	// The connector re-reads config.json before each write tool call, so
	// switching MCP writes off in the desktop app applies without restarting
	// this process. The HTTP guard remains the authoritative gate.
	if paths, err := config.ResolvePaths(*dataDir); err == nil {
		opts.WriteEnabled = config.MCPWriteGate(paths)
	} else {
		logger.Warn("读取配置失败，MCP 写开关交由 HTTP 门禁判定", "error", err)
	}
	server := mcp.New(opts)
	return server.Serve(context.Background(), stdin, stdout)
}

// targets resolves candidate loopback endpoints and the read API token.
// Without an explicit --api-url it reads config.json and assumes the desktop
// app's port fallback range (configured port .. +9).
func targets(dataDir, apiURL, token string) ([]string, string, error) {
	apiURL = strings.TrimSpace(apiURL)
	token = strings.TrimSpace(token)
	if apiURL == "" || token == "" {
		paths, err := config.ResolvePaths(dataDir)
		if err != nil {
			return nil, "", err
		}
		cfg, cfgErr := config.Load(paths)
		if cfgErr != nil {
			if apiURL == "" {
				return nil, "", fmt.Errorf("读取配置失败：%w", cfgErr)
			}
			// An explicit address still works for writes; reads may need the
			// token, which the user can pass with --token.
			return []string{apiURL}, token, nil
		}
		if token == "" {
			token = cfg.HTTP.Token
		}
		if apiURL != "" {
			return []string{apiURL}, token, nil
		}
		bases := make([]string, 0, config.PortFallbackSpan)
		for port := cfg.HTTP.Port; port < cfg.HTTP.Port+config.PortFallbackSpan && port <= 65535; port++ {
			bases = append(bases, fmt.Sprintf("http://127.0.0.1:%d", port))
		}
		return bases, token, nil
	}
	return []string{apiURL}, token, nil
}
