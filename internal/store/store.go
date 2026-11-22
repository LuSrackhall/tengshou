// Package store owns the SQLite database: migrations, writes and the
// read-only query surface used by the local HTTP API.
package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver (no cgo)
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Store is the writable database handle. Exactly one process should own it.
type Store struct {
	db   *sql.DB
	path string
}

// Open opens (creating if needed) the SQLite database and applies migrations.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create database dir: %w", err)
	}
	dsn := fileDSN(path, false)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	// A single writer connection keeps SQLite contention predictable.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	s := &Store{db: db, path: path}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func fileDSN(path string, readOnly bool) string {
	v := url.Values{}
	common := []string{"busy_timeout(5000)", "foreign_keys(1)"}
	if readOnly {
		v.Set("mode", "ro")
		v.Set("_pragma", "query_only(1)")
		// Keep busy_timeout for readers too.
		v.Add("_pragma", common[0])
	} else {
		v.Add("_pragma", common[0])
		v.Add("_pragma", common[1])
		v.Add("_pragma", "journal_mode(WAL)")
		v.Add("_pragma", "synchronous(NORMAL)")
	}
	return "file:" + filepath.ToSlash(path) + "?" + v.Encode()
}

// Close closes the database.
func (s *Store) Close() error {
	return s.db.Close()
}

// Path returns the database file path.
func (s *Store) Path() string { return s.path }

type migration struct {
	version int
	name    string
	sql     string
}

func loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return nil, err
	}
	var out []migration
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		var version int
		var name string
		if _, err := fmt.Sscanf(entry.Name(), "%04d_%s", &version, &name); err != nil {
			return nil, fmt.Errorf("migration filename %q must look like 0001_name.sql", entry.Name())
		}
		raw, err := migrationFS.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return nil, err
		}
		out = append(out, migration{version: version, name: entry.Name(), sql: string(raw)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	for i := 1; i < len(out); i++ {
		if out[i].version == out[i-1].version {
			return nil, fmt.Errorf("duplicate migration version %d", out[i].version)
		}
	}
	return out, nil
}

func (s *Store) migrate() error {
	migrations, err := loadMigrations()
	if err != nil {
		return fmt.Errorf("load migrations: %w", err)
	}
	if _, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    INTEGER PRIMARY KEY,
			name       TEXT NOT NULL,
			applied_at TEXT NOT NULL
		)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	applied := map[int]bool{}
	rows, err := s.db.Query(`SELECT version FROM schema_migrations`)
	if err != nil {
		return fmt.Errorf("read schema_migrations: %w", err)
	}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return err
		}
		applied[v] = true
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	for _, m := range migrations {
		if applied[m.version] {
			continue
		}
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(m.sql); err != nil {
			tx.Rollback()
			return fmt.Errorf("apply migration %s: %w", m.name, err)
		}
		if _, err := tx.Exec(
			`INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, ?, ?)`,
			m.version, m.name, formatTime(time.Now()),
		); err != nil {
			tx.Rollback()
			return fmt.Errorf("record migration %s: %w", m.name, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", m.name, err)
		}
	}
	return nil
}

// SchemaVersion returns the highest applied migration version.
func (s *Store) SchemaVersion() (int, error) {
	var v sql.NullInt64
	err := s.db.QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&v)
	if err != nil {
		return 0, err
	}
	if !v.Valid {
		return 0, nil
	}
	return int(v.Int64), nil
}

// Reader returns a read-only query surface. It opens a separate connection
// with mode=ro and PRAGMA query_only, so accidental writes fail loudly.
func (s *Store) Reader() (*Reader, error) {
	db, err := sql.Open("sqlite", fileDSN(s.path, true))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("open read-only database: %w", err)
	}
	return &Reader{db: db}, nil
}

// Backup writes a consistent copy of the database using VACUUM INTO.
func (s *Store) Backup(destDir string) (string, error) {
	if err := os.MkdirAll(destDir, 0o700); err != nil {
		return "", err
	}
	name := "tengshou-" + time.Now().Format("20060102-150405") + ".db"
	dest := filepath.Join(destDir, name)
	tmp := dest + ".tmp"
	_ = os.Remove(tmp)
	if _, err := s.db.Exec(`VACUUM INTO ?`, tmp); err != nil {
		return "", fmt.Errorf("backup database: %w", err)
	}
	if err := os.Rename(tmp, dest); err != nil {
		return "", err
	}
	return dest, nil
}

var (
	// ErrNotFound is returned when a record does not exist.
	ErrNotFound = errors.New("not found")
	// ErrDuplicateContractNo is returned when a contract number is already used.
	ErrDuplicateContractNo = errors.New("duplicate contract number")
	// ErrDuplicateTemplateVersion is returned when (name, version) already exists.
	ErrDuplicateTemplateVersion = errors.New("duplicate template version")
)

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

// tx runs fn inside a transaction.
func (s *Store) tx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}
