package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"femucaribe-backup-agent/internal/application"
	"femucaribe-backup-agent/internal/config"
	"femucaribe-backup-agent/internal/events"
	"femucaribe-backup-agent/internal/logging"
	"femucaribe-backup-agent/internal/secrets"
	"femucaribe-backup-agent/internal/storage"
	"femucaribe-backup-agent/internal/storage/local"
	"femucaribe-backup-agent/internal/storage/r2"
	"femucaribe-backup-agent/internal/storage/server"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

const (
	ExitOK          = 0 // Operación exitosa o backup ya realizado hoy
	ExitGeneralErr  = 1 // Error general de ejecución
	ExitConfigErr   = 2 // Configuración inválida o faltante
	ExitPendingSync = 3 // Backup local exitoso pero sincronización remota pendiente
	ExitLocked      = 4 // Otra instancia en ejecución
)

var (
	ErrConfig = errors.New("error de configuración")
	ErrNoTTY  = errors.New("se requiere un subcomando explícito en entornos no interactivos")
)

var isTerminal = func(f *os.File) bool {
	if f == nil {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

type RootOptions struct {
	ConfigPath string
	Profile    string
}

// resolveProfile decide con qué perfil opera el comando: el pedido por
// --profile o, si no se indicó, el perfil activo de la config (D1/D2).
// Un perfil inexistente es error de configuración (exit 2).
func resolveProfile(cfg config.Config, requested string) (string, error) {
	name := strings.TrimSpace(requested)
	if name == "" {
		name = cfg.ActiveProfile
	}
	if name == "" {
		name = config.InitialProfileName
	}
	if _, ok := cfg.ProfileByName(name); !ok {
		return "", fmt.Errorf("%w: el perfil %q no existe (disponibles: %s)",
			ErrConfig, name, strings.Join(profileNames(cfg), ", "))
	}
	return name, nil
}

func profileNames(cfg config.Config) []string {
	names := make([]string, 0, len(cfg.Profiles))
	for _, p := range cfg.Profiles {
		names = append(names, p.Name)
	}
	return names
}

// NewRootCmd crea el comando raíz y registra todos los subcomandos.
// appProvider recibe la ruta de config y el perfil resuelto.
func NewRootCmd(exeDir string, appProvider func(cfgPath, profile string) (*application.App, error)) *cobra.Command {
	var configPath string
	var profileFlag string

	providerFor := func() (*application.App, error) { return appProvider(configPath, profileFlag) }

	cmd := &cobra.Command{
		Use:   "backup-agent",
		Short: "Agente de backups FEMUCARIBE",
		Long:  "Agente de backups para SQL Server con subida a Cloudflare R2 y rotación automática.",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Comportamiento del comando raíz sin subcomando:
			// Si hay TTY -> iniciar menú interactivo.
			// Si NO hay TTY -> mostrar --help y terminar con código != 0.
			if isTerminal(os.Stdin) {
				app, err := providerFor()
				if err != nil {
					return err
				}
				return runInteractive(cmd.Context(), app, exeDir, os.Stdin, cmd.OutOrStdout())
			}

			_ = cmd.Help()
			return ErrNoTTY
		},
	}

	cmd.PersistentFlags().StringVar(&configPath, "config", "", "ruta a config.json (default: config.json junto al binario)")
	cmd.PersistentFlags().StringVar(&profileFlag, "profile", "", "perfil de backup (default: active_profile de config.json)")

	// Subcomandos
	cmd.AddCommand(newBackupCmd(providerFor))
	cmd.AddCommand(newSyncCmd(providerFor))
	cmd.AddCommand(newStatusCmd(providerFor))
	cmd.AddCommand(newLogsCmd(providerFor))
	cmd.AddCommand(newConfigureCmd(exeDir))
	cmd.AddCommand(newInteractiveCmd(exeDir, providerFor))
	cmd.AddCommand(newProfileCmd(exeDir, &configPath, &profileFlag))
	cmd.AddCommand(newScheduleCmd(exeDir, &configPath, &profileFlag))
	cmd.AddCommand(newConfigCmd(exeDir, &configPath))
	cmd.AddCommand(newDoctorCmd(exeDir, appProvider))

	return cmd
}

// BuildDefaultApp arma la instancia productiva de Application inyectando dependencias.
// El perfil se resuelve contra la config: inexistente -> ErrConfig (exit 2).
func BuildDefaultApp(exeDir string, cfgPath string, profile string) (*application.App, error) {
	if cfgPath == "" {
		cfgPath = filepath.Join(exeDir, "config.json")
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, fmt.Errorf("%w: config (%s): %v", ErrConfig, cfgPath, err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("%w: validación: %v", ErrConfig, err)
	}
	resolved, err := resolveProfile(cfg, profile)
	if err != nil {
		return nil, err
	}

	logger, writer := logging.New(filepath.Join(exeDir, "logs"))

	statePath := filepath.Join(exeDir, "state.json")
	// Lock POR PERFIL (D2): cada perfil tiene su propio agent-<perfil>.lock.
	lockPath := filepath.Join(exeDir, "agent-"+resolved+".lock")
	localBackend := local.New(cfg.BackupDir)

	var backends []storage.Backend
	datPath := filepath.Join(exeDir, "config.dat")
	creds, err := secrets.Load(datPath)
	switch {
	case err != nil && cfg.Cloudflare.Enabled:
		logger.Warn("Cloudflare R2 habilitado pero no se pudieron leer credenciales en config.dat; no se subirá a R2", "error", err)
	case err == nil && creds != nil && !cfg.Cloudflare.Enabled:
		// Aviso de migración (D9): antes el agente subía a R2 con solo tener
		// credenciales; ahora Cloudflare.Enabled gobierna la subida.
		logger.Warn("existen credenciales de R2 en config.dat pero cloudflare.enabled=false: NO se subirá a R2 hasta habilitarla")
	case err == nil && creds != nil && cfg.Cloudflare.Enabled:
		r2Client, err := r2.New(context.Background(), *creds)
		if err == nil && r2Client != nil {
			backends = append(backends, r2.NewBackend(r2Client, cfg.Database))
		} else {
			logger.Warn("cliente R2 no pudo inicializarse", "error", err)
		}
	}

	if cfg.RemoteServer.Enabled {
		serverBackend := server.New(server.Config{
			Enabled:    cfg.RemoteServer.Enabled,
			RemotePath: cfg.RemoteServer.RemotePath,
			Keep:       cfg.RemoteServer.Keep,
			TimeoutSec: cfg.RemoteServer.TimeoutSec,
			Database:   cfg.Database,
		}, logger)
		backends = append(backends, serverBackend)
	}

	var eventRepo events.EventRepository
	if cfg.Supabase.Enabled {
		apiKey := cfg.Supabase.APIKey
		if apiKey == "" {
			apiKey = os.Getenv("SUPABASE_KEY")
		}
		if apiKey == "" {
			apiKey = os.Getenv("SUPABASE_API_KEY")
		}
		if apiKey == "" {
			apiKey = os.Getenv("SUPABASE_ACCESS_TOKEN")
		}
		if apiKey == "" {
			logger.Warn("supabase habilitado pero no se encontró API key en config.json ni en variables de entorno (SUPABASE_KEY / SUPABASE_API_KEY)")
		} else {
			eventRepo = events.NewSupabaseRepository(cfg.Supabase, apiKey, nil)
		}
	}

	return application.New(application.Options{
		Config:       cfg,
		ConfigPath:   cfgPath,
		StatePath:    statePath,
		SecretsPath:  datPath,
		LockPath:     lockPath,
		LogDir:       filepath.Join(exeDir, "logs"),
		Profile:      resolved,
		Backends:     backends,
		LocalBackend: localBackend,
		EventRepo:    eventRepo,
		Logger:        logger,
		LogController: writer,
	}), nil
}

// ExitCodeForError traduce errores a códigos de salida centralizados.
func ExitCodeForError(err error) int {
	if err == nil || errors.Is(err, application.ErrAlreadyRanToday) {
		return ExitOK
	}
	if errors.Is(err, application.ErrInvalidConfig) || errors.Is(err, ErrConfig) ||
		errors.Is(err, application.ErrUnknownProfile) {
		return ExitConfigErr
	}
	if errors.Is(err, application.ErrPendingSync) {
		return ExitPendingSync
	}
	if errors.Is(err, application.ErrLocked) {
		return ExitLocked
	}
	return ExitGeneralErr
}

// Execute inicializa y corre el CLI devolviendo el exit code correspondiente.
func Execute() int {
	cleanup := initConsoleEncoding()
	defer cleanup()

	dir := exeDir()
	cmd := NewRootCmd(dir, func(cfgPath, profile string) (*application.App, error) {
		return BuildDefaultApp(dir, cfgPath, profile)
	})

	err := cmd.Execute()
	return ExitCodeForError(err)
}

func exeDir() string {
	if exe, err := os.Executable(); err == nil {
		if dir := filepath.Dir(exe); dir != "" {
			return dir
		}
	}
	if cwd, err := os.Getwd(); err == nil {
		return cwd
	}
	return "."
}
