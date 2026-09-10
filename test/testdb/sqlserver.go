package testdb

import (
	"context"
	"database/sql"
	"fmt"

	_ "github.com/microsoft/go-mssqldb"
	"femucaribe-backup-agent/test/fixtures"
	"femucaribe-backup-agent/test/testenv"
)

// SQLServerConfig contiene los parámetros para conectar a la instancia aislada de SQL Server.
type SQLServerConfig struct {
	Host       string
	Port       int
	Database   string
	User       string
	Password   string
	BackupRoot string
}

type sqlserverDatabase struct {
	driver string
	dsn    string
	db     *sql.DB
	cfg    SQLServerConfig
}

// NewSQLServer instancia una conexión a SQL Server previa validación estricta de seguridad anti-producción.
func NewSQLServer(ctx context.Context, cfg SQLServerConfig) (TestDatabase, error) {
	// Validación de seguridad obligatoria: nunca permitir Caproba01\vbadilla, CONTABILIDAD o C:\Backups
	safetyErr := testenv.ValidateSafety(testenv.TestEnvironmentConfig{
		DatabaseDriver: "sqlserver",
		DatabaseName:   cfg.Database,
		ServerInstance: fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		BackupRoot:     cfg.BackupRoot,
		TestMode:       true,
	})
	if safetyErr != nil {
		return nil, fmt.Errorf("seguridad de testing violada: %w", safetyErr)
	}

	dsn := fmt.Sprintf("server=%s;port=%d;database=%s;user id=%s;password=%s;encrypt=disable;trustservercertificate=true",
		cfg.Host, cfg.Port, cfg.Database, cfg.User, cfg.Password)

	db, err := sql.Open("sqlserver", dsn)
	if err != nil {
		return nil, fmt.Errorf("abrir conexión sqlserver test: %w", err)
	}

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlserver test (%s:%d): %w", cfg.Host, cfg.Port, err)
	}

	return &sqlserverDatabase{
		driver: "sqlserver",
		dsn:    dsn,
		db:     db,
		cfg:    cfg,
	}, nil
}

func (s *sqlserverDatabase) Driver() string {
	return s.driver
}

func (s *sqlserverDatabase) DSN() string {
	return s.dsn
}

func (s *sqlserverDatabase) DB() *sql.DB {
	return s.db
}

func (s *sqlserverDatabase) Seed(ctx context.Context) error {
	return fixtures.SeedTestData(ctx, s.db)
}

func (s *sqlserverDatabase) Reset(ctx context.Context) error {
	return fixtures.ResetTestData(ctx, s.db)
}

func (s *sqlserverDatabase) Close() error {
	return s.db.Close()
}
