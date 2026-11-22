// Package logging configures structured application logging.
package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/LuSrackhall/tengshou/internal/config"
)

// New builds a logger that writes JSON records to <data>/logs/tengshou.log and
// mirrors them to stderr. The log file is rotated on startup once it exceeds
// the configured size. The returned closer must be called on shutdown.
func New(paths config.Paths, cfg config.Log) (*slog.Logger, io.Closer, error) {
	if err := os.MkdirAll(paths.Logs, 0o700); err != nil {
		return nil, nil, fmt.Errorf("create log dir: %w", err)
	}
	logPath := filepath.Join(paths.Logs, "tengshou.log")
	if err := rotate(logPath, int64(cfg.MaxSizeMB)*1024*1024); err != nil {
		return nil, nil, err
	}
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, nil, fmt.Errorf("open log file: %w", err)
	}
	level := parseLevel(cfg.Level)
	handler := slog.NewJSONHandler(io.MultiWriter(f, os.Stderr), &slog.HandlerOptions{Level: level})
	return slog.New(handler), f, nil
}

func parseLevel(s string) slog.Level {
	switch s {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func rotate(path string, maxBytes int64) error {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat log file: %w", err)
	}
	if maxBytes <= 0 || info.Size() < maxBytes {
		return nil
	}
	previous := path + ".1"
	_ = os.Remove(previous)
	if err := os.Rename(path, previous); err != nil {
		return fmt.Errorf("rotate log file: %w", err)
	}
	return nil
}
