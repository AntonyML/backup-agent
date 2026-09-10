package server_test

import (
	"context"
	"errors"
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
