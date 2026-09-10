package config

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// ServerStorageConfig define la configuración para copia a servidor remoto / recurso compartido.
type ServerStorageConfig struct {
	Enabled    bool   `json:"enabled"`
	RemotePath string `json:"remote_path"`
	Keep       int    `json:"keep"`
	TimeoutSec int    `json:"timeout_sec"`
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
	// RemoteServer configura el almacenamiento remoto en servidor Windows / UNC (Fase 3).
	RemoteServer ServerStorageConfig `json:"remote_server,omitempty"`
}

// Default devuelve la configuración de producción FEMUCARIBE.
func Default() Config {
	return Config{
		BackupDir:        `C:\Backups\`,
		Server:           `Caproba01\vbadilla`,
		Database:         "CONTABILIDAD",
		Retain:           3,
		LoginTimeoutSec:  15,
		BackupTimeoutSec: 3600,
		RemoteServer: ServerStorageConfig{
			Enabled:    false,
			RemotePath: "",
			Keep:       10,
			TimeoutSec: 300,
		},
	}
}

var dbNameRe = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

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
	return nil
}
