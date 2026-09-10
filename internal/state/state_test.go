package state

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

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
	if s.LastRunDate != "" || s.LastBackupFile != "" || s.SHA256 != "" {
		t.Errorf("Load inexistente debería devolver State vacío, dio %+v", s)
	}
}

func TestSaveLoad_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	want := &State{
		LastRunDate:    "2026-01-15",
		LastBackupFile: `C:\Backups\CONTABILIDAD_20260115_1200.bak`,
		SHA256:         "abc123",
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
	if s.LastRunDate != "" {
		t.Errorf("Load corrupto debería devolver State vacío, dio %+v", s)
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
	if err := Save(path, &State{LastRunDate: Today()}); err != nil {
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

func TestRanOn(t *testing.T) {
	s := &State{LastRunDate: "2026-01-15"}
	if !s.RanOn("2026-01-15") {
		t.Error("RanOn mismo día debería ser true")
	}
	if s.RanOn("2026-01-16") {
		t.Error("RanOn distinto día debería ser false")
	}
	if (&State{}).RanOn("2026-01-16") {
		t.Error("RanOn con estado vacío debería ser false")
	}
	var nilState *State
	if nilState.RanOn("2026-01-16") {
		t.Error("RanOn con nil debería ser false, no panic")
	}
}

func TestLoad_Fase1Compatibility(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	fase1JSON := `{
  "last_run_date": "2026-01-15",
  "last_backup_file": "C:\\Backups\\CONTABILIDAD_20260115_1200.bak",
  "sha256": "hashfase1"
}
`
	if err := os.WriteFile(path, []byte(fase1JSON), 0o644); err != nil {
		t.Fatal(err)
	}

	st, err := Load(path)
	if err != nil {
		t.Fatalf("Load de state Fase 1 falló: %v", err)
	}

	if st.LastRunDate != "2026-01-15" || st.SHA256 != "hashfase1" {
		t.Errorf("datos base incorrectos: %+v", st)
	}
	if st.PendingSync.R2 {
		t.Errorf("PendingSync.R2 debería ser false por default")
	}
	if st.R2LastSyncedFile != "" {
		t.Errorf("R2LastSyncedFile debería ser vacío por default")
	}
}

func TestSaveLoad_Fase2Fields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	want := &State{
		LastRunDate:    "2026-01-15",
		LastBackupFile: `C:\Backups\CONTABILIDAD_20260115_1200.bak`,
		SHA256:         "abc123",
		PendingSync: PendingSync{
			R2: true,
		},
		R2LastSyncedFile: "CONTABILIDAD_20260114_1200.bak",
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

	// Probar helpers
	got.MarkR2Synced("CONTABILIDAD_20260115_1200.bak")
	if got.PendingSync.R2 {
		t.Errorf("tras MarkR2Synced, PendingSync.R2 debe ser false")
	}
	if got.R2LastSyncedFile != "CONTABILIDAD_20260115_1200.bak" {
		t.Errorf("R2LastSyncedFile no coincide")
	}

	got.SetPendingR2(true)
	if !got.PendingSync.R2 {
		t.Errorf("tras SetPendingR2(true), PendingSync.R2 debe ser true")
	}
}

func TestSaveLoad_Fase3ServerFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	want := &State{
		LastRunDate:    "2026-01-15",
		LastBackupFile: `C:\Backups\CONTABILIDAD_20260115_1200.bak`,
		SHA256:         "abc123",
		PendingSync: PendingSync{
			R2:     false,
			Server: true,
		},
		ServerLastSyncedFile: "CONTABILIDAD_20260114_1200.bak",
	}

	if err := Save(path, want); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("roundtrip Fase 3: quiero %+v, obtuve %+v", want, got)
	}

	// Probar helpers
	got.MarkServerSynced("CONTABILIDAD_20260115_1200.bak")
	if got.PendingSync.Server {
		t.Errorf("tras MarkServerSynced, PendingSync.Server debe ser false")
	}
	if got.ServerLastSyncedFile != "CONTABILIDAD_20260115_1200.bak" {
		t.Errorf("ServerLastSyncedFile no coincide")
	}

	got.SetPendingServer(true)
	if !got.PendingSync.Server {
		t.Errorf("tras SetPendingServer(true), PendingSync.Server debe ser true")
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


