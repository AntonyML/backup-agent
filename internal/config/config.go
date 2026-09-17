package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ScheduleConfig define la programación de las tareas automáticas (qué días y a qué hora).
// Todos los valores son editables desde Ajustes de la TUI; nada queda hardcodeado.
type ScheduleConfig struct {
	// Enabled indica si las tareas automáticas (backup/sync) están activas.
	Enabled bool `json:"enabled"`
	// Mode define la cadencia: "daily", "weekly" o "interval".
	Mode string `json:"mode"`
	// TimeOfDay es la hora de inicio en formato HH:MM (modos daily/weekly).
	TimeOfDay string `json:"time_of_day"`
	// Weekdays son los días activos (modo weekly): mon,tue,wed,thu,fri,sat,sun.
	Weekdays []string `json:"weekdays,omitempty"`
	// IntervalMinutes es cada cuántos minutos corre la tarea (modo interval).
	IntervalMinutes int `json:"interval_minutes"`
	// MaxDurationMin es el tiempo máximo permitido por corrida. 0 = sin límite.
	MaxDurationMin int `json:"max_duration_minutes"`
	// TaskName es el nombre de la tarea en el Programador de Windows.
	TaskName string `json:"task_name"`
	// SyncAfterBackup indica si tras un backup exitoso se dispara la sincronización remota.
	SyncAfterBackup bool `json:"sync_after_backup"`
}

// CloudflareConfig define el destino Cloudflare R2 (cuántas copias se conservan en la nube).
// Las credenciales (endpoint, bucket, keys) siguen viviendo cifradas en config.dat.
type CloudflareConfig struct {
	Enabled       bool `json:"enabled"`
	Keep          int  `json:"keep"`
	TimeoutSec    int  `json:"timeout_sec"`
	UploadRetries int  `json:"upload_retries"`
}

// ValidWeekdays lista los días aceptados por schedule.weekdays.
var ValidWeekdays = []string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"}

// ValidScheduleModes lista los modos aceptados por schedule.mode.
var ValidScheduleModes = []string{"daily", "weekly", "interval"}

// ServerStorageConfig define la configuración para copia a servidor remoto / recurso compartido.
type ServerStorageConfig struct {
	Enabled    bool   `json:"enabled"`
	RemotePath string `json:"remote_path"`
	Keep       int    `json:"keep"`
	TimeoutSec int    `json:"timeout_sec"`
}

// SupabaseStorageConfig define el destino Supabase Storage (bucket de backups).
type SupabaseStorageConfig struct {
	Enabled    bool   `json:"enabled"`
	Bucket     string `json:"bucket"`
	Keep       int    `json:"keep"`
	TimeoutSec int    `json:"timeout_sec"`
}

// SupabaseConfig define la configuración para el registro centralizado de eventos y storage en Supabase (Fase 4).
type SupabaseConfig struct {
	Enabled    bool                  `json:"enabled"`
	URL        string                `json:"url"`
	APIKey     string                `json:"api_key,omitempty"`
	TimeoutSec int                   `json:"timeout_sec"`
	Storage    SupabaseStorageConfig `json:"storage"`
}

// Config es toda la configuración de Fase 1 y backends desacoplados.
type Config struct {
	// BackupDir es la ruta local donde se escriben los .bak.
	// Debe ser una ruta local al servidor SQL (BACKUP DATABASE escribe
	// desde el servicio SQL, no desde este proceso).
	BackupDir string `json:"backup_dir"`
	// Server es host o host\instancia, ej: Caproba01\vbadilla.
	Server string `json:"server"`
	// Database es la base a respaldar, ej: CONTABILIDAD.
	Database string `json:"database"`
	// Retain es cuántas copias .bak finales conservar (rotación).
	Retain int `json:"retain"`
	// LoginTimeoutSec es el timeout de conexión al SQL Server.
	LoginTimeoutSec int `json:"login_timeout_sec"`
	// BackupTimeoutSec es el timeout máximo del BACKUP DATABASE.
	// 0 = sin timeout (no recomendado; el Task Scheduler igual puede matar).
	BackupTimeoutSec int `json:"backup_timeout_sec"`
	// AuthMode define el modo de autenticación SQL: "windows" (default) o "sql".
	AuthMode string `json:"auth_mode,omitempty"`
	// User es el usuario para autenticación SQL (ej: "sa"). Vacío en modo "windows".
	User string `json:"user,omitempty"`
	// Password es la contraseña para autenticación SQL. Vacío en modo "windows".
	Password string `json:"password,omitempty"`
	// SQLBackupDir es la ruta interna vista por el motor SQL (ej: /var/opt/mssql/backup en Docker).
	// Si está vacía, se utiliza backup_dir.
	SQLBackupDir string `json:"sql_backup_dir,omitempty"`
	// RemoteServer configura el almacenamiento remoto en servidor Windows / UNC (Fase 3).
	RemoteServer ServerStorageConfig `json:"remote_server,omitempty"`
	// Supabase configura el registro remoto de eventos (Fase 4).
	Supabase SupabaseConfig `json:"supabase,omitempty"`
	// Cloudflare configura el destino Cloudflare R2 (rotación en la nube).
	Cloudflare CloudflareConfig `json:"cloudflare,omitempty"`
	// Schedule configura día, hora y cantidad de corridas de las tareas.
	// Actúa como schedule GLOBAL/default: los perfiles sin schedule propio lo heredan (D1).
	Schedule ScheduleConfig `json:"schedule,omitempty"`
	// Profiles son los perfiles de backup (D10). Vacío = config legacy;
	// EnsureMigrated la convierte al perfil inicial "full".
	Profiles []Profile `json:"profiles,omitempty"`
	// ActiveProfile es el nombre del perfil activo (usado sin --profile).
	ActiveProfile string `json:"active_profile,omitempty"`
}

// Default devuelve la configuración de producción inicial.
func Default() Config {
	def := Config{
		BackupDir:        `C:\Backups\`,
		Server:           `localhost`,
		Database:         "EMPRESA_DB",
		Retain:           3,
		LoginTimeoutSec:  15,
		BackupTimeoutSec: 3600,
		RemoteServer: ServerStorageConfig{
			Enabled:    false,
			RemotePath: "",
			Keep:       10,
			TimeoutSec: 300,
		},
		Supabase: SupabaseConfig{
			Enabled:    false,
			URL:        "",
			TimeoutSec: 10,
			Storage: SupabaseStorageConfig{
				Enabled:    false,
				Bucket:     "backups",
				Keep:       30,
				TimeoutSec: 600,
			},
		},
		Cloudflare: CloudflareConfig{
			Enabled:       false,
			Keep:          1, // D5: coincide con la conducta productiva previa (rotación R2 conservaba 1 copia)
			TimeoutSec:    600,
			UploadRetries: 3,
		},
		Schedule: ScheduleConfig{
			Enabled:         false,
			Mode:            "daily",
			TimeOfDay:       "23:00",
			Weekdays:        []string{"mon", "tue", "wed", "thu", "fri"},
			IntervalMinutes: 60,
			MaxDurationMin:  90,
			TaskName:        "BackupAgent-Diario",
			SyncAfterBackup: true,
		},
	}
	// El default ya viene migrado: perfil inicial "full" (D11) que hereda el schedule global.
	cfg, _ := def.EnsureMigrated()
	return cfg
}

var dbNameRe = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

// timeOfDayRe valida HH:MM en formato 24 horas.
var timeOfDayRe = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)

// Load lee config.json. Si no existe, devuelve Default() sin error
// (primera instalación: convención sobre configuración).
// Un archivo parcial hace override solo de los campos presentes;
// el resto queda en defaults. Siempre valida al final.
func Load(path string) (Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return Config{}, fmt.Errorf("config: leer %s: %w", path, err)
	}
	// Decodifico sobre los defaults para que un json parcial funcione.
	// Truco: unmarshal a map y luego aplicar campo por campo presente.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return Config{}, fmt.Errorf("config: %s corrupto: %w", path, err)
	}
	merged, err := json.Marshal(cfg)
	if err != nil {
		return Config{}, fmt.Errorf("config: interno: %w", err)
	}
	var base map[string]json.RawMessage
	if err := json.Unmarshal(merged, &base); err != nil {
		return Config{}, fmt.Errorf("config: interno: %w", err)
	}
	for k, v := range raw {
		base[k] = v
	}
	merged, err = json.Marshal(base)
	if err != nil {
		return Config{}, fmt.Errorf("config: interno: %w", err)
	}
	if err := json.Unmarshal(merged, &cfg); err != nil {
		return Config{}, fmt.Errorf("config: %s inválido: %w", path, err)
	}
	// Migración automática e idempotente legacy -> perfiles (D11). Una config
	// vieja carga sin error y se re-escribe en formato nuevo al primer Save.
	cfg, _ = cfg.EnsureMigrated()
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Validate chequea que la config tenga sentido antes de tocar SQL o disco.
func (c Config) Validate() error {
	if c.BackupDir == "" {
		return fmt.Errorf("config: backup_dir vacío")
	}
	if c.Server == "" {
		return fmt.Errorf("config: server vacío")
	}
	if !dbNameRe.MatchString(c.Database) {
		return fmt.Errorf("config: database %q inválido (solo letras, dígitos y _)", c.Database)
	}
	if c.Retain < 1 {
		return fmt.Errorf("config: retain debe ser >= 1, recibí %d", c.Retain)
	}
	if c.LoginTimeoutSec < 0 {
		return fmt.Errorf("config: login_timeout_sec no puede ser negativo")
	}
	if c.BackupTimeoutSec < 0 {
		return fmt.Errorf("config: backup_timeout_sec no puede ser negativo")
	}
	if strings.ToLower(strings.TrimSpace(c.AuthMode)) == "sql" {
		if strings.TrimSpace(c.User) == "" {
			return fmt.Errorf("config: user es obligatorio cuando auth_mode es 'sql'")
		}
	}
	if c.RemoteServer.Enabled {
		if strings.TrimSpace(c.RemoteServer.RemotePath) == "" {
			return fmt.Errorf("config: remote_server.remote_path es obligatorio cuando enabled es true")
		}
		if c.RemoteServer.Keep < 1 {
			return fmt.Errorf("config: remote_server.keep debe ser >= 1, recibí %d", c.RemoteServer.Keep)
		}
		if c.RemoteServer.TimeoutSec < 0 {
			return fmt.Errorf("config: remote_server.timeout_sec no puede ser negativo")
		}
	}
	if c.Supabase.Enabled {
		if strings.TrimSpace(c.Supabase.URL) == "" {
			return fmt.Errorf("config: supabase.url es obligatorio cuando enabled es true")
		}
		if !strings.HasPrefix(c.Supabase.URL, "http://") && !strings.HasPrefix(c.Supabase.URL, "https://") {
			return fmt.Errorf("config: supabase.url debe ser una URL http o https válida")
		}
		if c.Supabase.TimeoutSec < 0 {
			return fmt.Errorf("config: supabase.timeout_sec no puede ser negativo")
		}
		if c.Supabase.Storage.Enabled {
			if strings.TrimSpace(c.Supabase.Storage.Bucket) == "" {
				return fmt.Errorf("config: supabase.storage.bucket no puede estar vacío")
			}
			if c.Supabase.Storage.Keep < 1 {
				return fmt.Errorf("config: supabase.storage.keep debe ser >= 1, recibí %d", c.Supabase.Storage.Keep)
			}
			if c.Supabase.Storage.TimeoutSec < 0 {
				return fmt.Errorf("config: supabase.storage.timeout_sec no puede ser negativo")
			}
		}
	} else if c.Supabase.Storage.Enabled {
		return fmt.Errorf("config: supabase.storage requiere que supabase.enabled sea true")
	}
	if c.Cloudflare.Enabled {
		if c.Cloudflare.Keep < 1 {
			return fmt.Errorf("config: cloudflare.keep debe ser >= 1, recibí %d", c.Cloudflare.Keep)
		}
		if c.Cloudflare.TimeoutSec < 0 {
			return fmt.Errorf("config: cloudflare.timeout_sec no puede ser negativo")
		}
		if c.Cloudflare.UploadRetries < 0 {
			return fmt.Errorf("config: cloudflare.upload_retries no puede ser negativo")
		}
	}
	if err := c.Schedule.Validate(); err != nil {
		return err
	}
	if err := c.ValidateProfiles(); err != nil {
		return err
	}
	return nil
}

// Validate chequea la programación. Los rangos siempre se validan; los campos
// vacíos solo se rechazan cuando la tarea está habilitada.
func (s ScheduleConfig) Validate() error {
	if s.Mode != "" && !contains(ValidScheduleModes, s.Mode) {
		return fmt.Errorf("config: schedule.mode %q inválido (daily, weekly o interval)", s.Mode)
	}
	if s.TimeOfDay != "" && !timeOfDayRe.MatchString(s.TimeOfDay) {
		return fmt.Errorf("config: schedule.time_of_day %q inválido (formato HH:MM)", s.TimeOfDay)
	}
	for _, d := range s.Weekdays {
		if !contains(ValidWeekdays, strings.ToLower(strings.TrimSpace(d))) {
			return fmt.Errorf("config: schedule.weekdays contiene %q inválido (mon..sun)", d)
		}
	}
	if s.IntervalMinutes != 0 && s.IntervalMinutes < 5 {
		return fmt.Errorf("config: schedule.interval_minutes debe ser >= 5, recibí %d", s.IntervalMinutes)
	}
	if s.MaxDurationMin < 0 {
		return fmt.Errorf("config: schedule.max_duration_minutes no puede ser negativo")
	}
	if !s.Enabled {
		return nil
	}
	if strings.TrimSpace(s.TaskName) == "" {
		return fmt.Errorf("config: schedule.task_name es obligatorio cuando la programación está habilitada")
	}
	switch s.Mode {
	case "daily":
		if s.TimeOfDay == "" {
			return fmt.Errorf("config: schedule.time_of_day es obligatorio en modo daily")
		}
	case "weekly":
		if s.TimeOfDay == "" {
			return fmt.Errorf("config: schedule.time_of_day es obligatorio en modo weekly")
		}
		if len(s.Weekdays) == 0 {
			return fmt.Errorf("config: schedule.weekdays no puede estar vacío en modo weekly")
		}
	case "interval":
		if s.IntervalMinutes < 5 {
			return fmt.Errorf("config: schedule.interval_minutes debe ser >= 5 en modo interval")
		}
	}
	return nil
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

// EffectiveSQLBackupDir devuelve la ruta de destino que recibe el comando BACKUP DATABASE.
// Si SQLBackupDir está configurado (caso Docker/Linux), se usa esa ruta; de lo contrario, se usa BackupDir.
func (c Config) EffectiveSQLBackupDir() string {
	if strings.TrimSpace(c.SQLBackupDir) != "" {
		return strings.TrimSpace(c.SQLBackupDir)
	}
	return c.BackupDir
}

// Save escribe la configuración completa en path de forma atómica (tmp + rename).
// Valida antes de tocar disco: nunca deja un config.json inconsistente.
func Save(path string, c Config) error {
	// Nunca se persiste una config sin migrar: el formato en disco es siempre
	// el nuevo (con perfiles). Idempotente si ya estaba migrada.
	c, _ = c.EnsureMigrated()
	if err := c.Validate(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("config: serializar: %w", err)
	}
	data = append(data, '\n')

	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("config: crear %s: %w", dir, err)
		}
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("config: escribir %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("config: reemplazar %s: %w", path, err)
	}
	return nil
}
