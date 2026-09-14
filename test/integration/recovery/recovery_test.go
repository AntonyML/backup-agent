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

// 1. .tmp huÃ©rfano: se limpian mÃºltiples archivos temporales abandonados al arrancar
func TestRecovery_OrphanTmpCleanup(t *testing.T) {
	_, backupDir, dbPath, statePath, lockPath := setupRecoveryTestEnv(t)
	createSeededSQLiteSource(t, dbPath)

	// Inyectar 3 temporales huÃ©rfanos de ejecuciones previas caÃ­das
	tmp1 := filepath.Join(backupDir, "run_01.bak.tmp")
	tmp2 := filepath.Join(backupDir, "run_02.bak.tmp")
	tmp3 := filepath.Join(backupDir, "orphan.tmp")
	for _, f := range []string{tmp1, tmp2, tmp3} {
		if err := os.WriteFile(f, []byte("incompleto"), 0o644); err != nil {
			t.Fatalf("escribir temporal huÃ©rfano %s: %v", f, err)
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
		t.Fatalf("backup fallÃ³: %v", err)
	}

	// Verificar que ninguno de los .tmp subsiste
	for _, f := range []string{tmp1, tmp2, tmp3} {
		if _, err := os.Stat(f); !os.IsNotExist(err) {
			t.Errorf("temporal huÃ©rfano %s no fue eliminado al arrancar", f)
		}
	}

	// Debe haber quedado exactamente 1 .bak vÃ¡lido
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
		t.Fatalf("se esperaba que el failpoint dejara el archivo .tmp huÃ©rfano en disco")
	}

	// state.json no debe haberse creado
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Errorf("state.json no debiÃ³ crearse tras kill prematuro")
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
		t.Fatalf("segunda corrida tras kill fallÃ³: %v", err)
	}

	// No debe quedar ningÃºn .tmp
	tmpsAfter, _ := filepath.Glob(filepath.Join(backupDir, "*.tmp"))
	if len(tmpsAfter) != 0 {
		t.Errorf("la segunda corrida no limpiÃ³ los .tmp huÃ©rfanos: %v", tmpsAfter)
	}

	// state.json debe ser vÃ¡lido
	st, err := state.Load(statePath)
	if err != nil || st.Profile(config.InitialProfileName).LastBackupFile == "" {
		t.Fatalf("state.json inconsistente tras recuperaciÃ³n: %v", err)
	}
}

// 3. Backup corrupto y VERIFYONLY fallido: detecciÃ³n de bytes alterados y rechazo
func TestRecovery_CorruptBackupDetectionAndRejection(t *testing.T) {
	_, backupDir, dbPath, statePath, lockPath := setupRecoveryTestEnv(t)
	createSeededSQLiteSource(t, dbPath)

	engine := testdb.NewSQLiteSQLEngine(dbPath)

	// Caso A: Verificar que la corrupciÃ³n fÃ­sica en SQLite es detectada por VerifyBackup
	corruptTestPath := filepath.Join(backupDir, "corrupted_target.sqlite")
	srcData, _ := os.ReadFile(dbPath)
	_ = os.WriteFile(corruptTestPath, srcData, 0o644)
	if err := helpers.CorruptFile(corruptTestPath); err != nil {
		t.Fatalf("error corrompiendo archivo de prueba: %v", err)
	}
	ctx := context.Background()
	verifyErr := engine.VerifyBackup(ctx, nil, corruptTestPath)
	if verifyErr == nil {
		t.Fatalf("se esperaba que VerifyBackup rechazara el archivo fÃ­sicamente corrupto")
	}

	// Caso B: Rechazo en el pipeline de la aplicaciÃ³n cuando VerifyBackup falla
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

	// El archivo .tmp corrupto debiÃ³ ser eliminado automÃ¡ticamente
	tmps, _ := filepath.Glob(filepath.Join(backupDir, "*.tmp"))
	if len(tmps) != 0 {
		t.Errorf("el .tmp corrupto no fue eliminado tras fallo de verify: %v", tmps)
	}

	// No debe haberse generado ningÃºn .bak definitivo
	baks, _ := filepath.Glob(filepath.Join(backupDir, "*.bak"))
	if len(baks) != 0 {
		t.Errorf("no debiÃ³ crearse ningÃºn .bak si verify fallÃ³: %v", baks)
	}

	// state.json no debe existir
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Errorf("state.json no debe crearse si verify fallÃ³")
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

// 5. Fallo de estado (JSON corrupto): recuperaciÃ³n tolerante sin crash
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
		t.Fatalf("el agente fallÃ³ ante state.json corrupto: %v", err)
	}

	// state.json debe haber sido recuperado y reescrito limpiamente
	st, err := state.Load(statePath)
	if err != nil {
		t.Fatalf("error cargando state.json reconstruido: %v", err)
	}
	if st.Profile(config.InitialProfileName).LastRunDate != state.Today() || st.Profile(config.InitialProfileName).LastBackupFile == "" || st.Profile(config.InitialProfileName).SHA256 == "" {
		t.Errorf("estado recuperado incompleto: %+v", st)
	}
}

// 6. Invariante crÃ­tica: NUNCA se pierde la copia vÃ¡lida anterior ante cualquier fallo posterior
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

	// 1. Crear copia vÃ¡lida A
	if err := app.Backup(ctx, application.BackupOptions{}); err != nil {
		t.Fatalf("creaciÃ³n de Backup A fallÃ³: %v", err)
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
	if err != nil || stA.Profile(config.InitialProfileName).SHA256 != hashA {
		t.Fatalf("state.json inicial inconsistente")
	}

	assertBackupAIntact := func(stage string) {
		t.Helper()
		fi, err := os.Stat(backupA)
		if err != nil {
			t.Fatalf("[%s] Backup A desapareciÃ³: %v", stage, err)
		}
		if fi.Size() != sizeA {
			t.Errorf("[%s] Backup A alterÃ³ su tamaÃ±o: original=%d, actual=%d", stage, sizeA, fi.Size())
		}
		currentHash, err := hasher.File(backupA)
		if err != nil || currentHash != hashA {
			t.Errorf("[%s] Backup A corrupto: hashA=%s, actual=%s", stage, hashA, currentHash)
		}
		currentSt, err := state.Load(statePath)
		if err != nil || currentSt.Profile(config.InitialProfileName).LastBackupFile != backupA || currentSt.Profile(config.InitialProfileName).SHA256 != hashA {
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
	engine.SetSimulateVerifyError(errors.New("checksum invÃ¡lido en verify"))
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

	// Escenario E: RecuperaciÃ³n final exitosa tras todos los fallos
	// Para simular un nuevo timestamp en la misma ejecuciÃ³n de test:
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
		t.Fatalf("recuperaciÃ³n final fallÃ³: %v", err)
	}

	// Backup A sigue existiendo y es Ã­ntegro
	if _, err := os.Stat(backupA); err != nil {
		t.Fatalf("Backup A fue destruido durante la recuperaciÃ³n: %v", err)
	}
	finalHashA, _ := hasher.File(backupA)
	if finalHashA != hashA {
		t.Errorf("Backup A fue modificado: esperado=%s, obtenido=%s", hashA, finalHashA)
	}

	// No deben quedar temporales huÃ©rfanos
	tmpsFinal, _ := filepath.Glob(filepath.Join(backupDir, "*.tmp"))
	if len(tmpsFinal) != 0 {
		t.Errorf("quedaron temporales huÃ©rfanos tras recuperaciÃ³n: %v", tmpsFinal)
	}

	// state.json ahora apunta al nuevo backup exitoso
	finalSt, err := state.Load(statePath)
	if err != nil || finalSt.Profile(config.InitialProfileName).LastBackupFile == "" {
		t.Fatalf("state.json invÃ¡lido tras recuperaciÃ³n: %v", err)
	}
	if finalSt.Profile(config.InitialProfileName).SHA256 == "" {
		t.Errorf("state.json no contiene SHA-256")
	}
}


