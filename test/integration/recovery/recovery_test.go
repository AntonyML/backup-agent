package recovery_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"femucaribe-backup-agent/internal/application"
	"femucaribe-backup-agent/internal/config"
	"femucaribe-backup-agent/internal/hasher"
	"femucaribe-backup-agent/internal/state"
	"femucaribe-backup-agent/internal/storage/local"
	"femucaribe-backup-agent/test/helpers"
	"femucaribe-backup-agent/test/testdb"
	"femucaribe-backup-agent/test/testenv"
	_ "modernc.org/sqlite"
)

func setupRecoveryTestEnv(t *testing.T) (string, string, string, string, string) {
	t.Helper()
	tempDir := t.TempDir()
	backupDir := filepath.Join(tempDir, "backups")
	dbPath := filepath.Join(tempDir, "source.sqlite")
	statePath := filepath.Join(tempDir, "state.json")
	lockPath := filepath.Join(tempDir, "agent.lock")

	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatalf("crear backupDir: %v", err)
	}

	safetyErr := testenv.ValidateSafety(testenv.TestEnvironmentConfig{
		DatabaseDriver: "sqlite",
		DatabaseName:   "test_recovery",
		ServerInstance: "sqlite_local",
		BackupRoot:     backupDir,
		TestMode:       true,
	})
	if safetyErr != nil {
		t.Fatalf("seguridad violada en test: %v", safetyErr)
	}

	return tempDir, backupDir, dbPath, statePath, lockPath
}

func createSeededSQLiteSource(t *testing.T, dbPath string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sdb, err := testdb.NewSQLiteFile(ctx, dbPath)
	if err != nil {
		t.Fatalf("crear SQLite: %v", err)
	}
	defer sdb.Close()

	if err := sdb.Seed(ctx); err != nil {
		t.Fatalf("seed SQLite: %v", err)
	}
}

// 1. .tmp huérfano: se limpian múltiples archivos temporales abandonados al arrancar
func TestRecovery_OrphanTmpCleanup(t *testing.T) {
	_, backupDir, dbPath, statePath, lockPath := setupRecoveryTestEnv(t)
	createSeededSQLiteSource(t, dbPath)

	// Inyectar 3 temporales huérfanos de ejecuciones previas caídas
	tmp1 := filepath.Join(backupDir, "run_01.bak.tmp")
	tmp2 := filepath.Join(backupDir, "run_02.bak.tmp")
	tmp3 := filepath.Join(backupDir, "orphan.tmp")
	for _, f := range []string{tmp1, tmp2, tmp3} {
		if err := os.WriteFile(f, []byte("incompleto"), 0o644); err != nil {
			t.Fatalf("escribir temporal huérfano %s: %v", f, err)
		}
	}

	engine := testdb.NewSQLiteSQLEngine(dbPath)
	app := application.New(application.Options{
		Config: config.Config{
			BackupDir: backupDir,
			Server:    "sqlite_local",
			Database:  "test_recovery",
			Retain:    3,
		},
		StatePath:    statePath,
		LockPath:     lockPath,
		SQLEngine:    engine,
		LocalBackend: local.New(backupDir),
	})

	ctx := context.Background()
	if err := app.Backup(ctx, application.BackupOptions{}); err != nil {
		t.Fatalf("backup falló: %v", err)
	}

	// Verificar que ninguno de los .tmp subsiste
	for _, f := range []string{tmp1, tmp2, tmp3} {
		if _, err := os.Stat(f); !os.IsNotExist(err) {
			t.Errorf("temporal huérfano %s no fue eliminado al arrancar", f)
		}
	}

	// Debe haber quedado exactamente 1 .bak válido
	baks, err := filepath.Glob(filepath.Join(backupDir, "*.bak"))
	if err != nil || len(baks) != 1 {
		t.Fatalf("se esperaba exactamente 1 .bak final, encontrados: %v", baks)
	}
}

// 2. Kill controlado mediante failpoint: deja .tmp, luego la corrida posterior se recupera
func TestRecovery_ControlledKillWithFailpoint(t *testing.T) {
	_, backupDir, dbPath, statePath, lockPath := setupRecoveryTestEnv(t)
	createSeededSQLiteSource(t, dbPath)

	engine := testdb.NewSQLiteSQLEngine(dbPath)
	killedApp := application.New(application.Options{
		Config: config.Config{
			BackupDir: backupDir,
			Server:    "sqlite_local",
			Database:  "test_recovery",
			Retain:    3,
		},
		StatePath: statePath,
		LockPath:  lockPath,
		SQLEngine: engine,
		Failpoint: func(point string) error {
			if point == "after_backup_started" {
				return errors.New("simulated SIGKILL after backup write")
			}
			return nil
		},
		LocalBackend: local.New(backupDir),
	})

	ctx := context.Background()
	err := killedApp.Backup(ctx, application.BackupOptions{})
	if err == nil || !strings.Contains(err.Error(), "simulated SIGKILL") {
		t.Fatalf("se esperaba error de failpoint SIGKILL, obtenido: %v", err)
	}

	// Debe haber quedado un archivo .bak.tmp abandonado
	tmps, _ := filepath.Glob(filepath.Join(backupDir, "*.tmp"))
	if len(tmps) == 0 {
		t.Fatalf("se esperaba que el failpoint dejara el archivo .tmp huérfano en disco")
	}

	// state.json no debe haberse creado
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Errorf("state.json no debió crearse tras kill prematuro")
	}

	// Segunda corrida limpia y recupera
	normalApp := application.New(application.Options{
		Config: config.Config{
			BackupDir: backupDir,
			Server:    "sqlite_local",
			Database:  "test_recovery",
			Retain:    3,
		},
		StatePath:    statePath,
		LockPath:     lockPath,
		SQLEngine:    engine,
		LocalBackend: local.New(backupDir),
	})

	if err := normalApp.Backup(ctx, application.BackupOptions{Force: true}); err != nil {
		t.Fatalf("segunda corrida tras kill falló: %v", err)
	}

	// No debe quedar ningún .tmp
	tmpsAfter, _ := filepath.Glob(filepath.Join(backupDir, "*.tmp"))
	if len(tmpsAfter) != 0 {
		t.Errorf("la segunda corrida no limpió los .tmp huérfanos: %v", tmpsAfter)
	}

	// state.json debe ser válido
	st, err := state.Load(statePath)
	if err != nil || st.LastBackupFile == "" {
		t.Fatalf("state.json inconsistente tras recuperación: %v", err)
	}
}

// 3. Backup corrupto y VERIFYONLY fallido: detección de bytes alterados y rechazo
func TestRecovery_CorruptBackupDetectionAndRejection(t *testing.T) {
	_, backupDir, dbPath, statePath, lockPath := setupRecoveryTestEnv(t)
	createSeededSQLiteSource(t, dbPath)

	engine := testdb.NewSQLiteSQLEngine(dbPath)

	// Caso A: Verificar que la corrupción física en SQLite es detectada por VerifyBackup
	corruptTestPath := filepath.Join(backupDir, "corrupted_target.sqlite")
	srcData, _ := os.ReadFile(dbPath)
	_ = os.WriteFile(corruptTestPath, srcData, 0o644)
	if err := helpers.CorruptFile(corruptTestPath); err != nil {
		t.Fatalf("error corrompiendo archivo de prueba: %v", err)
	}
	ctx := context.Background()
	verifyErr := engine.VerifyBackup(ctx, nil, corruptTestPath)
	if verifyErr == nil {
		t.Fatalf("se esperaba que VerifyBackup rechazara el archivo físicamente corrupto")
	}

	// Caso B: Rechazo en el pipeline de la aplicación cuando VerifyBackup falla
	engine.SetSimulateVerifyError(errors.New("RESTORE VERIFYONLY: media set checksum error"))
	app := application.New(application.Options{
		Config: config.Config{
			BackupDir: backupDir,
			Server:    "sqlite_local",
			Database:  "test_recovery",
			Retain:    3,
		},
		StatePath:    statePath,
		LockPath:     lockPath,
		SQLEngine:    engine,
		LocalBackend: local.New(backupDir),
	})

	err := app.Backup(ctx, application.BackupOptions{})
	if err == nil || !strings.Contains(err.Error(), "verify backup") {
		t.Fatalf("se esperaba fallo de verify backup en pipeline, obtenido: %v", err)
	}

	// El archivo .tmp corrupto debió ser eliminado automáticamente
	tmps, _ := filepath.Glob(filepath.Join(backupDir, "*.tmp"))
	if len(tmps) != 0 {
		t.Errorf("el .tmp corrupto no fue eliminado tras fallo de verify: %v", tmps)
	}

	// No debe haberse generado ningún .bak definitivo
	baks, _ := filepath.Glob(filepath.Join(backupDir, "*.bak"))
	if len(baks) != 0 {
		t.Errorf("no debió crearse ningún .bak si verify falló: %v", baks)
	}

	// state.json no debe existir
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Errorf("state.json no debe crearse si verify falló")
	}
}

// 4. Fallo de disco simulado: aborta antes de ejecutar BACKUP sin alterar disco ni estado
func TestRecovery_SimulatedDiskFull(t *testing.T) {
	_, backupDir, dbPath, statePath, lockPath := setupRecoveryTestEnv(t)
	createSeededSQLiteSource(t, dbPath)

	engine := testdb.NewSQLiteSQLEngine(dbPath)
	engine.SetSimulateDiskFull(true)

	app := application.New(application.Options{
		Config: config.Config{
			BackupDir: backupDir,
			Server:    "sqlite_local",
			Database:  "test_recovery",
			Retain:    3,
		},
		StatePath:    statePath,
		LockPath:     lockPath,
		SQLEngine:    engine,
		LocalBackend: local.New(backupDir),
	})

	ctx := context.Background()
	err := app.Backup(ctx, application.BackupOptions{})
	if err == nil || !strings.Contains(err.Error(), "espacio en disco") {
		t.Fatalf("se esperaba error de espacio en disco, obtenido: %v", err)
	}

	// No debe haber quedado absolutamente nada en backupDir
	entries, _ := os.ReadDir(backupDir)
	if len(entries) != 0 {
		t.Errorf("no debieron crearse archivos ante falta de espacio, encontrados: %d", len(entries))
	}
}

// 5. Fallo de estado (JSON corrupto): recuperación tolerante sin crash
func TestRecovery_CorruptedStateRecovery(t *testing.T) {
	_, backupDir, dbPath, statePath, lockPath := setupRecoveryTestEnv(t)
	createSeededSQLiteSource(t, dbPath)

	// Escribir state.json truncado o corrupto
	corruptedJSON := []byte(`{"last_run_date": "2026-09-10", "last_backup_file": `)
	if err := os.WriteFile(statePath, corruptedJSON, 0o644); err != nil {
		t.Fatalf("escribir state.json corrupto: %v", err)
	}

	engine := testdb.NewSQLiteSQLEngine(dbPath)
	app := application.New(application.Options{
		Config: config.Config{
			BackupDir: backupDir,
			Server:    "sqlite_local",
			Database:  "test_recovery",
			Retain:    3,
		},
		StatePath:    statePath,
		LockPath:     lockPath,
		SQLEngine:    engine,
		LocalBackend: local.New(backupDir),
	})

	ctx := context.Background()
	if err := app.Backup(ctx, application.BackupOptions{}); err != nil {
		t.Fatalf("el agente falló ante state.json corrupto: %v", err)
	}

	// state.json debe haber sido recuperado y reescrito limpiamente
	st, err := state.Load(statePath)
	if err != nil {
		t.Fatalf("error cargando state.json reconstruido: %v", err)
	}
	if st.LastRunDate != state.Today() || st.LastBackupFile == "" || st.SHA256 == "" {
		t.Errorf("estado recuperado incompleto: %+v", st)
	}
}

// 6. Invariante crítica: NUNCA se pierde la copia válida anterior ante cualquier fallo posterior
func TestRecovery_NeverLosePreviousValidBackupOnFailure(t *testing.T) {
	_, backupDir, dbPath, statePath, lockPath := setupRecoveryTestEnv(t)
	createSeededSQLiteSource(t, dbPath)

	ctx := context.Background()
	engine := testdb.NewSQLiteSQLEngine(dbPath)
	app := application.New(application.Options{
		Config: config.Config{
			BackupDir: backupDir,
			Server:    "sqlite_local",
			Database:  "test_recovery",
			Retain:    3,
		},
		StatePath:    statePath,
		LockPath:     lockPath,
		SQLEngine:    engine,
		LocalBackend: local.New(backupDir),
	})

	// 1. Crear copia válida A
	if err := app.Backup(ctx, application.BackupOptions{}); err != nil {
		t.Fatalf("creación de Backup A falló: %v", err)
	}

	baks, err := filepath.Glob(filepath.Join(backupDir, "*.bak"))
	if err != nil || len(baks) != 1 {
		t.Fatalf("se esperaba 1 backup inicial, obtenidos: %v", baks)
	}
	backupA := baks[0]
	hashA, err := hasher.File(backupA)
	if err != nil {
		t.Fatalf("calcular hash Backup A: %v", err)
	}
	fiA, _ := os.Stat(backupA)
	sizeA := fiA.Size()

	stA, err := state.Load(statePath)
	if err != nil || stA.SHA256 != hashA {
		t.Fatalf("state.json inicial inconsistente")
	}

	assertBackupAIntact := func(stage string) {
		t.Helper()
		fi, err := os.Stat(backupA)
		if err != nil {
			t.Fatalf("[%s] Backup A desapareció: %v", stage, err)
		}
		if fi.Size() != sizeA {
			t.Errorf("[%s] Backup A alteró su tamaño: original=%d, actual=%d", stage, sizeA, fi.Size())
		}
		currentHash, err := hasher.File(backupA)
		if err != nil || currentHash != hashA {
			t.Errorf("[%s] Backup A corrupto: hashA=%s, actual=%s", stage, hashA, currentHash)
		}
		currentSt, err := state.Load(statePath)
		if err != nil || currentSt.LastBackupFile != backupA || currentSt.SHA256 != hashA {
			t.Errorf("[%s] state.json fue sobreescrito indebidamente: %+v", stage, currentSt)
		}
	}

	// Escenario A: Falla por falta de espacio en disco en intento B
	engine.SetSimulateDiskFull(true)
	_ = app.Backup(ctx, application.BackupOptions{Force: true})
	assertBackupAIntact("Fallo de disco")

	// Escenario B: Falla durante BackupDatabase en intento B
	engine.SetSimulateDiskFull(false)
	engine.SetSimulateBackupError(errors.New("I/O error simulado en motor"))
	_ = app.Backup(ctx, application.BackupOptions{Force: true})
	assertBackupAIntact("Fallo de BackupDatabase")

	// Escenario C: Falla durante VerifyBackup en intento B
	engine.SetSimulateBackupError(nil)
	engine.SetSimulateVerifyError(errors.New("checksum inválido en verify"))
	_ = app.Backup(ctx, application.BackupOptions{Force: true})
	assertBackupAIntact("Fallo de VerifyBackup")

	// Escenario D: Kill abrupto por failpoint en intento B
	engine.SetSimulateVerifyError(nil)
	killApp := application.New(application.Options{
		Config: config.Config{
			BackupDir: backupDir,
			Server:    "sqlite_local",
			Database:  "test_recovery",
			Retain:    3,
		},
		StatePath: statePath,
		LockPath:  lockPath,
		SQLEngine: engine,
		Failpoint: func(point string) error {
			if point == "after_backup_started" {
				return errors.New("SIGKILL simulado")
			}
			return nil
		},
		LocalBackend: local.New(backupDir),
	})
	_ = killApp.Backup(ctx, application.BackupOptions{Force: true})
	assertBackupAIntact("Kill abrupto")

	// Escenario E: Recuperación final exitosa tras todos los fallos
	// Para simular un nuevo timestamp en la misma ejecución de test:
	// El nuevo backup debe crearse limpiando el .tmp, y conservando A
	recoverApp := application.New(application.Options{
		Config: config.Config{
			BackupDir: backupDir,
			Server:    "sqlite_local",
			Database:  "test_recovery",
			Retain:    3,
		},
		StatePath:    statePath,
		LockPath:     lockPath,
		SQLEngine:    engine,
		LocalBackend: local.New(backupDir),
	})

	// Forzamos la corrida
	if err := recoverApp.Backup(ctx, application.BackupOptions{Force: true}); err != nil {
		t.Fatalf("recuperación final falló: %v", err)
	}

	// Backup A sigue existiendo y es íntegro
	if _, err := os.Stat(backupA); err != nil {
		t.Fatalf("Backup A fue destruido durante la recuperación: %v", err)
	}
	finalHashA, _ := hasher.File(backupA)
	if finalHashA != hashA {
		t.Errorf("Backup A fue modificado: esperado=%s, obtenido=%s", hashA, finalHashA)
	}

	// No deben quedar temporales huérfanos
	tmpsFinal, _ := filepath.Glob(filepath.Join(backupDir, "*.tmp"))
	if len(tmpsFinal) != 0 {
		t.Errorf("quedaron temporales huérfanos tras recuperación: %v", tmpsFinal)
	}

	// state.json ahora apunta al nuevo backup exitoso
	finalSt, err := state.Load(statePath)
	if err != nil || finalSt.LastBackupFile == "" {
		t.Fatalf("state.json inválido tras recuperación: %v", err)
	}
	if finalSt.SHA256 == "" {
		t.Errorf("state.json no contiene SHA-256")
	}
}
