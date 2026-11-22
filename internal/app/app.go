// Package app wires configuration, storage, logging and PDF generation.
package app

import (
	"fmt"
	"io"
	"log/slog"
	"sync"

	"github.com/tdewolff/canvas"

	"github.com/LuSrackhall/tengshou/internal/config"
	"github.com/LuSrackhall/tengshou/internal/logging"
	"github.com/LuSrackhall/tengshou/internal/pdfdoc"
	"github.com/LuSrackhall/tengshou/internal/service"
	"github.com/LuSrackhall/tengshou/internal/store"
)

// App owns the long-lived application resources.
type App struct {
	Paths   config.Paths
	Config  config.Config
	Logger  *slog.Logger
	Store   *store.Store
	Reader  *store.Reader
	Service *service.Service

	logCloser io.Closer

	genOnce sync.Once
	gen     *pdfdoc.Generator
	genErr  error
}

// Open initializes the data directory, configuration, logger and database.
func Open(dataDir string) (*App, error) {
	paths, err := config.ResolvePaths(dataDir)
	if err != nil {
		return nil, err
	}
	cfg, err := config.Load(paths)
	if err != nil {
		return nil, err
	}
	logger, closer, err := logging.New(paths, cfg.Log)
	if err != nil {
		return nil, err
	}
	st, err := store.Open(paths.DB)
	if err != nil {
		closer.Close()
		return nil, err
	}
	reader, err := st.Reader()
	if err != nil {
		st.Close()
		closer.Close()
		return nil, err
	}
	svc := service.New(st, reader, cfg.Operator)
	svc.SetDataDir(paths.Root)
	a := &App{
		Paths:     paths,
		Config:    cfg,
		Logger:    logger,
		Store:     st,
		Reader:    reader,
		Service:   svc,
		logCloser: closer,
	}
	// PDF template overlays share the HTML generator's CJK font, so a single
	// font resolution serves both pipelines.
	svc.SetFontLoader(func() (*canvas.FontFamily, error) {
		gen, err := a.Generator()
		if err != nil {
			return nil, err
		}
		return gen.Font, nil
	})
	return a, nil
}

// Generator returns a lazily created PDF generator. Font resolution happens
// here so the HTTP service can start even when no CJK font is installed.
func (a *App) Generator() (*pdfdoc.Generator, error) {
	a.genOnce.Do(func() {
		a.gen, a.genErr = pdfdoc.NewGenerator(pdfdoc.FontOptions{
			Path:         a.Config.PDF.FontPath,
			Index:        a.Config.PDF.FontIndex,
			DataFontsDir: a.Paths.Fonts,
		}, a.Config.PDF.MarginMM, true)
		if a.genErr == nil && a.Logger != nil {
			a.Logger.Info("pdf font loaded", "path", a.gen.Spec.Path, "index", a.gen.Spec.Index, "name", a.gen.Spec.Name)
		}
	})
	return a.gen, a.genErr
}

// Backup writes a database backup into the data directory.
func (a *App) Backup() (string, error) {
	return a.Store.Backup(a.Paths.Backups)
}

// Close releases resources.
func (a *App) Close() error {
	var firstErr error
	if a.Reader != nil {
		if err := a.Reader.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if a.Store != nil {
		if err := a.Store.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if a.logCloser != nil {
		if err := a.logCloser.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// String describes the app for logs and diagnostics.
func (a *App) String() string {
	return fmt.Sprintf("tengshou data=%s db_schema=%d", a.Paths.Root, a.Config.Version)
}
