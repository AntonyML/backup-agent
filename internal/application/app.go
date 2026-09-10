package application

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"femucaribe-backup-agent/internal/config"
	"femucaribe-backup-agent/internal/events"
	"femucaribe-backup-agent/internal/sqlbackup"
	"femucaribe-backup-agent/internal/state"
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
	eventRepo    events.EventRepository
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
	EventRepo    events.EventRepository
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
		eventRepo:    opts.EventRepo,
		sqlEngine:    engine,
		failpoint:    fp,
		logger:       log,
	}
}

// recordEvent registra un evento operativo en Supabase de forma segura y no bloqueante.
// Si Supabase falla con un error recuperable, el evento se guarda como pendiente en state.json.
func (a *App) recordEvent(ctx context.Context, evt events.Event) {
	if a.eventRepo == nil {
		return
	}

	timeout := 10 * time.Second
	if a.cfg.Supabase.TimeoutSec > 0 {
		timeout = time.Duration(a.cfg.Supabase.TimeoutSec) * time.Second
	}
	eventCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if err := a.eventRepo.Append(eventCtx, evt); err != nil {
		a.logger.Warn("no se pudo registrar evento en Supabase", "tipo", evt.EventType, "error", err)
		if events.IsRetryable(err) {
			st, loadErr := state.Load(a.statePath)
			if loadErr == nil && st != nil {
				st.AddPendingEvent(evt)
				_ = state.Save(a.statePath, st)
			}
		}
	}
}

// flushPendingEvents intenta reenviar eventos acumulados en state.json hacia Supabase.
func (a *App) flushPendingEvents(ctx context.Context, st *state.State) {
	if a.eventRepo == nil || st == nil || len(st.PendingEvents) == 0 {
		return
	}

	a.logger.Info("sincronizando eventos pendientes hacia Supabase", "cantidad", len(st.PendingEvents))
	var remaining []events.Event

	for _, evt := range st.PendingEvents {
		timeout := 5 * time.Second
		if a.cfg.Supabase.TimeoutSec > 0 {
			timeout = time.Duration(a.cfg.Supabase.TimeoutSec) * time.Second
		}
		eventCtx, cancel := context.WithTimeout(ctx, timeout)
		err := a.eventRepo.Append(eventCtx, evt)
		cancel()

		if err != nil {
			a.logger.Warn("reintento de evento pendiente falló", "tipo", evt.EventType, "id", evt.EventID, "error", err)
			remaining = append(remaining, evt)
		} else {
			a.logger.Info("evento pendiente sincronizado exitosamente", "tipo", evt.EventType, "id", evt.EventID)
		}
	}

	st.PendingEvents = remaining
	_ = state.Save(a.statePath, st)
}


