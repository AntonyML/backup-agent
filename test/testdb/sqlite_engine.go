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
	dbPath string
}

func NewSQLiteSQLEngine(dbPath string) *SQLiteSQLEngine {
	return &SQLiteSQLEngine{dbPath: dbPath}
}

func (e *SQLiteSQLEngine) Open(server string, loginTimeoutSec int) (io.Closer, error) {
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
	return sqlbackup.EnsureFreeSpace(dir, neededBytes)
}

func (e *SQLiteSQLEngine) BackupDatabase(ctx context.Context, db io.Closer, database, targetPath string) error {
	srcData, err := os.ReadFile(e.dbPath)
	if err != nil {
		return fmt.Errorf("leer base origen: %w", err)
	}
	return os.WriteFile(targetPath, srcData, 0o644)
}

func (e *SQLiteSQLEngine) VerifyBackup(ctx context.Context, db io.Closer, targetPath string) error {
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
