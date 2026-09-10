package server_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"femucaribe-backup-agent/internal/hasher"
	"femucaribe-backup-agent/internal/storage"
	"femucaribe-backup-agent/internal/storage/server"
)

func TestConfig_Validate(t *testing.T) {
	t.Run("disabled permits empty remote path", func(t *testing.T) {
		cfg := server.DefaultConfig()
		cfg.Enabled = false
		cfg.RemotePath = ""
		if err := cfg.Validate(); err != nil {
			t.Errorf("Validate falló para disabled: %v", err)
		}
	})

	t.Run("enabled requires remote path", func(t *testing.T) {
		cfg := server.DefaultConfig()
		cfg.Enabled = true
		cfg.RemotePath = "   "
		err := cfg.Validate()
		if err == nil || !strings.Contains(err.Error(), "remote_path es obligatorio") {
			t.Errorf("debió rechazar remote_path vacío: %v", err)
		}
	})

	t.Run("enabled validates keep", func(t *testing.T) {
		cfg := server.DefaultConfig()
		cfg.Enabled = true
		cfg.RemotePath = `\\Servidor\Backups`
		cfg.Keep = 0
		err := cfg.Validate()
		if err == nil || !strings.Contains(err.Error(), "keep debe ser >= 1") {
			t.Errorf("debió rechazar keep < 1: %v", err)
		}
	})

	t.Run("enabled validates timeout", func(t *testing.T) {
		cfg := server.DefaultConfig()
		cfg.Enabled = true
		cfg.RemotePath = `\\Servidor\Backups`
		cfg.Keep = 10
		cfg.TimeoutSec = -5
		err := cfg.Validate()
		if err == nil || !strings.Contains(err.Error(), "timeout_sec no puede ser negativo") {
			t.Errorf("debió rechazar timeout_sec negativo: %v", err)
		}
	})

	t.Run("valid configuration passes", func(t *testing.T) {
		cfg := server.Config{
			Enabled:    true,
			RemotePath: `\\Servidor\Backups\CONTABILIDAD`,
			Keep:       10,
			TimeoutSec: 300,
		}
		if err := cfg.Validate(); err != nil {
			t.Errorf("Validate falló para configuración válida: %v", err)
		}
	})
}

func TestBackend_Upload_Success(t *testing.T) {
	tempDir := t.TempDir()
	localDir := filepath.Join(tempDir, "local")
	remoteDir := filepath.Join(tempDir, "remote_server")
	_ = os.MkdirAll(localDir, 0o755)

	localFile := filepath.Join(localDir, "CONTABILIDAD_TEST_20260910_1000.bak")
	content := "DUMMY_BACKUP_PAYLOAD_FOR_SERVER_TEST_123456789"
	if err := os.WriteFile(localFile, []byte(content), 0o644); err != nil {
		t.Fatalf("crear archivo local: %v", err)
	}

	backend := server.New(server.Config{
		Enabled:    true,
		RemotePath: remoteDir,
		Keep:       10,
		TimeoutSec: 10,
	}, nil)

	ctx := context.Background()
	if err := backend.Upload(ctx, localFile); err != nil {
		t.Fatalf("Upload falló: %v", err)
	}

	targetFile := filepath.Join(remoteDir, filepath.Base(localFile))
	tmpFile := targetFile + ".tmp"

	// 1. Debe existir el archivo final
	fi, err := os.Stat(targetFile)
	if err != nil {
		t.Fatalf("archivo final no existe en servidor: %v", err)
	}
	if fi.Size() != int64(len(content)) {
		t.Errorf("tamaño erróneo: esperado=%d, obtenido=%d", len(content), fi.Size())
	}

	// 2. No debe existir el temporal
	if _, err := os.Stat(tmpFile); !os.IsNotExist(err) {
		t.Errorf("archivo temporal .tmp no fue limpiado tras rename exitoso")
	}

	// 3. SHA-256 debe ser idéntico
	localHash, _ := hasher.File(localFile)
	remoteHash, _ := hasher.File(targetFile)
	if localHash != remoteHash {
		t.Errorf("hash SHA-256 no coincide: local=%s, remoto=%s", localHash, remoteHash)
	}
}

func TestBackend_Upload_Timeout(t *testing.T) {
	tempDir := t.TempDir()
	localFile := filepath.Join(tempDir, "test.bak")
	_ = os.WriteFile(localFile, []byte("large payload"), 0o644)

	backend := server.New(server.Config{
		Enabled:    true,
		RemotePath: filepath.Join(tempDir, "server"),
		Keep:       10,
		TimeoutSec: 10,
	}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancelado de antemano

	err := backend.Upload(ctx, localFile)
	if err == nil {
		t.Fatalf("Upload debió fallar con contexto cancelado")
	}

	var retryable *storage.RetryableError
	if !errors.As(err, &retryable) {
		t.Errorf("el error de transferencia cancelada debe clasificarse como RetryableError: %v", err)
	}

	// El temporal debe haber sido limpiado
	tmps, _ := filepath.Glob(filepath.Join(tempDir, "server", "*.tmp"))
	if len(tmps) != 0 {
		t.Errorf("quedaron temporales huérfanos tras timeout: %v", tmps)
	}
}

func TestBackend_Upload_Disabled(t *testing.T) {
	backend := server.New(server.Config{
		Enabled: false,
	}, nil)

	ctx := context.Background()
	if err := backend.Upload(ctx, "nonexistent.bak"); err != nil {
		t.Fatalf("Upload cuando está disabled debe retornar nil, dio: %v", err)
	}
}

func TestBackend_LatestRemote(t *testing.T) {
	tempDir := t.TempDir()
	backend := server.New(server.Config{
		Enabled:    true,
		RemotePath: tempDir,
	}, nil)

	ctx := context.Background()
	latest, err := backend.LatestRemote(ctx)
	if err != nil || latest != "" {
		t.Errorf("esperaba cadena vacía para directorio vacío, dio: %q (err: %v)", latest, err)
	}

	_ = os.WriteFile(filepath.Join(tempDir, "CONTABILIDAD_20260910_0800.bak"), []byte("1"), 0o644)
	_ = os.WriteFile(filepath.Join(tempDir, "CONTABILIDAD_20260910_1200.bak"), []byte("2"), 0o644)
	_ = os.WriteFile(filepath.Join(tempDir, "CONTABILIDAD_20260910_1000.bak"), []byte("3"), 0o644)
	_ = os.WriteFile(filepath.Join(tempDir, "CONTABILIDAD_20260910_1400.bak.tmp"), []byte("tmp"), 0o644)
	_ = os.WriteFile(filepath.Join(tempDir, "notes.txt"), []byte("txt"), 0o644)

	latest, err = backend.LatestRemote(ctx)
	if err != nil {
		t.Fatalf("LatestRemote falló: %v", err)
	}

	expected := "CONTABILIDAD_20260910_1200.bak"
	if latest != expected {
		t.Errorf("LatestRemote erróneo: esperado=%s, obtenido=%s", expected, latest)
	}
}

func TestBackend_Rotate_Boundaries(t *testing.T) {
	ctx := context.Background()

	t.Run("less than 10 backups: no files deleted", func(t *testing.T) {
		tempDir := t.TempDir()
		b := server.New(server.Config{
			Enabled:    true,
			RemotePath: tempDir,
			Keep:       10,
			Database:   "CONTABILIDAD",
		}, nil)

		for i := 1; i <= 5; i++ {
			fn := filepath.Join(tempDir, fmt.Sprintf("CONTABILIDAD_202609%02d_1200.bak", i))
			_ = os.WriteFile(fn, []byte("payload"), 0o644)
		}

		if err := b.Rotate(ctx, 10); err != nil {
			t.Fatalf("Rotate falló: %v", err)
		}

		entries, _ := os.ReadDir(tempDir)
		if len(entries) != 5 {
			t.Errorf("esperaba 5 archivos conservados, hay %d", len(entries))
		}
	})

	t.Run("exactly 10 backups: no files deleted", func(t *testing.T) {
		tempDir := t.TempDir()
		b := server.New(server.Config{
			Enabled:    true,
			RemotePath: tempDir,
			Keep:       10,
			Database:   "CONTABILIDAD",
		}, nil)

		for i := 1; i <= 10; i++ {
			fn := filepath.Join(tempDir, fmt.Sprintf("CONTABILIDAD_202609%02d_1200.bak", i))
			_ = os.WriteFile(fn, []byte("payload"), 0o644)
		}

		if err := b.Rotate(ctx, 10); err != nil {
			t.Fatalf("Rotate falló: %v", err)
		}

		entries, _ := os.ReadDir(tempDir)
		if len(entries) != 10 {
			t.Errorf("esperaba exactamente 10 archivos, hay %d", len(entries))
		}
	})

	t.Run("11 backups: deletes exactly the 1 oldest backup", func(t *testing.T) {
		tempDir := t.TempDir()
		b := server.New(server.Config{
			Enabled:    true,
			RemotePath: tempDir,
			Keep:       10,
			Database:   "CONTABILIDAD",
		}, nil)

		for i := 1; i <= 11; i++ {
			fn := filepath.Join(tempDir, fmt.Sprintf("CONTABILIDAD_202609%02d_1200.bak", i))
			_ = os.WriteFile(fn, []byte("payload"), 0o644)
		}

		if err := b.Rotate(ctx, 10); err != nil {
			t.Fatalf("Rotate falló: %v", err)
		}

		entries, _ := os.ReadDir(tempDir)
		if len(entries) != 10 {
			t.Fatalf("esperaba 10 archivos tras rotación, hay %d", len(entries))
		}

		// CONTABILIDAD_20260901_1200.bak debió ser borrado
		oldest := filepath.Join(tempDir, "CONTABILIDAD_20260901_1200.bak")
		if _, err := os.Stat(oldest); !os.IsNotExist(err) {
			t.Errorf("el backup más viejo (%s) no fue eliminado", oldest)
		}

		// CONTABILIDAD_20260902 ... CONTABILIDAD_20260911 deben existir
		for i := 2; i <= 11; i++ {
			fn := filepath.Join(tempDir, fmt.Sprintf("CONTABILIDAD_202609%02d_1200.bak", i))
			if _, err := os.Stat(fn); err != nil {
				t.Errorf("archivo %s debió conservarse: %v", fn, err)
			}
		}
	})

	t.Run("15 backups: deletes 5 oldest backups", func(t *testing.T) {
		tempDir := t.TempDir()
		b := server.New(server.Config{
			Enabled:    true,
			RemotePath: tempDir,
			Keep:       10,
			Database:   "CONTABILIDAD",
		}, nil)

		for i := 1; i <= 15; i++ {
			fn := filepath.Join(tempDir, fmt.Sprintf("CONTABILIDAD_202609%02d_1200.bak", i))
			_ = os.WriteFile(fn, []byte("payload"), 0o644)
		}

		if err := b.Rotate(ctx, 10); err != nil {
			t.Fatalf("Rotate falló: %v", err)
		}

		entries, _ := os.ReadDir(tempDir)
		if len(entries) != 10 {
			t.Fatalf("esperaba 10 archivos, hay %d", len(entries))
		}

		for i := 1; i <= 5; i++ {
			fn := filepath.Join(tempDir, fmt.Sprintf("CONTABILIDAD_202609%02d_1200.bak", i))
			if _, err := os.Stat(fn); !os.IsNotExist(err) {
				t.Errorf("archivo viejo %s debió eliminarse", fn)
			}
		}
		for i := 6; i <= 15; i++ {
			fn := filepath.Join(tempDir, fmt.Sprintf("CONTABILIDAD_202609%02d_1200.bak", i))
			if _, err := os.Stat(fn); err != nil {
				t.Errorf("archivo %s debió conservarse", fn)
			}
		}
	})
}

func TestBackend_Rotate_IgnoresInvalidFilesAndOtherDatabases(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()

	b := server.New(server.Config{
		Enabled:    true,
		RemotePath: tempDir,
		Keep:       10,
		Database:   "CONTABILIDAD",
	}, nil)

	// 10 backups válidos de CONTABILIDAD
	for i := 1; i <= 10; i++ {
		fn := filepath.Join(tempDir, fmt.Sprintf("CONTABILIDAD_202609%02d_1200.bak", i))
		_ = os.WriteFile(fn, []byte("valid backup"), 0o644)
	}

	// Archivos que NO deben contar ni ser borrados por rotación:
	otherDB := filepath.Join(tempDir, "OTRABASE_20260901_1200.bak")
	tmpFile := filepath.Join(tempDir, "CONTABILIDAD_20260911_1200.bak.tmp")
	corrupt0Byte := filepath.Join(tempDir, "CONTABILIDAD_20260901_1000.bak") // 0 bytes
	badDate := filepath.Join(tempDir, "CONTABILIDAD_invalid_date.bak")
	notes := filepath.Join(tempDir, "notes.txt")

	_ = os.WriteFile(otherDB, []byte("otra base"), 0o644)
	_ = os.WriteFile(tmpFile, []byte("incompleto"), 0o644)
	_ = os.WriteFile(corrupt0Byte, []byte{}, 0o644)
	_ = os.WriteFile(badDate, []byte("bad date"), 0o644)
	_ = os.WriteFile(notes, []byte("notas"), 0o644)

	// Agregamos el backup 11 de CONTABILIDAD
	newBackup := filepath.Join(tempDir, "CONTABILIDAD_20260911_1200.bak")
	_ = os.WriteFile(newBackup, []byte("new valid backup"), 0o644)

	// Rotamos a keep=10
	if err := b.Rotate(ctx, 10); err != nil {
		t.Fatalf("Rotate falló: %v", err)
	}

	// 1. El backup más antiguo válido de CONTABILIDAD (01_1200) debe eliminarse
	oldestValid := filepath.Join(tempDir, "CONTABILIDAD_20260901_1200.bak")
	if _, err := os.Stat(oldestValid); !os.IsNotExist(err) {
		t.Errorf("el backup válido más antiguo debió eliminarse: %s", oldestValid)
	}

	// 2. El backup nuevo (11_1200) debe permanecer intacto
	if _, err := os.Stat(newBackup); err != nil {
		t.Errorf("el backup recién confirmado %s debe existir: %v", newBackup, err)
	}

	// 3. Los archivos ajenos / temporales / corruptos DEBEN seguir intactos (rotación no los borra)
	for _, preserved := range []string{otherDB, tmpFile, corrupt0Byte, badDate, notes} {
		if _, err := os.Stat(preserved); err != nil {
			t.Errorf("archivo ajeno/inválido debió preservarse: %s (err: %v)", preserved, err)
		}
	}
}

func TestBackend_PreventionOfLoss_UploadFailurePreservesOlderBackups(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	serverDir := filepath.Join(tempDir, "remote_server")
	_ = os.MkdirAll(serverDir, 0o755)

	b := server.New(server.Config{
		Enabled:    true,
		RemotePath: serverDir,
		Keep:       10,
		Database:   "CONTABILIDAD",
	}, nil)

	// Backup remoto previo A válido
	backupA := filepath.Join(serverDir, "CONTABILIDAD_20260910_1000.bak")
	contentA := "ORIGINAL_REMOTE_BACKUP_A_DATA"
	_ = os.WriteFile(backupA, []byte(contentA), 0o644)

	// Intentar subir backup B inexistente o cancelado
	cancelCtx, cancel := context.WithCancel(ctx)
	cancel() // Cancelado para provocar fallo

	localB := filepath.Join(tempDir, "CONTABILIDAD_20260910_1100.bak")
	_ = os.WriteFile(localB, []byte("NEW_BACKUP_B_DATA"), 0o644)

	err := b.Upload(cancelCtx, localB)
	if err == nil {
		t.Fatalf("Upload debió fallar por contexto cancelado")
	}

	// Regla crítica de seguridad (Sección 6 y 19):
	// Backup remoto A debe continuar completamente intacto e inalterado
	dataA, readErr := os.ReadFile(backupA)
	if readErr != nil {
		t.Fatalf("backup remoto A desapareció o no puede leerse: %v", readErr)
	}
	if string(dataA) != contentA {
		t.Fatalf("contenido de backup remoto A fue alterado tras fallo de B")
	}

	// B.tmp no debe quedar huérfano
	bTmp := filepath.Join(serverDir, "CONTABILIDAD_20260910_1100.bak.tmp")
	if _, err := os.Stat(bTmp); !os.IsNotExist(err) {
		t.Errorf("archivo temporal B.tmp no fue limpiado tras fallo")
	}
}

func TestBackend_InterruptedUpload_CleansTmpOnNextRun(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	serverDir := filepath.Join(tempDir, "remote_server")
	_ = os.MkdirAll(serverDir, 0o755)

	b := server.New(server.Config{
		Enabled:    true,
		RemotePath: serverDir,
		Keep:       10,
		Database:   "CONTABILIDAD",
	}, nil)

	// Simulamos una interrupción previa que dejó un .tmp en el servidor (Sección 18)
	orphanTmp := filepath.Join(serverDir, "CONTABILIDAD_20260910_1000.bak.tmp")
	_ = os.WriteFile(orphanTmp, []byte("INCOMPLETE_TRANSFER_DATA"), 0o644)

	// Creamos el archivo local válido a respaldar
	localFile := filepath.Join(tempDir, "CONTABILIDAD_20260910_1000.bak")
	validContent := "FULL_VALID_BACKUP_NEW_ATTEMPT_12345"
	_ = os.WriteFile(localFile, []byte(validContent), 0o644)

	// La siguiente ejecución de Upload debe limpiar el .tmp y subir exitosamente
	if err := b.Upload(ctx, localFile); err != nil {
		t.Fatalf("Upload falló en reintento: %v", err)
	}

	// 1. El .tmp ya no debe existir
	if _, err := os.Stat(orphanTmp); !os.IsNotExist(err) {
		t.Errorf(".tmp huérfano debió ser eliminado")
	}

	// 2. El .bak final debe existir con el contenido correcto y verificado
	finalTarget := filepath.Join(serverDir, "CONTABILIDAD_20260910_1000.bak")
	data, err := os.ReadFile(finalTarget)
	if err != nil {
		t.Fatalf("archivo final no existe: %v", err)
	}
	if string(data) != validContent {
		t.Errorf("contenido no coincide: esperado=%s, obtenido=%s", validContent, string(data))
	}
}

