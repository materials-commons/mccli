// Package filedb provides access to a project-local SQLite database that tracks
// Materials Commons file state.
//
// Each local project has its own database:
//
//	$PROJECT/.mc/mc2.sqlite
//
// The database stores one row per known project path. Remote paths are Materials
// Commons project paths and always start with "/".
package filedb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	mclogging "github.com/materials-commons/mccli/pkg/logging"
	"github.com/materials-commons/mccli/pkg/projectpath"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	// DatabaseFileName is the project-local SQLite database file name.
	DatabaseFileName = "mc2.sqlite"

	defaultBusyTimeout = 5000
)

var (
	// ErrRecordNotFound indicates that a file record does not exist.
	ErrRecordNotFound = gorm.ErrRecordNotFound
)

// DBPath returns the project-local file database path:
//
//	$PROJECT/.mc/mc2.sqlite
func DBPath(projectRoot string) string {
	return filepath.Join(projectpath.ConfigDir(projectRoot), DatabaseFileName)
}

// FileRecord stores local and remote state for one project path.
//
// Path is a Materials Commons remote project path, such as "/" or
// "/Dir1/file.txt".
type FileRecord struct {
	Path string `gorm:"column:path;primaryKey"`

	Dir  string `gorm:"column:dir;not null;index"`
	Name string `gorm:"column:name;not null"`

	IsCleanLocalCopy bool `gorm:"column:is_clean_local_copy;not null;default:false"`

	LocalSize       int64   `gorm:"column:local_size;not null"`
	LocalMTimeNS    int64   `gorm:"column:local_mtime_ns;not null"`
	LocalCTimeNS    int64   `gorm:"column:local_ctime_ns;not null"`
	LocalLastSeenTS int64   `gorm:"column:local_last_seen_ts;not null;index"`
	LocalChecksum   *string `gorm:"column:local_checksum"`

	RemoteFileID     *int64  `gorm:"column:remote_file_id"`
	RemoteSize       *int64  `gorm:"column:remote_size"`
	RemoteCTimeNS    *int64  `gorm:"column:remote_ctime_ns"`
	RemoteChecksum   *string `gorm:"column:remote_checksum"`
	RemoteLastSeenTS *int64  `gorm:"column:remote_last_seen_ts"`

	Status     *string `gorm:"column:status;index"`
	Origin     *string `gorm:"column:origin"`
	TransferID *string `gorm:"column:transfer_id"`
}

// TableName returns the SQLite table name.
func (FileRecord) TableName() string {
	return "files"
}

// Store is a project-local file index database.
//
// Store is safe for concurrent use by multiple goroutines.
type Store struct {
	path string
	db   *gorm.DB
	sql  *sql.DB
}

// Open opens or creates the file database for projectRoot.
func Open(ctx context.Context, projectRoot string) (*Store, error) {
	dbPath := DBPath(projectRoot)
	return OpenPath(ctx, dbPath)
}

// OpenPath opens or creates a file database at dbPath.
func OpenPath(ctx context.Context, dbPath string) (*Store, error) {
	if dbPath == "" {
		return nil, fmt.Errorf("database path is required")
	}

	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("create file database directory %q: %w", filepath.Dir(dbPath), err)
	}

	logger := mclogging.Logger(ctx)
	logger.Debug("opening file database", "path", dbPath)

	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		TranslateError: true,
	})
	if err != nil {
		return nil, fmt.Errorf("open file database %q: %w", dbPath, err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("get sql database handle for %q: %w", dbPath, err)
	}

	// SQLite allows many readers but only one writer. Keep the pool conservative
	// at first. Callers may still use Store concurrently; database/sql will
	// serialize operations through the single connection.
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	sqlDB.SetConnMaxLifetime(0)

	store := &Store{
		path: dbPath,
		db:   db,
		sql:  sqlDB,
	}

	if err := store.configure(ctx); err != nil {
		_ = store.Close(ctx)
		return nil, err
	}

	if err := store.migrate(ctx); err != nil {
		_ = store.Close(ctx)
		return nil, err
	}

	return store, nil
}

// Path returns the SQLite database path.
func (s *Store) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

// Close closes the underlying SQLite database.
func (s *Store) Close(ctx context.Context) error {
	if s == nil || s.sql == nil {
		return nil
	}

	mclogging.Logger(ctx).Debug("closing file database", "path", s.path)

	// Best effort checkpoint. If this fails, still close the DB and return the
	// close error if any.
	checkpointErr := s.db.Exec("PRAGMA wal_checkpoint(TRUNCATE);").Error
	closeErr := s.sql.Close()

	if closeErr != nil {
		return fmt.Errorf("close file database %q: %w", s.path, closeErr)
	}
	if checkpointErr != nil {
		return fmt.Errorf("checkpoint file database %q: %w", s.path, checkpointErr)
	}

	return nil
}

func (s *Store) configure(ctx context.Context) error {
	pragmas := []string{
		"PRAGMA journal_mode=WAL;",
		fmt.Sprintf("PRAGMA busy_timeout=%d;", defaultBusyTimeout),
		"PRAGMA synchronous=NORMAL;",
		"PRAGMA foreign_keys=ON;",
		"PRAGMA cache_size=100000;",
		"PRAGMA temp_store=MEMORY;",
	}

	for _, pragma := range pragmas {
		if err := s.db.WithContext(ctx).Exec(pragma).Error; err != nil {
			return fmt.Errorf("configure file database %q with %s: %w", s.path, pragma, err)
		}
	}

	return nil
}

func (s *Store) migrate(ctx context.Context) error {
	if err := s.db.WithContext(ctx).AutoMigrate(&FileRecord{}); err != nil {
		return fmt.Errorf("migrate file database %q: %w", s.path, err)
	}
	return nil
}

// Upsert inserts or updates one file record.
//
// Nullable fields preserve the existing value when the incoming value is nil.
// This matches the Python COALESCE behavior used by the original upsert SQL.
func (s *Store) Upsert(ctx context.Context, record FileRecord) error {
	if err := validateRecord(record); err != nil {
		return err
	}

	mclogging.Logger(ctx).Debug("upserting file record", "path", record.Path)

	err := gorm.G[FileRecord](s.db, upsertClause()).Create(ctx, &record)
	if err != nil {
		return fmt.Errorf("upsert file record %q: %w", record.Path, err)
	}

	return nil
}

// UpsertMany inserts or updates multiple file records in one transaction.
func (s *Store) UpsertMany(ctx context.Context, records []FileRecord) error {
	if len(records) == 0 {
		return nil
	}

	for _, record := range records {
		if err := validateRecord(record); err != nil {
			return err
		}
	}

	mclogging.Logger(ctx).Debug("upserting file records", "count", len(records))

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return gorm.G[FileRecord](tx, upsertClause()).CreateInBatches(ctx, &records, 100)
	})
	if err != nil {
		return fmt.Errorf("upsert %d file records: %w", len(records), err)
	}

	return nil
}

// GetByPath returns the file record for path.
func (s *Store) GetByPath(ctx context.Context, filePath string) (FileRecord, error) {
	record, err := gorm.G[FileRecord](s.db).
		Where("path = ?", filePath).
		First(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return FileRecord{}, ErrRecordNotFound
		}
		return FileRecord{}, fmt.Errorf("get file record by path %q: %w", filePath, err)
	}

	return record, nil
}

// ListByDir returns all file records whose parent directory is dir.
func (s *Store) ListByDir(ctx context.Context, dir string) ([]FileRecord, error) {
	records, err := gorm.G[FileRecord](s.db).
		Where("dir = ?", dir).
		Order("name ASC").
		Find(ctx)
	if err != nil {
		return nil, fmt.Errorf("list file records by dir %q: %w", dir, err)
	}

	return records, nil
}

// DeleteByPath deletes the file record for path.
func (s *Store) DeleteByPath(ctx context.Context, filePath string) error {
	_, err := gorm.G[FileRecord](s.db).
		Where("path = ?", filePath).
		Delete(ctx)
	if err != nil {
		return fmt.Errorf("delete file record by path %q: %w", filePath, err)
	}

	return nil
}

// MarkTransfer records transfer metadata for a file path.
func (s *Store) MarkTransfer(ctx context.Context, filePath, status, origin, transferID string) error {
	updates := map[string]any{
		"status":      status,
		"origin":      origin,
		"transfer_id": transferID,
	}

	err := s.db.WithContext(ctx).
		Model(&FileRecord{}).
		Where("path = ?", filePath).
		Updates(updates).
		Error
	if err != nil {
		return fmt.Errorf("mark transfer for %q: %w", filePath, err)
	}

	return nil
}

// TouchLocalSeen updates the local_last_seen_ts timestamp for a file path.
func (s *Store) TouchLocalSeen(ctx context.Context, filePath string, seen time.Time) error {
	_, err := gorm.G[FileRecord](s.db).
		Where("path = ?", filePath).
		Update(ctx, "local_last_seen_ts", seen.Unix())
	if err != nil {
		return fmt.Errorf("touch local seen for %q: %w", filePath, err)
	}

	return nil
}

func validateRecord(record FileRecord) error {
	if record.Path == "" {
		return fmt.Errorf("file record path is required")
	}
	if record.Dir == "" {
		return fmt.Errorf("file record dir is required for %q", record.Path)
	}
	if record.Name == "" {
		return fmt.Errorf("file record name is required for %q", record.Path)
	}
	return nil
}

func upsertClause() clause.OnConflict {
	return clause.OnConflict{
		Columns: []clause.Column{{Name: "path"}},
		DoUpdates: clause.Assignments(map[string]any{
			"dir":                 gorm.Expr("excluded.dir"),
			"name":                gorm.Expr("excluded.name"),
			"is_clean_local_copy": gorm.Expr("excluded.is_clean_local_copy"),

			"local_size":         gorm.Expr("excluded.local_size"),
			"local_mtime_ns":     gorm.Expr("excluded.local_mtime_ns"),
			"local_ctime_ns":     gorm.Expr("excluded.local_ctime_ns"),
			"local_last_seen_ts": gorm.Expr("excluded.local_last_seen_ts"),
			"local_checksum":     gorm.Expr("COALESCE(excluded.local_checksum, files.local_checksum)"),

			"remote_file_id":      gorm.Expr("COALESCE(excluded.remote_file_id, files.remote_file_id)"),
			"remote_size":         gorm.Expr("COALESCE(excluded.remote_size, files.remote_size)"),
			"remote_ctime_ns":     gorm.Expr("COALESCE(excluded.remote_ctime_ns, files.remote_ctime_ns)"),
			"remote_checksum":     gorm.Expr("COALESCE(excluded.remote_checksum, files.remote_checksum)"),
			"remote_last_seen_ts": gorm.Expr("COALESCE(excluded.remote_last_seen_ts, files.remote_last_seen_ts)"),

			"status":      gorm.Expr("COALESCE(excluded.status, files.status)"),
			"origin":      gorm.Expr("COALESCE(excluded.origin, files.origin)"),
			"transfer_id": gorm.Expr("COALESCE(excluded.transfer_id, files.transfer_id)"),
		}),
	}
}
