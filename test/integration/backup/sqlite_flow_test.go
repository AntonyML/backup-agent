package backup_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"femucaribe-backup-agent/internal/application"
	"femucaribe-backup-agent/internal/config"
	"femucaribe-backup-agent/internal/hasher"
	"femucaribe-backup-agent/internal/state"
	"femucaribe-backup-agent/internal/storage/local"
	"femucaribe-backup-agent/test/fixtures"
	"femucaribe-backup-agent/test/helpers"
	"femucaribe-backup-agent/test/testdb"
	"femucaribe-backup-agent/test/testenv"
	_ "modernc.org/sqlite"
)

func TestSQLite_EndToEndBackupFlow(t *testing.T) {
	tempDir := t.TempDir()
	backupDir := filepath.Join(tempDir, "backups")
	dbPath := filepath.Join(tempDir, "source.sqlite")

	// 1. Validar seguridad anti-producciÃ³n
	safetyErr := testenv.ValidateSafety(testenv.TestEnvironmentConfig{
		DatabaseDriver: "sqlite",
		DatabaseName:   "test_contabilidad",
		ServerInstance: "sqlite_local",
		BackupRoot:     backupDir,
		TestMode:       true,
	})
	if safetyErr != nil {
		t.Fatalf("violaciÃ³n de seguridad en test: %v", safetyErr)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 2. Crear base origen SQLite y aplicar seed determinÃ­stico
	sourceDB, err := testdb.NewSQLiteFile(ctx, dbPath)
	if err != nil {
		t.Fatalf("error creando base SQLite de prueba: %v", err)
	}
	defer sourceDB.Close()

	if err := sourceDB.Seed(ctx); err != nil {
		t.Fatalf("error en seed de datos: %v", err)
	}

	if err := fixtures.AssertSeedIntegrity(ctx, sourceDB.DB()); err != nil {
		t.Fatalf("error en integridad inicial del seed: %v", err)
	}

	// 3. Inicializar App con SQLiteSQLEngine
	statePath := filepath.Join(tempDir, "state.json")
	lockPath := filepath.Join(tempDir, "agent.lock")

	app := application.New(application.Options{
		Config: config.Config{
			BackupDir:        backupDir,
			Server:           "sqlite_local",
			Database:         "test_contabilidad",
			Retain:           3,
			LoginTimeoutSec:  5,
			BackupTimeoutSec: 10,
		},
		StatePath:    statePath,
		LockPath:     lockPath,
		SQLEngine:    testdb.NewSQLiteSQLEngine(dbPath),
		LocalBackend: local.New(backupDir),
	})

	// 4. Ejecutar backup completo
	if err := app.Backup(ctx, application.BackupOptions{}); err != nil {
		t.Fatalf("error ejecutando backup: %v", err)
	}

	// 5. Verificar que el archivo .bak exista y no haya temporales .tmp
	bakFiles, err := filepath.Glob(filepath.Join(backupDir, "test_contabilidad_*.bak"))
	if err != nil || len(bakFiles) != 1 {
		t.Fatalf("se esperaba exactamente un archivo .bak generado, encontrados: %v (err: %v)", bakFiles, err)
	}
	finalBackupFile := bakFiles[0]

	tmpFiles, err := filepath.Glob(filepath.Join(backupDir, "*.tmp"))
	if err != nil || len(tmpFiles) != 0 {
		t.Fatalf("no deberÃ­an quedar archivos temporales .tmp huÃ©rfanos, encontrados: %v", tmpFiles)
	}

	// 6. Verificar state.json y coincidencia estricta de SHA-256
	st, err := state.Load(statePath)
	if err != nil {
		t.Fatalf("error cargando state.json: %v", err)
	}

	if st.Profile(config.InitialProfileName).LastRunDate != state.Today() {
		t.Errorf("LastRunDate invÃ¡lido: esperada=%s, obtenida=%s", state.Today(), st.Profile(config.InitialProfileName).LastRunDate)
	}
	if filepath.Clean(st.Profile(config.InitialProfileName).LastBackupFile) != filepath.Clean(finalBackupFile) {
		t.Errorf("LastBackupFile invÃ¡lido: esperado=%s, obtenido=%s", finalBackupFile, st.Profile(config.InitialProfileName).LastBackupFile)
	}

	computedSHA, err := hasher.File(finalBackupFile)
	if err != nil {
		t.Fatalf("error calculando hash del .bak: %v", err)
	}
	if st.Profile(config.InitialProfileName).SHA256 != computedSHA {
		t.Errorf("SHA-256 no coincide: en state.json=%s, calculado=%s", st.Profile(config.InitialProfileName).SHA256, computedSHA)
	}

	// 7. Simular restauraciÃ³n: abrir el .bak como base SQLite y validar equivalencia total
	func() {
		restoredDB, err := sql.Open("sqlite", finalBackupFile)
		if err != nil {
			t.Fatalf("error abriendo backup como SQLite: %v", err)
		}
		defer restoredDB.Close()

		if err := fixtures.AssertSeedIntegrity(ctx, restoredDB); err != nil {
			t.Fatalf("base restaurada no cumple con la integridad del seed: %v", err)
		}

		helpers.AssertDatabaseEquivalentWithContext(ctx, t, sourceDB.DB(), restoredDB)
	}()


	// 8. Verificar idempotencia diaria (segunda corrida debe rechazar sin force)
	err = app.Backup(ctx, application.BackupOptions{})
	if !errors.Is(err, application.ErrAlreadyRanToday) {
		t.Fatalf("se esperaba ErrAlreadyRanToday en segunda corrida, obtenido: %v", err)
	}

	// 9. Con Force=true debe permitir una nueva corrida
	err = app.Backup(ctx, application.BackupOptions{Force: true})
	if err != nil {
		t.Fatalf("error en backup con Force=true: %v", err)
	}
}

func TestSQLite_TmpOrphanCleanupOnStartup(t *testing.T) {
	tempDir := t.TempDir()
	backupDir := filepath.Join(tempDir, "backups")
	dbPath := filepath.Join(tempDir, "source.sqlite")

	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatalf("crear backupDir: %v", err)
	}

	// Crear archivo huÃ©rfano .tmp simulando caÃ­da previa
	orphanFile := filepath.Join(backupDir, "interrupted_run.bak.tmp")
	if err := os.WriteFile(orphanFile, []byte("datos incompletos"), 0o644); err != nil {
		t.Fatalf("crear huÃ©rfano: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sourceDB, err := testdb.NewSQLiteFile(ctx, dbPath)
	if err != nil {
		t.Fatalf("crear SQLite source: %v", err)
	}
	defer sourceDB.Close()
	if err := sourceDB.Seed(ctx); err != nil {
		t.Fatalf("seed: %v", err)
	}

	app := application.New(application.Options{
		Config: config.Config{
			BackupDir: backupDir,
			Server:    "sqlite_local",
			Database:  "test_db",
			Retain:    3,
		},
		StatePath:    filepath.Join(tempDir, "state.json"),
		LockPath:     filepath.Join(tempDir, "agent.lock"),
		SQLEngine:    testdb.NewSQLiteSQLEngine(dbPath),
		LocalBackend: local.New(backupDir),
	})

	if err := app.Backup(ctx, application.BackupOptions{}); err != nil {
		t.Fatalf("app.Backup fallÃ³: %v", err)
	}

	// El huÃ©rfano debe haber sido eliminado
	if _, err := os.Stat(orphanFile); !os.IsNotExist(err) {
		t.Errorf("se esperaba que el archivo huÃ©rfano %s fuera eliminado en el arranque", orphanFile)
	}
}

