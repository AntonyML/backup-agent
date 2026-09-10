package main

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"femucaribe-backup-agent/internal/logger"
)

func testLogger(t *testing.T) *logger.Logger {
	t.Helper()
	l := logger.New(t.TempDir())
	l.SetOutput(io.Discard)
	return l
}

func mustFile(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func exists(t *testing.T, dir, name string) bool {
	t.Helper()
	_, err := os.Stat(filepath.Join(dir, name))
	return err == nil
}

// Simula el kill a mitad de proceso: un .tmp huérfano debe descartarse
// en el arranque sin tocar .bak finales ni state.json.
func TestRemoveTmpOrphans_KillRecovery(t *testing.T) {
	dir := t.TempDir()
	mustFile(t, dir, "CONTABILIDAD_20260101_1200.bak")
	mustFile(t, dir, "CONTABILIDAD_20260102_1200.bak.tmp")
	mustFile(t, dir, "state.json")

	n, err := removeTmpOrphans(dir, testLogger(t))
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("debería borrar 1 huérfano, borró %d", n)
	}
	if !exists(t, dir, "CONTABILIDAD_20260101_1200.bak") {
		t.Error("el .bak final no debe tocarse")
	}
	if exists(t, dir, "CONTABILIDAD_20260102_1200.bak.tmp") {
		t.Error("el .tmp huérfano debería haber sido borrado")
	}
	if !exists(t, dir, "state.json") {
		t.Error("state.json no debe tocarse")
	}
}

func TestRemoveTmpOrphans_CleanDir(t *testing.T) {
	dir := t.TempDir()
	mustFile(t, dir, "CONTABILIDAD_20260101_1200.bak")
	n, err := removeTmpOrphans(dir, testLogger(t))
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("sin huérfanos debería borrar 0, borró %d", n)
	}
}

func TestAtomicRename(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a.bak.tmp")
	dst := filepath.Join(dir, "a.bak")
	mustFile(t, dir, "a.bak.tmp")
	if err := atomicRename(src, dst); err != nil {
		t.Fatal(err)
	}
	if exists(t, dir, "a.bak.tmp") || !exists(t, dir, "a.bak") {
		t.Error("rename debería mover src a dst")
	}

	// Con destino existente (doble --force el mismo minuto): reemplaza.
	mustFile(t, dir, "a.bak.tmp")
	if err := atomicRename(src, dst); err != nil {
		t.Fatalf("rename con destino existente: %v", err)
	}
	if exists(t, dir, "a.bak.tmp") || !exists(t, dir, "a.bak") {
		t.Error("rename con destino existente debería reemplazar")
	}
}

func TestExeDir_NonEmpty(t *testing.T) {
	if exeDir() == "" {
		t.Error("exeDir no debería ser vacío")
	}
}

func TestGetR2Client_MissingConfig(t *testing.T) {
	dir := t.TempDir()
	_, err := getR2Client(t.Context(), dir)
	if err == nil {
		t.Error("getR2Client sin config.dat debería fallar")
	}
}
