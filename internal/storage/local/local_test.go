package local

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalBackend_UploadAndLatest(t *testing.T) {
	tmpDir := t.TempDir()
	srcDir := t.TempDir()
	backend := New(tmpDir)

	if backend.Name() != "local" {
		t.Errorf("esperaba 'local', dio '%s'", backend.Name())
	}

	// Subir archivo desde otra ruta
	srcFile := filepath.Join(srcDir, "CONTABILIDAD_20260101_1000.bak")
	if err := os.WriteFile(srcFile, []byte("backup1"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := backend.Upload(context.Background(), srcFile); err != nil {
		t.Fatalf("Upload falló: %v", err)
	}

	latest, err := backend.LatestRemote(context.Background())
	if err != nil {
		t.Fatalf("LatestRemote falló: %v", err)
	}
	if latest != "CONTABILIDAD_20260101_1000.bak" {
		t.Errorf("esperaba CONTABILIDAD_20260101_1000.bak, dio %s", latest)
	}

	// Subir un segundo archivo más reciente
	srcFile2 := filepath.Join(srcDir, "CONTABILIDAD_20260102_1000.bak")
	if err := os.WriteFile(srcFile2, []byte("backup2"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := backend.Upload(context.Background(), srcFile2); err != nil {
		t.Fatalf("Upload 2 falló: %v", err)
	}

	latest2, err := backend.LatestRemote(context.Background())
	if err != nil {
		t.Fatalf("LatestRemote 2 falló: %v", err)
	}
	if latest2 != "CONTABILIDAD_20260102_1000.bak" {
		t.Errorf("esperaba CONTABILIDAD_20260102_1000.bak, dio %s", latest2)
	}
}

func TestLocalBackend_Rotate(t *testing.T) {
	tmpDir := t.TempDir()
	backend := New(tmpDir)

	// Crear 4 backups
	for _, name := range []string{
		"CONTABILIDAD_20260101_1000.bak",
		"CONTABILIDAD_20260102_1000.bak",
		"CONTABILIDAD_20260103_1000.bak",
		"CONTABILIDAD_20260104_1000.bak",
	} {
		if err := os.WriteFile(filepath.Join(tmpDir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Rotar dejando 2
	if err := backend.Rotate(context.Background(), 2); err != nil {
		t.Fatalf("Rotate falló: %v", err)
	}

	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Errorf("esperaba 2 archivos restantes, dio %d", len(entries))
	}
}
