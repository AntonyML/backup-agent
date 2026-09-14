package state

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"femucaribe-backup-agent/internal/config"
	"femucaribe-backup-agent/internal/events"
)

func TestLoad_MissingFile_ReturnsEmptyWithoutError(t *testing.T) {
	s, err := Load(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Load inexistente no debería dar error: %v", err)
	}
	if s == nil {
		t.Fatal("Load debería devolver State no-nil")
	}
	if p, ok := s.ProfileIfExists(config.InitialProfileName); ok && (p.LastRunDate != "" || p.LastBackupFile != "") {
		t.Errorf("Load inexistente debería devolver State vacío, dio %+v", p)
	}
}

func TestSaveLoad_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	want := &State{
		Profiles: map[string]*ProfileState{
			config.InitialProfileName: {
				LastRunDate:    "2026-01-15",
				LastBackupFile: `C:\Backups\CONTABILIDAD_20260115_1200.bak`,
				SHA256:         "abc123",
			},
		},
	}
	if err := Save(path, want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("round-trip: quiero %+v, obtuve %+v", want, got)
	}
}

func TestLoad_CorruptFile_ReturnsEmptyWithError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte("{json roto,,,"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Load(path)
	if err == nil {
		t.Error("Load corrupto debería devolver error (sin crashear)")
	}
	if s == nil {
		t.Fatal("Load corrupto debería devolver State vacío no-nil")
	}
	if p, _ := s.ProfileIfExists(config.InitialProfileName); p != nil && p.LastRunDate != "" {
		t.Errorf("Load corrupto debería devolver State vacío, dio %+v", p)
	}
}

func TestLoad_EmptyFile_IsCorrupt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Error("Load con archivo vacío debería devolver error")
	}
}

func TestSave_CreatesParentDirs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "dir", "state.json")
	s := &State{}
	s.Profile(config.InitialProfileName).LastRunDate = Today()
	if err := Save(path, s); err != nil {
		t.Fatalf("Save debería crear directorios padres: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("state.json debería existir: %v", err)
	}
}

func TestSave_NilState_Errors(t *testing.T) {
	if err := Save(filepath.Join(t.TempDir(), "state.json"), nil); err == nil {
		t.Error("Save(nil) debería devolver error")
	}
}

func TestProfileState_RanOn(t *testing.T) {
	p := &ProfileState{LastRunDate: "2026-01-15"}
	if !p.RanOn("2026-01-15") {
		t.Error("RanOn mismo día debería ser true")
	}
	if p.RanOn("2026-01-16") {
		t.Error("RanOn distinto día debería ser false")
	}
	var nilP *ProfileState
	if nilP.RanOn("2026-01-16") {
		t.Error("RanOn con nil debería ser false, no panic")
	}
}

// TestLoad_Fase1Compatibility verifica la migración del esquema viejo
// (top-level) al namespacing por perfil (D3/D11).
func TestLoad_Fase1Compatibility(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	fase1JSON := "{\n" +
		"  \"last_run_date\": \"2026-01-15\",\n" +
		"  \"last_backup_file\": \"C:/Backups/CONTABILIDAD_20260115_1200.bak\",\n" +
		"  \"sha256\": \"hashfase1\",\n" +
		"  \"pending_sync\": {\"r2\": true, \"server\": true},\n" +
		"  \"r2_last_synced_file\": \"CONTABILIDAD_20260114_1200.bak\",\n" +
		"  \"server_last_synced_file\": \"CONTABILIDAD_20260113_1200.bak\"\n" +
		"}\n"
	if err := os.WriteFile(path, []byte(fase1JSON), 0o644); err != nil {
		t.Fatal(err)
	}

	st, err := Load(path)
	if err != nil {
		t.Fatalf("Load de state Fase 1 falló: %v", err)
	}

	p, ok := st.ProfileIfExists(config.InitialProfileName)
	if !ok {
		t.Fatal("la migración debería crear la sección del perfil inicial")
	}
	if p.LastRunDate != "2026-01-15" || p.SHA256 != "hashfase1" {
		t.Errorf("datos base incorrectos: %+v", p)
	}
	if !p.IsPending(config.PlatformCloudflare) || !p.IsPending(config.PlatformServer) {
		t.Errorf("PendingSync legacy r2/server debería mapear a cloudflare/remote_server: %+v", p.PendingSync)
	}
	if p.LastSyncedFiles[config.PlatformCloudflare] != "CONTABILIDAD_20260114_1200.bak" {
		t.Errorf("r2_last_synced_file debería migrar a cloudflare: %+v", p.LastSyncedFiles)
	}
	if p.LastSyncedFiles[config.PlatformServer] != "CONTABILIDAD_20260113_1200.bak" {
		t.Errorf("server_last_synced_file debería migrar a remote_server: %+v", p.LastSyncedFiles)
	}

	// Re-guardar y re-cargar: debe ser idempotente
	if err := Save(path, st); err != nil {
		t.Fatalf("Save tras migración: %v", err)
	}
	st2, err := Load(path)
	if err != nil {
		t.Fatalf("re-Load: %v", err)
	}
	p2, _ := st2.ProfileIfExists(config.InitialProfileName)
	if !reflect.DeepEqual(p, p2) {
		t.Errorf("migración idempotente falló: quiero %+v, obtuve %+v", p, p2)
	}
}

// TestSaveLoad_Fase2Fields verifica el esquema nuevo con pending por plataforma (D4).
func TestSaveLoad_Fase2Fields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	want := &State{
		Profiles: map[string]*ProfileState{
			config.InitialProfileName: {
				LastRunDate:    "2026-01-15",
				LastBackupFile: `C:\Backups\CONTABILIDAD_20260115_1200.bak`,
				SHA256:         "abc123",
				PendingSync:    map[string]bool{config.PlatformCloudflare: true},
				LastSyncedFiles: map[string]string{
					config.PlatformCloudflare: "CONTABILIDAD_20260114_1200.bak",
				},
			},
		},
	}

	if err := Save(path, want); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("roundtrip Fase 2: quiero %+v, obtuve %+v", want, got)
	}

	p := got.Profile(config.InitialProfileName)
	p.MarkSynced(config.PlatformCloudflare, "CONTABILIDAD_20260115_1200.bak")
	if p.IsPending(config.PlatformCloudflare) {
		t.Errorf("tras MarkSynced, cloudflare no debe estar pendiente")
	}
	if p.LastSyncedFiles[config.PlatformCloudflare] != "CONTABILIDAD_20260115_1200.bak" {
		t.Errorf("LastSyncedFiles[cloudflare] no coincide")
	}

	p.SetPending(config.PlatformServer, true)
	if !p.IsPending(config.PlatformServer) {
		t.Errorf("tras SetPending(remote_server,true) debe estar pendiente")
	}
	if !p.HasPending() {
		t.Error("HasPending debería ser true con remote_server pendiente")
	}
}

// TestState_PerfilNamespacing verifica que dos perfiles no se pisan (D3).
func TestState_PerfilNamespacing(t *testing.T) {
	s := &State{}
	a := s.Profile("noche")
	b := s.Profile("dia")
	a.LastRunDate = "2026-01-15"
	b.LastRunDate = "2026-01-14"
	if a.LastRunDate != "2026-01-15" || b.LastRunDate != "2026-01-14" {
		t.Errorf("las secciones de perfil deben ser independientes: %+v %+v", a, b)
	}
	if !s.RanOn("noche", "2026-01-15") || s.RanOn("dia", "2026-01-15") {
		t.Error("State.RanOn debería delegar en la sección del perfil")
	}
}

func TestState_PendingEvents(t *testing.T) {
	st := &State{}
	evt1 := events.Event{EventID: "evt_1", EventType: "test"}
	evt2 := events.Event{EventID: "evt_2", EventType: "test"}

	st.AddPendingEvent(evt1)
	st.AddPendingEvent(evt2)
	st.AddPendingEvent(evt1) // Duplicado no debe agregarse

	if len(st.PendingEvents) != 2 {
		t.Fatalf("esperaba 2 eventos pendientes, tengo %d", len(st.PendingEvents))
	}

	st.ClearPendingEvents()
	if len(st.PendingEvents) != 0 {
		t.Errorf("ClearPendingEvents debería vaciar el slice, tengo %d", len(st.PendingEvents))
	}
}
