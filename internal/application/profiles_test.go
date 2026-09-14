package application

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"femucaribe-backup-agent/internal/config"
	"femucaribe-backup-agent/internal/state"
	"femucaribe-backup-agent/internal/storage"
)

// appConPerfil arma una App con config migrada y dos backends (r2 + server).
func appConPerfil(t *testing.T, cfg config.Config, profile string, r2, server *mockBackend) *App {
	t.Helper()
	backends := []storage.Backend{}
	if r2 != nil {
		backends = append(backends, r2)
	}
	if server != nil {
		backends = append(backends, server)
	}
	tmp := t.TempDir()
	return New(Options{
		Config:       cfg,
		StatePath:    filepath.Join(tmp, "state.json"),
		LogDir:       tmp,
		Profile:      profile,
		Backends:     backends,
		LocalBackend: &mockBackend{name: "local"},
		SQLEngine:    &mockSQLEngine{},
	})
}

func backendNames(app *App) string {
	names := []string{}
	for _, b := range app.backends {
		names = append(names, b.Name())
	}
	return strings.Join(names, ",")
}

// TestFiltradoBackendsPorPerfil verifica que cada perfil vea solo sus plataformas.
func TestFiltradoBackendsPorPerfil(t *testing.T) {
	cfg := config.Default()
	cfg.RemoteServer.Enabled = true
	cfg.RemoteServer.RemotePath = `\\srv\bkp`
	cfg.Cloudflare.Enabled = true
	cfg.Profiles = []config.Profile{
		{Name: "todos", Kind: config.KindFull, Platforms: []string{config.PlatformCloudflare, config.PlatformServer}},
		{Name: "unc", Kind: config.KindDev, Platforms: []string{config.PlatformServer}},
		{Name: "solo", Kind: config.KindLocal},
	}
	cfg.ActiveProfile = "todos"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("fixture invÃ¡lida: %v", err)
	}

	app := appConPerfil(t, cfg, "unc", &mockBackend{name: "r2"}, &mockBackend{name: "server"})
	if got := backendNames(app); got != "server" {
		t.Errorf("perfil unc deberÃ­a ver solo server, ve %q", got)
	}

	app = appConPerfil(t, cfg, "solo", &mockBackend{name: "r2"}, &mockBackend{name: "server"})
	if got := backendNames(app); got != "" {
		t.Errorf("perfil local deberÃ­a no ver remotos, ve %q", got)
	}

	app = appConPerfil(t, cfg, "todos", &mockBackend{name: "r2"}, &mockBackend{name: "server"})
	if got := backendNames(app); got != "r2,server" {
		t.Errorf("perfil full deberÃ­a ver ambos, ve %q", got)
	}
}

// TestResolucionOverrides verifica overrides de perfil sobre globales (D5).
func TestResolucionOverrides(t *testing.T) {
	keepR2, toR2, retR2 := 7, 900, 9
	keepUNC := 30
	cfg := config.Default()
	cfg.Cloudflare.Enabled = true
	cfg.RemoteServer.Enabled = true
	cfg.RemoteServer.RemotePath = `\\srv\bkp`
	cfg.Profiles = []config.Profile{{
		Name: "dev", Kind: config.KindDev, Platforms: []string{config.PlatformCloudflare},
		Overrides: config.ProfileOverrides{
			Cloudflare:   &config.PlatformCloudflareOverride{Keep: &keepR2, TimeoutSec: &toR2, UploadRetries: &retR2},
			RemoteServer: &config.PlatformServerOverride{Keep: &keepUNC},
		},
	}}
	cfg.ActiveProfile = "dev"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("fixture invÃ¡lida: %v", err)
	}

	app := appConPerfil(t, cfg, "dev", &mockBackend{name: "r2"}, nil)
	if got := app.cloudflareKeep(); got != 7 {
		t.Errorf("cloudflareKeep deberÃ­a ser 7 (override), dio %d", got)
	}
	if got := app.cloudflareTimeoutSec(); got != 900 {
		t.Errorf("cloudflareTimeoutSec deberÃ­a ser 900, dio %d", got)
	}
	if got := app.cloudflareRetries(); got != 9 {
		t.Errorf("cloudflareRetries deberÃ­a ser 9, dio %d", got)
	}
	if got := app.serverKeep(); got != 30 {
		t.Errorf("serverKeep deberÃ­a ser 30 (override), dio %d", got)
	}

	// Sin overrides: globales (defaults: keep R2=1, UNC=10, timeout 600/300).
	app2 := appConPerfil(t, config.Default(), config.InitialProfileName, nil, nil)
	if got := app2.cloudflareKeep(); got != 1 {
		t.Errorf("sin override, cloudflareKeep deberÃ­a ser 1, dio %d", got)
	}
	if got := app2.serverKeep(); got != 10 {
		t.Errorf("sin override, serverKeep deberÃ­a ser 10, dio %d", got)
	}
	if got := app2.cloudflareTimeoutSec(); got != 600 {
		t.Errorf("sin override, timeout R2 deberÃ­a ser 600, dio %d", got)
	}
}

// TestSyncAfterBackupPorPerfil verifica que el flag se respeta desde el
// schedule efectivo (propio o heredado) con conducta default=true.
func TestSyncAfterBackupPorPerfil(t *testing.T) {
	// Heredado: global con false -> no sube.
	globalOff := config.Default()
	globalOff.Cloudflare.Enabled = true
	globalOff.Profiles = []config.Profile{{Name: "p", Kind: config.KindDev, Platforms: []string{config.PlatformCloudflare}}}
	globalOff.ActiveProfile = "p"
	globalOff.Schedule = config.ScheduleConfig{Enabled: true, Mode: "daily", TimeOfDay: "23:00", TaskName: "G", SyncAfterBackup: false}
	app := appConPerfil(t, globalOff, "p", &mockBackend{name: "r2"}, nil)
	if app.syncAfterBackup() {
		t.Error("con global SyncAfterBackup=false deberÃ­a no subir")
	}

	// Propio con false pero global con true -> no sube (manda el propio).
	ownFalse := &config.ScheduleConfig{Enabled: true, Mode: "daily", TimeOfDay: "22:00", TaskName: "P", SyncAfterBackup: false}
	globalOn := globalOff
	globalOn.Schedule.SyncAfterBackup = true
	globalOn.Profiles = []config.Profile{{
		Name: "p", Kind: config.KindDev, Platforms: []string{config.PlatformCloudflare}, Schedule: ownFalse,
	}}
	app = appConPerfil(t, globalOn, "p", &mockBackend{name: "r2"}, nil)
	if app.syncAfterBackup() {
		t.Error("el schedule propio con false deberÃ­a ganar al global")
	}

	// Schedule zero-value (legacy) -> true = conducta histÃ³rica.
	legacy := config.Config{BackupDir: t.TempDir(), Server: "s", Database: "D", Retain: 1}
	app = appConPerfil(t, legacy, "full", &mockBackend{name: "r2"}, nil)
	if !app.syncAfterBackup() {
		t.Error("schedule vacÃ­o deberÃ­a mantener la conducta histÃ³rica (true)")
	}
}

// TestIdempotenciaPorPerfil verifica que last_run_date no se pisa entre perfiles (D3).
func TestIdempotenciaPorPerfil(t *testing.T) {
	cfg := config.Default()
	cfg.Profiles = []config.Profile{
		{Name: "noche", Kind: config.KindLocal},
		{Name: "dia", Kind: config.KindLocal},
	}
	cfg.ActiveProfile = "noche"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("fixture invÃ¡lida: %v", err)
	}

	_, statePath, lockPath := setupTestApp(t, &mockSQLEngine{}, nil)

	// Escribimos corrida de hoy solo en el perfil "noche".
	st, _ := state.Load(statePath)
	st.Profile("noche").LastRunDate = state.Today()
	st.Profile("noche").LastBackupFile = "noche.bak"
	if err := state.Save(statePath, st); err != nil {
		t.Fatal(err)
	}

	appNoche := New(Options{
		Config: cfg, StatePath: statePath, LogDir: t.TempDir(),
		Profile: "noche", LockPath: lockPath, LocalBackend: &mockBackend{name: "local"}, SQLEngine: &mockSQLEngine{},
	})
	if err := appNoche.Backup(context.Background(), BackupOptions{}); !errors.Is(err, ErrAlreadyRanToday) {
		t.Errorf("noche deberÃ­a reportar idempotencia, dio: %v", err)
	}

	appDia := New(Options{
		Config: cfg, StatePath: statePath, LogDir: t.TempDir(),
		Profile: "dia", LockPath: lockPath, LocalBackend: &mockBackend{name: "local"}, SQLEngine: &mockSQLEngine{},
	})
	if err := appDia.Backup(context.Background(), BackupOptions{Force: true}); err != nil {
		t.Fatalf("dia deberÃ­a poder respaldar, dio: %v", err)
	}
	after, _ := state.Load(statePath)
	if !after.Profile("dia").RanOn(state.Today()) {
		t.Error("dia deberÃ­a tener last_run de hoy")
	}
	if after.Profile("noche").LastBackupFile != "noche.bak" {
		t.Errorf("noche no deberÃ­a haberse pisado: %+v", after.Profile("noche"))
	}
}

// TestListProfilesYDetalle verifica los DTOs de la TUI.
func TestListProfilesYDetalle(t *testing.T) {
	cfg := config.Default()
	cfg.RemoteServer.Enabled = true
	cfg.RemoteServer.RemotePath = `\\srv\bkp`
	cfg.Cloudflare.Enabled = true
	cfg.Profiles = []config.Profile{
		{Name: "nube", Kind: config.KindDev, Platforms: []string{config.PlatformCloudflare}},
	}
	cfg.ActiveProfile = "nube"
	cfg.Schedule.TaskName = "" // Sin explícito: aplica convención FEMUCARIBE-Backup-<perfil> (D2)
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	app := appConPerfil(t, cfg, "nube", nil, nil)

	infos := app.ListProfiles()
	if len(infos) != 1 || infos[0].Name != "nube" || !infos[0].Active {
		t.Errorf("ListProfiles inesperado: %+v", infos)
	}
	if infos[0].TaskName != config.TaskNamePrefix+"nube" {
		t.Errorf("task name por convenciÃ³n: %q", infos[0].TaskName)
	}

	if err := app.UseProfile("fantasma"); !errors.Is(err, ErrUnknownProfile) {
		t.Errorf("UseProfile con inexistente deberÃ­a dar ErrUnknownProfile, dio: %v", err)
	}
}

// TestNextRun_Estimacion verifica las estimaciones de prÃ³xima corrida.
func TestNextRun_Estimacion(t *testing.T) {
	now := time.Date(2026, 9, 14, 10, 0, 0, 0, time.Local) // lunes 10:00
	daily := config.ScheduleConfig{Enabled: true, Mode: "daily", TimeOfDay: "23:00"}
	if got := NextRunForProfile(daily, now); got != "2026-09-14 23:00" {
		t.Errorf("daily futuro deberÃ­a ser hoy, dio %q", got)
	}
	pasado := config.ScheduleConfig{Enabled: true, Mode: "daily", TimeOfDay: "09:00"}
	if got := NextRunForProfile(pasado, now); got != "2026-09-15 09:00" {
		t.Errorf("daily pasado deberÃ­a ser maÃ±ana, dio %q", got)
	}
	weekly := config.ScheduleConfig{Enabled: true, Mode: "weekly", TimeOfDay: "22:00", Weekdays: []string{"mon", "fri"}}
	if got := NextRunForProfile(weekly, now); got != "2026-09-14 22:00" {
		t.Errorf("weekly lunes 22:00 deberÃ­a ser hoy, dio %q", got)
	}
	off := config.ScheduleConfig{Enabled: false, Mode: "daily", TimeOfDay: "23:00"}
	if got := NextRunForProfile(off, now); got != "" {
		t.Errorf("deshabilitado deberÃ­a dar vacÃ­o, dio %q", got)
	}
	if got := NextRunForProfile(config.ScheduleConfig{Enabled: true, Mode: "interval", IntervalMinutes: 60}, now); got != "cada 60 min" {
		t.Errorf("interval deberÃ­a estimarse, dio %q", got)
	}
}


