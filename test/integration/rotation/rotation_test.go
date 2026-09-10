package rotation_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"femucaribe-backup-agent/internal/storage/local"
	"femucaribe-backup-agent/test/testenv"
)

func setupRotationTestDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "backups")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("crear dir: %v", err)
	}

	// Validar seguridad anti-producción
	if err := testenv.ValidateSafety(testenv.TestEnvironmentConfig{
		DatabaseDriver: "sqlite",
		DatabaseName:   "test_rotation",
		ServerInstance: "sqlite_local",
		BackupRoot:     dir,
		TestMode:       true,
	}); err != nil {
		t.Fatalf("seguridad violada: %v", err)
	}

	return dir
}

func createDummyFile(t *testing.T, dir, name string, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("crear archivo %s: %v", p, err)
	}
	return p
}

func TestRotation_EmptyDirectory(t *testing.T) {
	dir := setupRotationTestDir(t)
	b := local.New(dir)

	ctx := context.Background()
	if err := b.Rotate(ctx, 3); err != nil {
		t.Fatalf("Rotate en directorio vacío falló: %v", err)
	}

	latest, err := b.LatestRemote(ctx)
	if err != nil {
		t.Fatalf("LatestRemote falló: %v", err)
	}
	if latest != "" {
		t.Errorf("se esperaba cadena vacía para directorio vacío, obtenido: %q", latest)
	}
}

func TestRotation_SingleFile(t *testing.T) {
	dir := setupRotationTestDir(t)
	b := local.New(dir)

	createDummyFile(t, dir, "CONTABILIDAD_TEST_20260910_1000.bak", "backup1")

	ctx := context.Background()
	if err := b.Rotate(ctx, 3); err != nil {
		t.Fatalf("Rotate falló: %v", err)
	}

	files, _ := filepath.Glob(filepath.Join(dir, "*.bak"))
	if len(files) != 1 {
		t.Errorf("se esperaba 1 archivo conservado, encontrados: %d", len(files))
	}
}

func TestRotation_ThresholdBoundaries(t *testing.T) {
	// Probar N-1, N y N+1
	cases := []struct {
		name       string
		fileCount  int
		keep       int
		expectKept int
	}{
		{name: "N-1 (2 archivos con keep 3)", fileCount: 2, keep: 3, expectKept: 2},
		{name: "N (3 archivos con keep 3)", fileCount: 3, keep: 3, expectKept: 3},
		{name: "N+1 (4 archivos con keep 3)", fileCount: 4, keep: 3, expectKept: 3},
		{name: "N+3 (6 archivos con keep 3)", fileCount: 6, keep: 3, expectKept: 3},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := setupRotationTestDir(t)
			b := local.New(dir)

			for i := 1; i <= tc.fileCount; i++ {
				filename := fmt.Sprintf("CONTABILIDAD_TEST_20260910_%02d00.bak", i)
				createDummyFile(t, dir, filename, fmt.Sprintf("content_%d", i))
			}

			ctx := context.Background()
			if err := b.Rotate(ctx, tc.keep); err != nil {
				t.Fatalf("Rotate falló: %v", err)
			}

			remaining, _ := filepath.Glob(filepath.Join(dir, "*.bak"))
			if len(remaining) != tc.expectKept {
				t.Errorf("esperados %d archivos, quedaron: %d", tc.expectKept, len(remaining))
			}

			// Validar que los eliminados fueron los más antiguos
			if tc.fileCount > tc.keep {
				oldest := filepath.Join(dir, "CONTABILIDAD_TEST_20260910_0100.bak")
				if _, err := os.Stat(oldest); !os.IsNotExist(err) {
					t.Errorf("el archivo más antiguo %s debió haber sido eliminado", oldest)
				}
				newest := fmt.Sprintf("CONTABILIDAD_TEST_20260910_%02d00.bak", tc.fileCount)
				newestPath := filepath.Join(dir, newest)
				if _, err := os.Stat(newestPath); err != nil {
					t.Errorf("el archivo más nuevo %s fue eliminado indebidamente", newestPath)
				}
			}
		})
	}
}

func TestRotation_IgnoresTmpAndCorruptFiles(t *testing.T) {
	dir := setupRotationTestDir(t)
	b := local.New(dir)

	// Archivos válidos
	createDummyFile(t, dir, "CONTABILIDAD_TEST_20260910_1000.bak", "bak1")
	createDummyFile(t, dir, "CONTABILIDAD_TEST_20260910_1100.bak", "bak2")
	createDummyFile(t, dir, "CONTABILIDAD_TEST_20260910_1200.bak", "bak3")
	createDummyFile(t, dir, "CONTABILIDAD_TEST_20260910_1300.bak", "bak4")

	// Archivos no-backup y temporales que NO deben ser contados ni tocados por Rotate
	tmp1 := createDummyFile(t, dir, "CONTABILIDAD_TEST_20260910_1400.bak.tmp", "temp1")
	tmp2 := createDummyFile(t, dir, "orphaned.tmp", "temp2")
	txt := createDummyFile(t, dir, "notes.txt", "texto")
	jsonFile := createDummyFile(t, dir, "state.json", "{}")

	ctx := context.Background()
	if err := b.Rotate(ctx, 3); err != nil {
		t.Fatalf("Rotate falló: %v", err)
	}

	// 1. Debe haber rotado los .bak dejando exactamente 3
	baks, _ := filepath.Glob(filepath.Join(dir, "*.bak"))
	if len(baks) != 3 {
		t.Errorf("se esperaban exactamente 3 .bak, encontrados: %d", len(baks))
	}

	// 2. Los archivos auxiliares y .tmp deben permanecer intactos
	for _, aux := range []string{tmp1, tmp2, txt, jsonFile} {
		if _, err := os.Stat(aux); err != nil {
			t.Errorf("archivo auxiliar o .tmp %s fue alterado o eliminado por Rotate: %v", aux, err)
		}
	}
}

func TestRotation_LatestRemoteAccuracy(t *testing.T) {
	dir := setupRotationTestDir(t)
	b := local.New(dir)

	createDummyFile(t, dir, "CONTABILIDAD_TEST_20260910_0800.bak", "1")
	createDummyFile(t, dir, "CONTABILIDAD_TEST_20260910_1200.bak", "2")
	createDummyFile(t, dir, "CONTABILIDAD_TEST_20260910_0900.bak", "3")
	createDummyFile(t, dir, "CONTABILIDAD_TEST_20260910_1200.bak.tmp", "no contar")

	ctx := context.Background()
	latest, err := b.LatestRemote(ctx)
	if err != nil {
		t.Fatalf("LatestRemote falló: %v", err)
	}

	expected := "CONTABILIDAD_TEST_20260910_1200.bak"
	if latest != expected {
		t.Errorf("LatestRemote incorrecto: esperado=%s, obtenido=%s", expected, latest)
	}
}
