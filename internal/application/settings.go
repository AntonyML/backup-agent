package application

import (
	"os"
	"path/filepath"

	"femucaribe-backup-agent/internal/config"
	"femucaribe-backup-agent/internal/events"
)

// Settings es la configuración editable del agente expuesta a la capa UI.
// Es un alias de config.Config: la UI nunca importa el paquete config
// directamente (regla de Clean Architecture, igual que storage y state).
type Settings = config.Config
type Profile = config.Profile
type ProfileOverrides = config.ProfileOverrides
type PlatformCloudflareOverride = config.PlatformCloudflareOverride
type PlatformServerOverride = config.PlatformServerOverride
type PlatformSupabaseOverride = config.PlatformSupabaseOverride
type CloudflareConfig = config.CloudflareConfig
type ServerStorageConfig = config.ServerStorageConfig
type SupabaseStorageConfig = config.SupabaseStorageConfig
type ScheduleConfig = config.ScheduleConfig
type SupabaseConfig = config.SupabaseConfig

const (
	KindLocal          = config.KindLocal
	KindDev            = config.KindDev
	KindHibrido        = config.KindHibrido
	KindFull           = config.KindFull
	PlatformCloudflare = config.PlatformCloudflare
	PlatformServer     = config.PlatformServer
	PlatformSupabase   = config.PlatformSupabase
	PlatformLocal      = config.PlatformLocal
)

// GetSettings devuelve una copia de la configuración activa.
func (a *App) GetSettings() Settings {
	return a.cfg
}

// ConfigPath devuelve la ruta de config.json usada por esta instancia.
// Si no fue inyectada explícitamente, se asume junto a state.json.
func (a *App) ConfigPath() string {
	if a.configPath != "" {
		return a.configPath
	}
	return filepath.Join(filepath.Dir(a.statePath), "config.json")
}

// SaveSettings valida y persiste la configuración en config.json.
// Los cambios que dependen del arranque (backends, credenciales) recién
// toman efecto al reiniciar el agente; el resto se refleja de inmediato.
func (a *App) SaveSettings(s Settings) error {
	if err := s.Validate(); err != nil {
		return err
	}
	if err := config.Save(a.ConfigPath(), s); err != nil {
		return err
	}
	a.cfg = s

	// Actualizar cliente de eventos Supabase en caliente
	if s.Supabase.Enabled {
		apiKey := s.Supabase.APIKey
		if apiKey == "" {
			apiKey = os.Getenv("SUPABASE_KEY")
		}
		if apiKey == "" {
			apiKey = os.Getenv("SUPABASE_API_KEY")
		}
		if apiKey == "" {
			apiKey = os.Getenv("SUPABASE_ACCESS_TOKEN")
		}
		if apiKey != "" {
			a.eventRepo = events.NewSupabaseRepository(s.Supabase, apiKey, nil)
		} else {
			a.eventRepo = nil
		}
	} else {
		a.eventRepo = nil
	}

	return nil
}
