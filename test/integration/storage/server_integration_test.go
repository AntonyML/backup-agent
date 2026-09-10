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

// 1. Pipeline real: Local -> Copia remota a Servidor -> Verificación integridad -> Rotación a 10
func TestServerIntegration_RealPipeline_UploadVerifyRotate10(t *testing.T) {
	_, backupDir, serverDir, dbPath, statePath, lockPath := setupServerIntegrationEnv(t)

	// Sembrar 10 copias válidas previas en el servidor remoto para probar la frontera exacta
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
		t.Fatalf("Backup falló: %v", err)
	}

	// 1. Verificar backup local
	st, err := state.Load(statePath)
	if err != nil {
		t.Fatalf("cargar state: %v", err)
	}
	if st.LastBackupFile == "" {
		t.Fatalf("LastBackupFile vacío en state")
	}
	localHash, err := hasher.File(st.LastBackupFile)
	if err != nil {
		t.Fatalf("calcular hash local: %v", err)
	}
	if st.SHA256 != localHash {
		t.Errorf("SHA256 en state (%s) != localHash (%s)", st.SHA256, localHash)
	}

	// 2. Verificar archivo en servidor remoto
	remoteBackupFile := filepath.Join(serverDir, filepath.Base(st.LastBackupFile))
	remoteHash, err := hasher.File(remoteBackupFile)
	if err != nil {
		t.Fatalf("calcular hash remoto: %v", err)
	}
	if remoteHash != localHash {
		t.Errorf("integridad remota violada: remoteHash (%s) != localHash (%s)", remoteHash, localHash)
	}

	// 3. Verificar que no queden temporales huérfanos
	tmps, _ := filepath.Glob(filepath.Join(serverDir, "*.tmp"))
	if len(tmps) != 0 {
		t.Errorf("quedaron temporales en servidor: %v", tmps)
	}

	// 4. Verificar rotación: deben quedar exactamente 10 copias
	entries, err := os.ReadDir(serverDir)
	if err != nil {
		t.Fatalf("leer serverDir: %v", err)
	}
	if len(entries) != 10 {
		t.Fatalf("esperaba exactamente 10 copias en servidor remoto tras rotación, hay %d", len(entries))
	}

	// La copia más vieja (20260901_1200.bak) debió ser eliminada
	oldestFile := filepath.Join(serverDir, "CONTABILIDAD_TEST_20260901_1200.bak")
	if _, err := os.Stat(oldestFile); !os.IsNotExist(err) {
		t.Errorf("el backup más viejo (%s) debió eliminarse", oldestFile)
	}

	// La nueva copia debe estar presente
	if _, err := os.Stat(remoteBackupFile); err != nil {
		t.Errorf("la nueva copia confirmada %s debe existir en el servidor: %v", remoteBackupFile, err)
	}

	// 5. Verificar estado persistente
	if st.PendingSync.Server {
		t.Errorf("PendingSync.Server debió ser false")
	}
	if st.ServerLastSyncedFile != filepath.Base(st.LastBackupFile) {
		t.Errorf("ServerLastSyncedFile (%s) != LastBackupFile (%s)", st.ServerLastSyncedFile, filepath.Base(st.LastBackupFile))
	}
}

// 2. Interrupción y recuperación: .tmp huérfano de corrida previa abortada (Sección 18)
func TestServerIntegration_InterruptionAndRecovery(t *testing.T) {
	_, backupDir, serverDir, dbPath, statePath, lockPath := setupServerIntegrationEnv(t)

	// Simular corrida previa interrumpida que dejó un archivo .tmp huérfano
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
		t.Fatalf("Backup falló en corrida con temporales huérfanos: %v", err)
	}

	// 1. El temporal huérfano debió ser limpiado automáticamente
	if _, err := os.Stat(orphanTmp); !os.IsNotExist(err) {
		t.Errorf("el archivo temporal huérfano %s debió ser eliminado", orphanTmp)
	}

	// 2. Los archivos ajenos deben conservarse intactos
	if _, err := os.Stat(notesFile); err != nil {
		t.Errorf("notes.txt debió conservarse intacto: %v", err)
	}
	if _, err := os.Stat(otherDB); err != nil {
		t.Errorf("OTRABASE backup debió conservarse intacto: %v", err)
	}

	// 3. El nuevo backup debe existir y ser válido
	st, _ := state.Load(statePath)
	target := filepath.Join(serverDir, filepath.Base(st.LastBackupFile))
	if _, err := os.Stat(target); err != nil {
		t.Errorf("el backup nuevo no se creó en el servidor: %v", err)
	}
}

// 3. Prevención de pérdida y recuperación: fallo de red no borra copias anteriores y permite sync posterior (Sección 19)
func TestServerIntegration_PreventionOfLoss_NetworkDownThenSync(t *testing.T) {
	tempDir, backupDir, serverDir, dbPath, statePath, lockPath := setupServerIntegrationEnv(t)

	// Sembrar 10 copias válidas en el servidor
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

	// Simulamos servidor remoto caído usando una ruta inválida o inaccesible
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
		t.Fatalf("esperaba ErrPendingSync ante caída de red remota, dio: %v", err)
	}

	// Regla crítica de seguridad: Las 10 copias en serverDir continúan 100% intactas
	for name, wantHash := range originalHashes {
		p := filepath.Join(serverDir, name)
		gotHash, rerr := hasher.File(p)
		if rerr != nil {
			t.Fatalf("archivo original %s desapareció o es inaccesible tras fallo: %v", name, rerr)
		}
		if gotHash != wantHash {
			t.Fatalf("archivo original %s fue alterado o corrompido", name)
		}
	}

	// Verificar estado
	st, _ := state.Load(statePath)
	if !st.PendingSync.Server {
		t.Errorf("PendingSync.Server debió ser true")
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
		t.Fatalf("Sync de recuperación falló: %v", err)
	}

	// Verificar que se haya copiado el archivo al servidor y rotado
	stAfter, _ := state.Load(statePath)
	if stAfter.PendingSync.Server {
		t.Errorf("PendingSync.Server debió ser false tras Sync")
	}
	if stAfter.ServerLastSyncedFile != filepath.Base(st.LastBackupFile) {
		t.Errorf("ServerLastSyncedFile no coincide tras Sync")
	}

	// Deberían quedar 10 copias: el backup 1 se eliminó, el nuevo existe
	entries, _ := os.ReadDir(serverDir)
	if len(entries) != 10 {
		t.Fatalf("tras rotación post-sync esperaba 10 copias, hay %d", len(entries))
	}
	if _, err := os.Stat(filepath.Join(serverDir, "CONTABILIDAD_TEST_20260901_1200.bak")); !os.IsNotExist(err) {
		t.Errorf("el backup más viejo (01) debió ser eliminado")
	}
}

// 4. Integración dual: R2 y Server trabajando en simultáneo en el mismo pipeline
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
		t.Fatalf("Backup dual falló: %v", err)
	}

	st, _ := state.Load(statePath)
	if st.PendingSync.R2 || st.PendingSync.Server {
		t.Errorf("ambos backends debieron sincronizar exitosamente")
	}
	if st.R2LastSyncedFile == "" || st.ServerLastSyncedFile == "" {
		t.Errorf("ambos archivos de sync deben estar registrados")
	}

	// R2 recibió rotación de 1 copia
	if mockR2.rotateCalls != 1 {
		t.Errorf("R2 debió recibir llamada de rotación, llamadas=%d", mockR2.rotateCalls)
	}

	// Server tiene el archivo verificado
	targetServerFile := filepath.Join(serverDir, filepath.Base(st.LastBackupFile))
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
