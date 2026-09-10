package application

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"os"

	"femucaribe-backup-agent/internal/config"
	"femucaribe-backup-agent/internal/sqlbackup"
	"femucaribe-backup-agent/internal/storage"
)

// SQLEngine desacopla las operaciones de SQL Server para permitir pruebas unitarias sin base de datos real.
type SQLEngine interface {
	Open(server string, loginTimeoutSec int) (io.Closer, error)
	DatabaseSizeBytes(ctx context.Context, db io.Closer, database string) (int64, error)
	EnsureFreeSpace(dir string, neededBytes int64) error
	BackupDatabase(ctx context.Context, db io.Closer, database, targetPath string) error
	VerifyBackup(ctx context.Context, db io.Closer, targetPath string) error
}

type defaultSQLEngine struct{}

func (e defaultSQLEngine) Open(server string, loginTimeoutSec int) (io.Closer, error) {
	return sqlbackup.Open(server, loginTimeoutSec)
}

func (e defaultSQLEngine) DatabaseSizeBytes(ctx context.Context, db io.Closer, database string) (int64, error) {
	sqlDB, ok := db.(*sql.DB)
	if !ok {
		return 0, fmt.Errorf("sql engine: handle de base de datos inválido")
	}
	return sqlbackup.DatabaseSizeBytes(ctx, sqlDB, database)
}

func (e defaultSQLEngine) EnsureFreeSpace(dir string, neededBytes int64) error {
	return sqlbackup.EnsureFreeSpace(dir, neededBytes)
}

func (e defaultSQLEngine) BackupDatabase(ctx context.Context, db io.Closer, database, targetPath string) error {
	sqlDB, ok := db.(*sql.DB)
	if !ok {
		return fmt.Errorf("sql engine: handle de base de datos inválido")
	}
	return sqlbackup.BackupDatabase(ctx, sqlDB, database, targetPath)
}

func (e defaultSQLEngine) VerifyBackup(ctx context.Context, db io.Closer, targetPath string) error {
	sqlDB, ok := db.(*sql.DB)
	if !ok {
		return fmt.Errorf("sql engine: handle de base de datos inválido")
	}
	return sqlbackup.VerifyBackup(ctx, sqlDB, targetPath)
}

// DefaultSQLEngine devuelve la implementación productiva basada en internal/sqlbackup.
func DefaultSQLEngine() SQLEngine {
	return defaultSQLEngine{}
}

// FailpointHook permite inyectar fallos controlados para pruebas de resiliencia y recuperación (solo en tests).
type FailpointHook func(point string) error

// App orquesta los casos de uso del agente sin depender de Cobra ni de flujos de terminal/TTY.
type App struct {
	cfg          config.Config
	statePath    string
	secretsPath  string
	lockPath     string
	logDir       string
	backends     []storage.Backend
	localBackend storage.Backend
	sqlEngine    SQLEngine
	failpoint    FailpointHook
	logger       *slog.Logger
}

type Options struct {
	Config       config.Config
	StatePath    string
	SecretsPath  string
	LockPath     string
	LogDir       string
	Backends     []storage.Backend
	LocalBackend storage.Backend
	SQLEngine    SQLEngine
	Failpoint    FailpointHook
	Logger       *slog.Logger
}

func New(opts Options) *App {
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	engine := opts.SQLEngine
	if engine == nil {
		engine = DefaultSQLEngine()
	}
	fp := opts.Failpoint
	if fp == nil && os.Getenv("TEST_FAILPOINT") != "" {
		target := os.Getenv("TEST_FAILPOINT")
		fp = func(point string) error {
			if point == target {
				return fmt.Errorf("failpoint simulado por TEST_FAILPOINT: %s", point)
			}
			return nil
		}
	}
	return &App{
		cfg:          opts.Config,
		statePath:    opts.StatePath,
		secretsPath:  opts.SecretsPath,
		lockPath:     opts.LockPath,
		logDir:       opts.LogDir,
		backends:     opts.Backends,
		localBackend: opts.LocalBackend,
		sqlEngine:    engine,
		failpoint:    fp,
		logger:       log,
	}
}


