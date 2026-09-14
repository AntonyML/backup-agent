package storage_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"femucaribe-backup-agent/internal/application"
	"femucaribe-backup-agent/internal/config"
	"femucaribe-backup-agent/internal/hasher"
	"femucaribe-backup-agent/internal/state"
	"femucaribe-backup-agent/internal/storage"
	"femucaribe-backup-agent/internal/storage/local"
	"femucaribe-backup-agent/internal/storage/server"
	"femucaribe-backup-agent/test/testdb"
	"femucaribe-backup-agent/test/testenv"
	_ "modernc.org/sqlite"
)

func setupServerIntegrationEnv(t *testing.T) (string, string, string, string, string, string) {
	t.Helper()
	tempDir := t.TempDir()
	backupDir := filepath.Join(tempDir, "local_backups")
	serverDir := filepath.Join(tempDir, "remote_server_share")
	dbPath := filepath.Join(tempDir, "CONTABILIDAD_TEST.sqlite")
	statePath := filepath.Join(tempDir, "state.json")
	lockPath := filepath.Join(tempDir, "agent.lock")

	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatalf("crear backupDir: %v", err)
	}
	if err := os.MkdirAll(serverDir, 0o755); err != nil {
		t.Fatalf("crear serverDir: %v", err)
	}

	if err := testenv.ValidateSafety(testenv.TestEnvironmentConfig{
		DatabaseDriver: "sqlite",
		DatabaseName:   "CONTABILIDAD_TEST",
		ServerInstance: "sqlite_local",
		BackupRoot:     backupDir,
		TestMode:       true,
	}); err != nil {
		t.Fatalf("seguridad violada: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	sdb, err := testdb.NewSQLiteFile(ctx, dbPath)
	if err != nil {
		t.Fatalf("crear sqlite: %v", err)
	}
	defer sdb.Close()
	if err := sdb.Seed(ctx); err != nil {
		t.Fatalf("seed: %v", err)
	}

	return tempDir, backupDir, serverDir, dbPath, statePath, lockPath
}

// 1. Pipeline real: Local -> Copia remota a Servidor -> VerificaciÃ³n integridad -> RotaciÃ³n a 10
func TestServerIntegration_RealPipeline_UploadVerifyRotate10(t *testing.T) {
	_, backupDir, serverDir, dbPath, statePath, lockPath := setupServerIntegrationEnv(t)

	// Sembrar 10 copias vÃ¡lidas previas en el servidor remoto para probar la frontera exacta
	for i := 1; i <= 10; i++ {
		name := fmt.Sprintf("CONTABILIDAD_TEST_202609%02d_1200.bak", i)
		p := filepath.Join(serverDir, name)
		if err := os.WriteFile(p, []byte(fmt.Sprintf("PREVIOUS_VALID_BACKUP_CONTENT_%02d", i)), 0o644); err != nil {
			t.Fatalf("sembrar backup previo %s: %v", name, err)
		}
	}

	engine := testdb.NewSQLiteSQLEngine(dbPath)
	serverBackend := server.New(server.Config{
		Enabled:    true,
		RemotePath: serverDir,
		Keep:       10,
		Database:   "CONTABILIDAD_TEST",
		TimeoutSec: 30,
	}, nil)

	app := application.New(application.Options{
		Config: config.Config{
			BackupDir: backupDir,
			Server:    "sqlite_local",
			Database:  "CONTABILIDAD_TEST",
			Retain:    3,
		},
		StatePath:    statePath,
		LockPath:     lockPath,
		SQLEngine:    engine,
		LocalBackend: local.New(backupDir),
		Backends:     []storage.Backend{serverBackend},
	})

	ctx := context.Background()
	err := app.Backup(ctx, application.BackupOptions{})
	if err != nil {
		t.Fatalf("Backup fallÃ³: %v", err)
	}

	// 1. Verificar backup local
	st, err := state.Load(statePath)
	if err != nil {
		t.Fatalf("cargar state: %v", err)
	}
	if st.Profile(config.InitialProfileName).LastBackupFile == "" {
		t.Fatalf("LastBackupFile vacÃ­o en state")
	}
	localHash, err := hasher.File(st.Profile(config.InitialProfileName).LastBackupFile)
	if err != nil {
		t.Fatalf("calcular hash local: %v", err)
	}
	if st.Profile(config.InitialProfileName).SHA256 != localHash {
		t.Errorf("SHA256 en state (%s) != localHash (%s)", st.Profile(config.InitialProfileName).SHA256, localHash)
	}

	// 2. Verificar archivo en servidor remoto
	remoteBackupFile := filepath.Join(serverDir, filepath.Base(st.Profile(config.InitialProfileName).LastBackupFile))
	remoteHash, err := hasher.File(remoteBackupFile)
	if err != nil {
		t.Fatalf("calcular hash remoto: %v", err)
	}
	if remoteHash != localHash {
		t.Errorf("integridad remota violada: remoteHash (%s) != localHash (%s)", remoteHash, localHash)
	}

	// 3. Verificar que no queden temporales huÃ©rfanos
	tmps, _ := filepath.Glob(filepath.Join(serverDir, "*.tmp"))
	if len(tmps) != 0 {
		t.Errorf("quedaron temporales en servidor: %v", tmps)
	}

	// 4. Verificar rotaciÃ³n: deben quedar exactamente 10 copias
	entries, err := os.ReadDir(serverDir)
	if err != nil {
		t.Fatalf("leer serverDir: %v", err)
	}
	if len(entries) != 10 {
		t.Fatalf("esperaba exactamente 10 copias en servidor remoto tras rotaciÃ³n, hay %d", len(entries))
	}

	// La copia mÃ¡s vieja (20260901_1200.bak) debiÃ³ ser eliminada
	oldestFile := filepath.Join(serverDir, "CONTABILIDAD_TEST_20260901_1200.bak")
	if _, err := os.Stat(oldestFile); !os.IsNotExist(err) {
		t.Errorf("el backup mÃ¡s viejo (%s) debiÃ³ eliminarse", oldestFile)
	}

	// La nueva copia debe estar presente
	if _, err := os.Stat(remoteBackupFile); err != nil {
		t.Errorf("la nueva copia confirmada %s debe existir en el servidor: %v", remoteBackupFile, err)
	}

	// 5. Verificar estado persistente
	if st.Profile(config.InitialProfileName).IsPending(config.PlatformServer) {
		t.Errorf("PendingSync.Server debiÃ³ ser false")
	}
	if st.Profile(config.InitialProfileName).LastSyncedFiles[config.PlatformServer] != filepath.Base(st.Profile(config.InitialProfileName).LastBackupFile) {
		t.Errorf("ServerLastSyncedFile (%s) != LastBackupFile (%s)", st.Profile(config.InitialProfileName).LastSyncedFiles[config.PlatformServer], filepath.Base(st.Profile(config.InitialProfileName).LastBackupFile))
	}
}

// 2. InterrupciÃ³n y recuperaciÃ³n: .tmp huÃ©rfano de corrida previa abortada (SecciÃ³n 18)
func TestServerIntegration_InterruptionAndRecovery(t *testing.T) {
	_, backupDir, serverDir, dbPath, statePath, lockPath := setupServerIntegrationEnv(t)

	// Simular corrida previa interrumpida que dejÃ³ un archivo .tmp huÃ©rfano
	orphanTmp := filepath.Join(serverDir, "CONTABILIDAD_TEST_20260910_0900.bak.tmp")
	if err := os.WriteFile(orphanTmp, []byte("INCOMPLETE_TRANSFER_FROM_CRASH"), 0o644); err != nil {
		t.Fatalf("crear orphan tmp: %v", err)
	}

	// Archivos ajenos que no deben verse afectados ni contar
	notesFile := filepath.Join(serverDir, "notes.txt")
	otherDB := filepath.Join(serverDir, "OTRABASE_20260910_1000.bak")
	_ = os.WriteFile(notesFile, []byte("notas"), 0o644)
	_ = os.WriteFile(otherDB, []byte("otra base de datos"), 0o644)

	engine := testdb.NewSQLiteSQLEngine(dbPath)
	serverBackend := server.New(server.Config{
		Enabled:    true,
		RemotePath: serverDir,
		Keep:       10,
		Database:   "CONTABILIDAD_TEST",
		TimeoutSec: 30,
	}, nil)

	app := application.New(application.Options{
		Config: config.Config{
			BackupDir: backupDir,
			Server:    "sqlite_local",
			Database:  "CONTABILIDAD_TEST",
			Retain:    3,
		},
		StatePath:    statePath,
		LockPath:     lockPath,
		SQLEngine:    engine,
		LocalBackend: local.New(backupDir),
		Backends:     []storage.Backend{serverBackend},
	})

	err := app.Backup(context.Background(), application.BackupOptions{})
	if err != nil {
		t.Fatalf("Backup fallÃ³ en corrida con temporales huÃ©rfanos: %v", err)
	}

	// 1. El temporal huÃ©rfano debiÃ³ ser limpiado automÃ¡ticamente
	if _, err := os.Stat(orphanTmp); !os.IsNotExist(err) {
		t.Errorf("el archivo temporal huÃ©rfano %s debiÃ³ ser eliminado", orphanTmp)
	}

	// 2. Los archivos ajenos deben conservarse intactos
	if _, err := os.Stat(notesFile); err != nil {
		t.Errorf("notes.txt debiÃ³ conservarse intacto: %v", err)
	}
	if _, err := os.Stat(otherDB); err != nil {
		t.Errorf("OTRABASE backup debiÃ³ conservarse intacto: %v", err)
	}

	// 3. El nuevo backup debe existir y ser vÃ¡lido
	st, _ := state.Load(statePath)
	target := filepath.Join(serverDir, filepath.Base(st.Profile(config.InitialProfileName).LastBackupFile))
	if _, err := os.Stat(target); err != nil {
		t.Errorf("el backup nuevo no se creÃ³ en el servidor: %v", err)
	}
}

// 3. PrevenciÃ³n de pÃ©rdida y recuperaciÃ³n: fallo de red no borra copias anteriores y permite sync posterior (SecciÃ³n 19)
func TestServerIntegration_PreventionOfLoss_NetworkDownThenSync(t *testing.T) {
	tempDir, backupDir, serverDir, dbPath, statePath, lockPath := setupServerIntegrationEnv(t)

	// Sembrar 10 copias vÃ¡lidas en el servidor
	originalHashes := make(map[string]string)
	for i := 1; i <= 10; i++ {
		name := fmt.Sprintf("CONTABILIDAD_TEST_202609%02d_1200.bak", i)
		p := filepath.Join(serverDir, name)
		content := []byte(fmt.Sprintf("ORIGINAL_CONTENT_%02d", i))
		if err := os.WriteFile(p, content, 0o644); err != nil {
			t.Fatalf("sembrar: %v", err)
		}
		h, _ := hasher.File(p)
		originalHashes[name] = h
	}

	engine := testdb.NewSQLiteSQLEngine(dbPath)

	// Simulamos servidor remoto caÃ­do usando una ruta invÃ¡lida o inaccesible
	unreachableDir := filepath.Join(tempDir, "unreachable_server", "share")
	failingBackend := server.New(server.Config{
		Enabled:    true,
		RemotePath: unreachableDir,
		Keep:       10,
		Database:   "CONTABILIDAD_TEST",
		TimeoutSec: 1,
	}, nil)

	// Decoramos o forzamos fallo recuperable
	flakyWrapper := &flakyBackendWrapper{
		inner:          failingBackend,
		forceUploadErr: storage.NewRetryableError(errors.New("error de red: share no accesible")),
	}

	app := application.New(application.Options{
		Config: config.Config{
			BackupDir: backupDir,
			Server:    "sqlite_local",
			Database:  "CONTABILIDAD_TEST",
			Retain:    3,
		},
		StatePath:    statePath,
		LockPath:     lockPath,
		SQLEngine:    engine,
		LocalBackend: local.New(backupDir),
		Backends:     []storage.Backend{flakyWrapper},
	})

	// Ejecutar Backup -> debe dar ErrPendingSync
	err := app.Backup(context.Background(), application.BackupOptions{})
	if !errors.Is(err, application.ErrPendingSync) {
		t.Fatalf("esperaba ErrPendingSync ante caÃ­da de red remota, dio: %v", err)
	}

	// Regla crÃ­tica de seguridad: Las 10 copias en serverDir continÃºan 100% intactas
	for name, wantHash := range originalHashes {
		p := filepath.Join(serverDir, name)
		gotHash, rerr := hasher.File(p)
		if rerr != nil {
			t.Fatalf("archivo original %s desapareciÃ³ o es inaccesible tras fallo: %v", name, rerr)
		}
		if gotHash != wantHash {
			t.Fatalf("archivo original %s fue alterado o corrompido", name)
		}
	}

	// Verificar estado
	st, _ := state.Load(statePath)
	if !st.Profile(config.InitialProfileName).IsPending(config.PlatformServer) {
		t.Errorf("PendingSync.Server debiÃ³ ser true")
	}

	// Restaurar servidor: ahora apunta a serverDir y sin error
	healthyBackend := server.New(server.Config{
		Enabled:    true,
		RemotePath: serverDir,
		Keep:       10,
		Database:   "CONTABILIDAD_TEST",
		TimeoutSec: 30,
	}, nil)

	healthyApp := application.New(application.Options{
		Config: config.Config{
			BackupDir: backupDir,
			Server:    "sqlite_local",
			Database:  "CONTABILIDAD_TEST",
			Retain:    3,
		},
		StatePath:    statePath,
		LockPath:     lockPath,
		SQLEngine:    engine,
		LocalBackend: local.New(backupDir),
		Backends:     []storage.Backend{healthyBackend},
	})

	// Sincronizar backup pendiente
	if err := healthyApp.Sync(context.Background(), application.SyncOptions{}); err != nil {
		t.Fatalf("Sync de recuperaciÃ³n fallÃ³: %v", err)
	}

	// Verificar que se haya copiado el archivo al servidor y rotado
	stAfter, _ := state.Load(statePath)
	if stAfter.Profile(config.InitialProfileName).IsPending(config.PlatformServer) {
		t.Errorf("PendingSync.Server debiÃ³ ser false tras Sync")
	}
	if stAfter.Profile(config.InitialProfileName).LastSyncedFiles[config.PlatformServer] != filepath.Base(st.Profile(config.InitialProfileName).LastBackupFile) {
		t.Errorf("ServerLastSyncedFile no coincide tras Sync")
	}

	// DeberÃ­an quedar 10 copias: el backup 1 se eliminÃ³, el nuevo existe
	entries, _ := os.ReadDir(serverDir)
	if len(entries) != 10 {
		t.Fatalf("tras rotaciÃ³n post-sync esperaba 10 copias, hay %d", len(entries))
	}
	if _, err := os.Stat(filepath.Join(serverDir, "CONTABILIDAD_TEST_20260901_1200.bak")); !os.IsNotExist(err) {
		t.Errorf("el backup mÃ¡s viejo (01) debiÃ³ ser eliminado")
	}
}

// 4. IntegraciÃ³n dual: R2 y Server trabajando en simultÃ¡neo en el mismo pipeline
func TestServerIntegration_DualRemote_R2AndServer(t *testing.T) {
	_, backupDir, serverDir, dbPath, statePath, lockPath := setupServerIntegrationEnv(t)

	engine := testdb.NewSQLiteSQLEngine(dbPath)
	mockR2 := NewMockBackend("r2")
	realServer := server.New(server.Config{
		Enabled:    true,
		RemotePath: serverDir,
		Keep:       10,
		Database:   "CONTABILIDAD_TEST",
		TimeoutSec: 30,
	}, nil)

	app := application.New(application.Options{
		Config: config.Config{
			BackupDir: backupDir,
			Server:    "sqlite_local",
			Database:  "CONTABILIDAD_TEST",
			Retain:    3,
		},
		StatePath:    statePath,
		LockPath:     lockPath,
		SQLEngine:    engine,
		LocalBackend: local.New(backupDir),
		Backends:     []storage.Backend{mockR2, realServer},
	})

	err := app.Backup(context.Background(), application.BackupOptions{})
	if err != nil {
		t.Fatalf("Backup dual fallÃ³: %v", err)
	}

	st, _ := state.Load(statePath)
	if st.Profile(config.InitialProfileName).IsPending(config.PlatformCloudflare) || st.Profile(config.InitialProfileName).IsPending(config.PlatformServer) {
		t.Errorf("ambos backends debieron sincronizar exitosamente")
	}
	if st.Profile(config.InitialProfileName).LastSyncedFiles[config.PlatformCloudflare] == "" || st.Profile(config.InitialProfileName).LastSyncedFiles[config.PlatformServer] == "" {
		t.Errorf("ambos archivos de sync deben estar registrados")
	}

	// R2 recibiÃ³ rotaciÃ³n de 1 copia
	if mockR2.rotateCalls != 1 {
		t.Errorf("R2 debiÃ³ recibir llamada de rotaciÃ³n, llamadas=%d", mockR2.rotateCalls)
	}

	// Server tiene el archivo verificado
	targetServerFile := filepath.Join(serverDir, filepath.Base(st.Profile(config.InitialProfileName).LastBackupFile))
	if _, err := os.Stat(targetServerFile); err != nil {
		t.Errorf("archivo en servidor remoto no existe: %v", err)
	}
}

type flakyBackendWrapper struct {
	inner          storage.Backend
	forceUploadErr error
}

func (w *flakyBackendWrapper) Name() string {
	return w.inner.Name()
}

func (w *flakyBackendWrapper) Upload(ctx context.Context, localPath string) error {
	if w.forceUploadErr != nil {
		return w.forceUploadErr
	}
	return w.inner.Upload(ctx, localPath)
}

func (w *flakyBackendWrapper) Rotate(ctx context.Context, keep int) error {
	return w.inner.Rotate(ctx, keep)
}

func (w *flakyBackendWrapper) LatestRemote(ctx context.Context) (string, error) {
	return w.inner.LatestRemote(ctx)
}

