// Command tengshou is the Wails v3 desktop application. The window is a thin
// shell around the same local web UI and HTTP service used by tengshou-cli.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/LuSrackhall/tengshou/internal/app"
	"github.com/LuSrackhall/tengshou/internal/config"
	"github.com/LuSrackhall/tengshou/internal/httpapi"
	"github.com/LuSrackhall/tengshou/internal/mcpconn"
	"github.com/LuSrackhall/tengshou/internal/version"
	"github.com/wailsapp/wails/v3/pkg/application"
)

func main() {
	// `tengshou mcp ...` is the single-file MCP delivery form: the customer's
	// desktop client doubles as the stdio connector. Dispatch before Wails,
	// the database and the HTTP service so no window opens and no port is
	// bound; mcpconn is the exact code the CLI runs.
	if mcpconn.IsMCPCommand(os.Args[1:]) {
		if err := mcpconn.Run(os.Args[2:], os.Stdin, os.Stdout, os.Stderr); err != nil {
			fmt.Fprintln(os.Stderr, "错误："+err.Error())
			os.Exit(1)
		}
		return
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "启动失败："+err.Error())
		os.Exit(1)
	}
}

func run() error {
	a, err := app.Open("")
	if err != nil {
		return err
	}
	defer a.Close()

	// Warm the PDF font cache so the UI can report a useful status later, but
	// do not block startup on it.
	if _, err := a.Generator(); err != nil {
		a.Logger.Warn("pdf font unavailable", "error", err)
	}

	server, ln, err := listenWithFallback(a)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		if err := server.Serve(ctx, ln); err != nil && !errors.Is(err, context.Canceled) {
			a.Logger.Error("local service stopped", "error", err)
		}
	}()
	for _, u := range server.URLs(ln) {
		a.Logger.Info("local service url", "url", u)
	}

	wailsApp := application.New(application.Options{
		Name:        "腾手",
		Description: "本地合同文书工具",
		LogLevel:    slog.LevelInfo,
		Assets: application.AssetOptions{
			Handler: server.Handler(),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})
	wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:  "腾手",
		Width:  1080,
		Height: 760,
		URL:    "/app/",
		Mac: application.MacWindow{
			TitleBar: application.MacTitleBarDefault,
		},
	})

	if err := wailsApp.Run(); err != nil {
		return err
	}
	stop()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return server.Shutdown(shutdownCtx)
}

// listenWithFallback binds the configured port, trying the next few ports when
// it is already in use (another 腾手 instance or an unrelated service).
func listenWithFallback(a *app.App) (*httpapi.Server, net.Listener, error) {
	cfg := a.Config
	var lastErr error
	for port := cfg.HTTP.Port; port < cfg.HTTP.Port+config.PortFallbackSpan && port <= 65535; port++ {
		cfg.HTTP.Port = port
		server := httpapi.New(httpapi.Options{
			Reader:   a.Reader,
			Service:  a.Service,
			Logger:   a.Logger,
			Config:   cfg,
			Settings: config.NewSettings(a.Paths, cfg),
			Version:  version.String(),
		})
		ln, err := server.Listen()
		if err == nil {
			return server, ln, nil
		}
		lastErr = err
	}
	return nil, nil, fmt.Errorf("无法监听本地端口: %w", lastErr)
}
