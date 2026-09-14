package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestLoad_LegacyConfig_MigratesToFullProfile verifica la migración D11:
// una config sin perfiles se convierte en un perfil "full" con las
// plataformas habilitadas y el schedule global heredado.
func TestLoad_LegacyConfig_MigratesToFullProfile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	legacy := `{
		"backup_dir": "C:\\Backups\\",
		"server": "localhost",
		"database": "CONTABILIDAD",
		"retain": 3,
		"remote_server": {"enabled": true, "remote_path": "\\\\srv\\bkp", "keep": 10, "timeout_sec": 300},
		"cloudflare": {"enabled": true, "keep": 1, "timeout_sec": 600, "upload_retries": 3},
		"schedule": {"enabled": true, "mode": "daily", "time_of_day": "23:00", "task_name": "T"}
	}`
	if err := os.WriteFile(path, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load de config legacy no debe fallar: %v", err)
	}
	if len(cfg.Profiles) != 1 {
		t.Fatalf("esperaba 1 perfil migrado, hay %d", len(cfg.Profiles))
	}
	p := cfg.Profiles[0]
	if p.Name != InitialProfileName || p.Kind != KindFull {
		t.Errorf("perfil inicial incorrecto: %+v", p)
	}
	if cfg.ActiveProfile != InitialProfileName {
		t.Errorf("active_profile debería ser %q, es %q", InitialProfileName, cfg.ActiveProfile)
	}
	if len(p.Platforms) != 2 {
		t.Errorf("perfil full debería tener ambas plataformas, tiene %v", p.Platforms)
	}
	if p.Schedule != nil {
		t.Errorf("el perfil migrado no debe llevar schedule propio (hereda el global): %+v", p.Schedule)
	}
	if cfg.EffectiveSchedule(p).TimeOfDay != "23:00" {
		t.Errorf("el perfil debería heredar el schedule global: %+v", cfg.EffectiveSchedule(p))
	}

	// Idempotencia: re-guardar escribe el formato nuevo sin duplicar perfiles.
	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save migrado: %v", err)
	}
	again, err := Load(path)
	if err != nil {
		t.Fatalf("re-Load: %v", err)
	}
	if len(again.Profiles) != 1 || again.Profiles[0].Name != InitialProfileName {
		t.Errorf("migración no idempotente: %+v", again.Profiles)
	}

	raw, _ := os.ReadFile(path)
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if _, ok := doc["profiles"]; !ok {
		t.Error("el archivo re-guardado debería incluir \"profiles\"")
	}
}

// TestEnsureMigrated_Idempotent verifica que migrar dos veces no cambia nada.
func TestEnsureMigrated_Idempotent(t *testing.T) {
	cfg := Default()
	if _, changed := cfg.EnsureMigrated(); changed {
		t.Error("Default() ya viene migrado: EnsureMigrated no debería cambiar nada")
	}
	legacy := Config{BackupDir: `C:\B`, Server: "srv", Database: "DB", Retain: 1}
	migrated, changed := legacy.EnsureMigrated()
	if !changed {
		t.Error("una config legacy debería reportar cambios al migrar")
	}
	if _, changed2 := migrated.EnsureMigrated(); changed2 {
		t.Error("la segunda migración debería ser no-op")
	}
}

// TestEnabledPlatforms verifica la derivación de plataformas habilitadas.
func TestEnabledPlatforms(t *testing.T) {
	c := Default()
	if got := c.EnabledPlatforms(); len(got) != 0 {
		t.Errorf("sin plataformas habilitadas esperaba lista vacía, dio %v", got)
	}
	c.RemoteServer.Enabled = true
	if got := c.EnabledPlatforms(); len(got) != 1 || got[0] != PlatformServer {
		t.Errorf("esperaba [remote_server], dio %v", got)
	}
	c.Cloudflare.Enabled = true
	got := c.EnabledPlatforms()
	if len(got) != 2 || got[0] != PlatformCloudflare || got[1] != PlatformServer {
		t.Errorf("esperaba [cloudflare remote_server] ordenado, dio %v", got)
	}
}

// TestCloudflareDefaults preserva la conducta productiva (D5).
func TestCloudflareDefaults(t *testing.T) {
	if k := Default().Cloudflare.Keep; k != 1 {
		t.Errorf("cloudflare.keep default debería ser 1 (rotación R2 conservaba 1), es %d", k)
	}
	if k := Default().RemoteServer.Keep; k != 10 {
		t.Errorf("remote_server.keep default debería ser 10, es %d", k)
	}
	if s := Default().Cloudflare.TimeoutSec; s != 600 {
		t.Errorf("cloudflare.timeout_sec default debería ser 600 (= 10 min previos), es %d", s)
	}
}