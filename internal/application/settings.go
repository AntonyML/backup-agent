package application

import (
	"path/filepath"

	"femucaribe-backup-agent/internal/config"
)

// Settings es la configuración editable del agente expuesta a la capa UI.
// Es un alias de config.Config: la UI nunca importa el paquete config
// directamente (regla de Clean Architecture, igual que storage y state).
type Settings = config.Config

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
	return nil
}
