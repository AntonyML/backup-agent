package testdb

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	"femucaribe-backup-agent/test/fixtures"
	_ "modernc.org/sqlite"
)

type sqliteDatabase struct {
	driver   string
	dsn      string
	db       *sql.DB
	filePath string
}

func NewSQLiteMemory(ctx context.Context) (TestDatabase, error) {
	dsn := "file::memory:?cache=shared"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("abrir sqlite memory: %w", err)
	}
	db.SetMaxOpenConns(1)

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite memory: %w", err)
	}

	return &sqliteDatabase{
		driver: "sqlite",
		dsn:    dsn,
		db:     db,
	}, nil
}

func NewSQLiteFile(ctx context.Context, filePath string) (TestDatabase, error) {
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		return nil, fmt.Errorf("crear directorio para sqlite: %w", err)
	}

	db, err := sql.Open("sqlite", filePath)
	if err != nil {
		return nil, fmt.Errorf("abrir sqlite file (%s): %w", filePath, err)
	}

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite file: %w", err)
	}

	return &sqliteDatabase{
		driver:   "sqlite",
		dsn:      filePath,
		db:       db,
		filePath: filePath,
	}, nil
}

func (s *sqliteDatabase) Driver() string {
	return s.driver
}

func (s *sqliteDatabase) DSN() string {
	return s.dsn
}

func (s *sqliteDatabase) DB() *sql.DB {
	return s.db
}

func (s *sqliteDatabase) Seed(ctx context.Context) error {
	return fixtures.SeedTestData(ctx, s.db)
}

func (s *sqliteDatabase) Reset(ctx context.Context) error {
	return fixtures.ResetTestData(ctx, s.db)
}

func (s *sqliteDatabase) Close() error {
	return s.db.Close()
}
