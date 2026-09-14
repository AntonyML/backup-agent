package application

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"femucaribe-backup-agent/internal/config"
	"femucaribe-backup-agent/internal/events"
	"femucaribe-backup-agent/internal/sqlbackup"
	"femucaribe-backup-agent/internal/state"
	"femucaribe-backup-agent/internal/storage"
)

// SQLEngine desacopla las operaciones de SQL Server para permitir pruebas unitarias sin base de datos real.
type SQLEngine interface {
	Open(opts sqlbackup.ConnectOptions) (io.Closer, error)
	DatabaseSizeBytes(ctx context.Context, db io.Closer, database string) (int64, error)
	EnsureFreeSpace(dir string, neededBytes int64) error
	BackupDatabase(ctx context.Context, db io.Closer, database, targetPath string) error
	VerifyBackup(ctx context.Context, db io.Closer, targetPath string) error
}

type defaultSQLEngine struct{}

func (e defaultSQLEngine) Open(opts sqlbackup.ConnectOptions) (io.Closer, error) {
	return sqlbackup.Open(opts)
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
	configPath   string
	statePath    string
	secretsPath  string
	lockPath     string
	logDir       string
	profileName  string
	backends     []storage.Backend
	localBackend storage.Backend
	eventRepo    events.EventRepository
	sqlEngine    SQLEngine
	failpoint    FailpointHook
	logger       *slog.Logger
}

type Options struct {
	Config       config.Config
	ConfigPath   string
	StatePath    string
	SecretsPath  string
	LockPath     string
	LogDir       string
	// Profile es el nombre del perfil con el que opera esta instancia de App.
	// Vacío = perfil activo de la config.
	Profile      string
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
	profile := opts.Profile
	if profile == "" {
		profile = opts.Config.ActiveProfile
	}
	if profile == "" {
		profile = config.InitialProfileName
	}
	cfg := opts.Config
	// D9: un Schedule zero-value (config construida por código, p.ej. en tests)
	// mantiene la conducta histórica: sync tras backup activo. Las configs
	// cargadas de archivo ya heredan true desde Default().
	if isZeroSchedule(cfg.Schedule) {
		cfg.Schedule.SyncAfterBackup = true
	}
	backends := filterBackendsForProfile(cfg, profile, opts.Backends)
	return &App{
		cfg:          cfg,
		configPath:   opts.ConfigPath,
		statePath:    opts.StatePath,
		secretsPath:  opts.SecretsPath,
		lockPath:     opts.LockPath,
		logDir:       opts.LogDir,
		profileName:  profile,
		backends:     backends,
		localBackend: opts.LocalBackend,
		eventRepo:    opts.EventRepo,
		sqlEngine:    engine,
		failpoint:    fp,
		logger:       log,
	}
}

// filterBackendsForProfile construye SOLO los backends del perfil activo
// (Etapa 3): local es implícito (localBackend, no se filtra), R2 y server se
// conservan solo si el perfil los lista. storage.Backend no conoce perfiles;
// los nombres de backend se mapean a plataformas acá, en application.
// Si el perfil no existe en la config (config construida a mano en tests),
// se conservan todos para no romper flujos legacy; la CLI valida aparte.
func filterBackendsForProfile(cfg config.Config, profile string, backends []storage.Backend) []storage.Backend {
	p, ok := cfg.ProfileByName(profile)
	if !ok {
		return backends
	}
	wanted := map[string]bool{}
	for _, plat := range p.Platforms {
		wanted[plat] = true
	}
	out := make([]storage.Backend, 0, len(backends))
	for _, b := range backends {
		switch {
		case strings.EqualFold(b.Name(), "r2"):
			if wanted[config.PlatformCloudflare] {
				out = append(out, b)
			}
		case strings.EqualFold(b.Name(), "server"):
			if wanted[config.PlatformServer] {
				out = append(out, b)
			}
		default:
			// Backend futuro/desconocido: se conserva por compatibilidad.
			out = append(out, b)
		}
	}
	return out
}

// ProfileName devuelve el nombre del perfil con el que opera esta App.
func (a *App) ProfileName() string { return a.profileName }

// profileState extrae la sección de estado del perfil activo.
func (a *App) profileState(st *state.State) *state.ProfileState {
	return st.Profile(a.profileName)
}

// activeProfile devuelve el perfil activo (puede ser vacío si la config es legacy).
func (a *App) activeProfile() config.Profile {
	p, ok := a.cfg.ProfileByName(a.profileName)
	if !ok {
		return config.Profile{Name: a.profileName}
	}
	return p
}

// profileCloudflareOverride devuelve el override R2 del perfil activo, si lo hay.
func (a *App) profileCloudflareOverride() *config.PlatformCloudflareOverride {
	o := a.activeProfile().Overrides.Cloudflare
	return o
}

// profileServerOverride devuelve el override UNC del perfil activo, si lo hay.
func (a *App) profileServerOverride() *config.PlatformServerOverride {
	return a.activeProfile().Overrides.RemoteServer
}

func orInt(v *int, def int) int {
	if v == nil {
		return def
	}
	return *v
}

// syncAfterBackup resuelve el flag desde el schedule efectivo del perfil (D9,
// D1): propio o heredado del global.
func (a *App) syncAfterBackup() bool {
	s := a.cfg.EffectiveSchedule(a.activeProfile())
	if !s.SyncAfterBackup && isZeroSchedule(s) {
		// Schedule vacío sin flag = conducta histórica (subir tras backup).
		return true
	}
	return s.SyncAfterBackup
}

// cloudflareKeep resuelve el keep R2: override del perfil, si no el global (D5).
func (a *App) cloudflareKeep() int {
	if o := a.profileCloudflareOverride(); o != nil {
		return orInt(o.Keep, a.cfg.Cloudflare.Keep)
	}
	return a.cfg.Cloudflare.Keep
}

// sqlConnectOptions construye los parámetros de conexión resueltos para SQL Server.
func (a *App) sqlConnectOptions() sqlbackup.ConnectOptions {
	return sqlbackup.ConnectOptions{
		Server:          a.cfg.Server,
		Database:        a.cfg.Database,
		AuthMode:        a.cfg.AuthMode,
		User:            a.cfg.User,
		Password:        a.cfg.Password,
		LoginTimeoutSec: a.cfg.LoginTimeoutSec,
	}
}

// cloudflareTimeoutSec devuelve el timeout configurado para subidas a R2
// (reparación D5: antes era un literal de 10 minutos).
func (a *App) cloudflareTimeoutSec() int {
	if o := a.profileCloudflareOverride(); o != nil {
		return orInt(o.TimeoutSec, a.cfg.Cloudflare.TimeoutSec)
	}
	if a.cfg.Cloudflare.TimeoutSec > 0 {
		return a.cfg.Cloudflare.TimeoutSec
	}
	return 600
}

// cloudflareRetries resuelve los reintentos de subida a R2 (D5).
func (a *App) cloudflareRetries() int {
	if o := a.profileCloudflareOverride(); o != nil {
		return orInt(o.UploadRetries, a.cfg.Cloudflare.UploadRetries)
	}
	return a.cfg.Cloudflare.UploadRetries
}

// serverKeep resuelve el keep UNC: override del perfil, si no el global (D5).
func (a *App) serverKeep() int {
	if o := a.profileServerOverride(); o != nil {
		return orInt(o.Keep, a.cfg.RemoteServer.Keep)
	}
	return a.cfg.RemoteServer.Keep
}

// serverTimeoutSec devuelve el timeout configurado para copias al servidor UNC.
func (a *App) serverTimeoutSec() int {
	if o := a.profileServerOverride(); o != nil {
		return orInt(o.TimeoutSec, a.cfg.RemoteServer.TimeoutSec)
	}
	if a.cfg.RemoteServer.TimeoutSec > 0 {
		return a.cfg.RemoteServer.TimeoutSec
	}
	return 300
}

// backendTimeout devuelve el timeout de subida para un backend según plataforma.
func (a *App) backendTimeout(b storage.Backend) time.Duration {
	if strings.EqualFold(b.Name(), "server") {
		return time.Duration(a.serverTimeoutSec()) * time.Second
	}
	return time.Duration(a.cloudflareTimeoutSec()) * time.Second
}

// RemoteSyncTimeout devuelve el timeout configurado para operaciones remotas
// (reparación D5: antes la TUI usaba un literal de 10 minutos).
func (a *App) RemoteSyncTimeout() time.Duration {
	return time.Duration(a.cloudflareTimeoutSec()) * time.Second
}

// isZeroSchedule reporta si el schedule no tiene ningún campo configurado.
func isZeroSchedule(s config.ScheduleConfig) bool {
	return !s.Enabled && s.Mode == "" && s.TimeOfDay == "" && len(s.Weekdays) == 0 &&
		s.IntervalMinutes == 0 && s.MaxDurationMin == 0 && s.TaskName == "" && !s.SyncAfterBackup
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


