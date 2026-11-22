package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/LuSrackhall/tengshou/internal/backup"
	"github.com/LuSrackhall/tengshou/internal/model"
)

// CreateBackupFile builds a complete backup package in <data>/backups and
// records the export in the change log. Callers are responsible for removing
// the temporary file after streaming it to the user.
func (s *Service) CreateBackupFile(ctx context.Context) (string, backup.Manifest, error) {
	if s.backupMgr == nil {
		return "", backup.Manifest{}, business("not_configured", "当前没有可备份的数据目录")
	}
	path, manifest, err := s.backupMgr.CreateTo(ctx, s.backupMgr.Paths.Backups)
	if err != nil {
		return "", backup.Manifest{}, err
	}
	s.logChange(ctx, model.ChangeEntityBackup, manifest.ID, model.ChangeActionExport,
		fmt.Sprintf("导出完整备份包：%d 个文件，%s，数据库 schema v%d",
			len(manifest.Files), humanSize(manifest.TotalSize()), manifest.SchemaVersion), nil)
	return path, manifest, nil
}

// StageBackupRestore validates an uploaded package and stages it for the next
// startup. The running database and assets are not modified.
func (s *Service) StageBackupRestore(ctx context.Context, r io.Reader) (backup.Manifest, error) {
	if s.backupMgr == nil {
		return backup.Manifest{}, business("not_configured", "当前没有可恢复的数据目录")
	}
	manifest, err := s.backupMgr.Stage(ctx, r, backup.DefaultMaxUploadBytes)
	if err != nil {
		if errors.Is(err, backup.ErrInvalidPackage) {
			return backup.Manifest{}, business("invalid_backup", "%s", cleanPackageError(err))
		}
		return backup.Manifest{}, err
	}
	s.logChange(ctx, model.ChangeEntityBackup, manifest.ID, model.ChangeActionRestore,
		fmt.Sprintf("导入备份包并通过校验：%d 个文件，%s，数据库 schema v%d；重启腾手后生效",
			len(manifest.Files), humanSize(manifest.TotalSize()), manifest.SchemaVersion), nil)
	return manifest, nil
}

// PendingBackup summarizes a staged restore for the UI.
type PendingBackup struct {
	ID            string    `json:"id"`
	StagedAt      time.Time `json:"staged_at"`
	CreatedAt     time.Time `json:"created_at"`
	AppVersion    string    `json:"app_version"`
	SchemaVersion int       `json:"schema_version"`
	FileCount     int       `json:"file_count"`
	TotalSize     int64     `json:"total_size"`
}

// BackupStatus reports the data directory and any restore awaiting restart.
type BackupStatus struct {
	Configured bool           `json:"configured"`
	DataDir    string         `json:"data_dir"`
	Pending    *PendingBackup `json:"pending,omitempty"`
}

// BackupStatus returns the backup configuration and staged-restore state.
func (s *Service) BackupStatus() (BackupStatus, error) {
	status := BackupStatus{DataDir: s.dataDir, Configured: s.backupMgr != nil}
	if s.backupMgr == nil {
		return status, nil
	}
	info, ok, err := s.backupMgr.Pending()
	if err != nil {
		return status, err
	}
	if !ok {
		return status, nil
	}
	status.Pending = &PendingBackup{
		ID:            info.Manifest.ID,
		StagedAt:      info.StagedAt,
		CreatedAt:     info.Manifest.CreatedAt,
		AppVersion:    info.Manifest.AppVersion,
		SchemaVersion: info.Manifest.SchemaVersion,
		FileCount:     len(info.Manifest.Files),
		TotalSize:     info.Manifest.TotalSize(),
	}
	return status, nil
}

// AssetIssue is one database record whose file is missing.
type AssetIssue struct {
	Path string `json:"path"`
}

// IntegrityReport is a read-only consistency report. Nothing is deleted:
// missing and orphan files are reported for the user to decide.
type IntegrityReport struct {
	CheckedAt     time.Time    `json:"checked_at"`
	DatabaseOK    bool         `json:"database_ok"`
	DatabaseError string       `json:"database_error,omitempty"`
	CheckedFiles  int          `json:"checked_files"`
	MissingFiles  []AssetIssue `json:"missing_files"`
	OrphanFiles   []string     `json:"orphan_files"`
}

// CheckIntegrity runs PRAGMA integrity_check and compares database asset
// references with the files on disk. It never modifies anything (B9).
func (s *Service) CheckIntegrity(ctx context.Context) (IntegrityReport, error) {
	report := IntegrityReport{
		CheckedAt:    s.now(),
		MissingFiles: []AssetIssue{},
		OrphanFiles:  []string{},
	}
	if err := s.st.IntegrityCheck(); err != nil {
		report.DatabaseError = err.Error()
	} else {
		report.DatabaseOK = true
	}
	root := strings.TrimSpace(s.dataDir)
	if root == "" {
		return report, nil
	}
	refs, err := s.rd.ReferencedAssetPaths(ctx)
	if err != nil {
		return report, err
	}
	known := map[string]bool{}
	for _, rel := range refs {
		clean := path.Clean(filepath.ToSlash(strings.TrimSpace(rel)))
		if clean == "." || clean == "" {
			continue
		}
		known[clean] = true
		full := filepath.Join(root, filepath.FromSlash(clean))
		info, statErr := os.Stat(full)
		if statErr != nil || !info.Mode().IsRegular() {
			report.MissingFiles = append(report.MissingFiles, AssetIssue{Path: clean})
			continue
		}
		report.CheckedFiles++
	}
	for _, dirName := range []string{"assets", "templates", "outputs"} {
		dir := filepath.Join(root, dirName)
		walkErr := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					return nil
				}
				return err
			}
			if d.IsDir() {
				return nil
			}
			rel, relErr := filepath.Rel(root, p)
			if relErr != nil {
				return relErr
			}
			name := filepath.ToSlash(rel)
			if !known[name] {
				report.OrphanFiles = append(report.OrphanFiles, name)
			}
			return nil
		})
		if walkErr != nil {
			return report, walkErr
		}
	}
	sort.Slice(report.MissingFiles, func(i, j int) bool {
		return report.MissingFiles[i].Path < report.MissingFiles[j].Path
	})
	sort.Strings(report.OrphanFiles)
	return report, nil
}

func cleanPackageError(err error) string {
	message := err.Error()
	prefix := backup.ErrInvalidPackage.Error() + "："
	message = strings.TrimPrefix(message, prefix)
	prefix = backup.ErrInvalidPackage.Error() + ": "
	return strings.TrimPrefix(message, prefix)
}

func humanSize(n int64) string {
	const unit = int64(1024)
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := unit, 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}
