package state

import (
	"os"
	"path/filepath"
	"testing"
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
	if *got != *want {
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
