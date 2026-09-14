package testdb

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"

	"femucaribe-backup-agent/internal/sqlbackup"
	"femucaribe-backup-agent/test/fixtures"
	_ "modernc.org/sqlite"
)

// SQLiteSQLEngine adapta una base de datos SQLite para cumplir con la interfaz application.SQLEngine en tests rápidos.
type SQLiteSQLEngine struct {
	dbPath            string
	simulateDiskFull  bool
	simulateBackupErr error
	simulateVerifyErr error
}

func NewSQLiteSQLEngine(dbPath string) *SQLiteSQLEngine {
	return &SQLiteSQLEngine{dbPath: dbPath}
}

func (e *SQLiteSQLEngine) SetSimulateDiskFull(v bool) {
	e.simulateDiskFull = v
}

func (e *SQLiteSQLEngine) SetSimulateBackupError(err error) {
	e.simulateBackupErr = err
}

func (e *SQLiteSQLEngine) SetSimulateVerifyError(err error) {
	e.simulateVerifyErr = err
}

func (e *SQLiteSQLEngine) Open(opts sqlbackup.ConnectOptions) (io.Closer, error) {
	db, err := sql.Open("sqlite", e.dbPath)
	if err != nil {
		return nil, fmt.Errorf("abrir sqlite test: %w", err)
	}
	return db, nil
}

func (e *SQLiteSQLEngine) DatabaseSizeBytes(ctx context.Context, db io.Closer, database string) (int64, error) {
	fi, err := os.Stat(e.dbPath)
	if err != nil {
		return 0, fmt.Errorf("obtener tamaño sqlite: %w", err)
	}
	return fi.Size(), nil
}

func (e *SQLiteSQLEngine) EnsureFreeSpace(dir string, neededBytes int64) error {
	if e.simulateDiskFull {
		return sqlbackup.ErrInsufficientSpace
	}
	return sqlbackup.EnsureFreeSpace(dir, neededBytes)
}

func (e *SQLiteSQLEngine) BackupDatabase(ctx context.Context, db io.Closer, database, targetPath string) error {
	if e.simulateBackupErr != nil {
		return e.simulateBackupErr
	}
	srcData, err := os.ReadFile(e.dbPath)
	if err != nil {
		return fmt.Errorf("leer base origen: %w", err)
	}
	return os.WriteFile(targetPath, srcData, 0o644)
}

func (e *SQLiteSQLEngine) VerifyBackup(ctx context.Context, db io.Closer, targetPath string) error {
	if e.simulateVerifyErr != nil {
		return e.simulateVerifyErr
	}
	testDB, err := sql.Open("sqlite", targetPath)
	if err != nil {
		return fmt.Errorf("abrir backup para verify: %w", err)
	}
	defer testDB.Close()

	var integrity string
	if err := testDB.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); err != nil {
		return fmt.Errorf("integrity check: %w", err)
	}
	if integrity != "ok" {
		return fmt.Errorf("integrity check falló: %s", integrity)
	}

	return fixtures.AssertSeedIntegrity(ctx, testDB)
}

