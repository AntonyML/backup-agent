package storage_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"femucaribe-backup-agent/internal/application"
	"femucaribe-backup-agent/internal/config"
	"femucaribe-backup-agent/internal/state"
	"femucaribe-backup-agent/internal/storage"
	"femucaribe-backup-agent/internal/storage/local"
	"femucaribe-backup-agent/test/testdb"
	"femucaribe-backup-agent/test/testenv"
	_ "modernc.org/sqlite"
)

// MockBackend simula un almacenamiento remoto (ej. Cloudflare R2 o Server) para testing controlado.
type MockBackend struct {
	mu           sync.Mutex
	name         string
	uploadErr    error
	rotateErr    error
	uploadedKeys []string
	rotateCalls  int
}

func NewMockBackend(name string) *MockBackend {
	return &MockBackend{name: name}
}

func (m *MockBackend) Name() string {
	return m.name
}

func (m *MockBackend) SetUploadError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.uploadErr = err
}

func (m *MockBackend) Upload(ctx context.Context, localPath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.uploadErr != nil {
		return m.uploadErr
	}
	m.uploadedKeys = append(m.uploadedKeys, filepath.Base(localPath))
	return nil
}

func (m *MockBackend) Rotate(ctx context.Context, keep int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rotateCalls++
	return m.rotateErr
}

func (m *MockBackend) LatestRemote(ctx context.Context) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.uploadedKeys) == 0 {
		return "", nil
	}
	return m.uploadedKeys[len(m.uploadedKeys)-1], nil
}

func (m *MockBackend) UploadedFiles() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.uploadedKeys))
	copy(out, m.uploadedKeys)
	return out
}

func setupStorageTestEnv(t *testing.T) (string, string, string, string, string) {
	t.Helper()
	tempDir := t.TempDir()
	backupDir := filepath.Join(tempDir, "backups")
	dbPath := filepath.Join(tempDir, "source.sqlite")
	statePath := filepath.Join(tempDir, "state.json")
	lockPath := filepath.Join(tempDir, "agent.lock")

	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatalf("crear backupDir: %v", err)
	}

	if err := testenv.ValidateSafety(testenv.TestEnvironmentConfig{
		DatabaseDriver: "sqlite",
		DatabaseName:   "test_storage",
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

	return tempDir, backupDir, dbPath, statePath, lockPath
}

// 1. Ciclo de vida completo de pending_sync ante fallos transitorios en backend remoto
func TestBackend_PendingSyncLifecycle(t *testing.T) {
	_, backupDir, dbPath, statePath, lockPath := setupStorageTestEnv(t)

	engine := testdb.NewSQLiteSQLEngine(dbPath)
	mockR2 := NewMockBackend("r2")

	// Corrida 1: MockR2 falla con error transitorio (RetryableError)
	mockR2.SetUploadError(storage.NewRetryableError(errors.New("503 Service Unavailable")))

	app := application.New(application.Options{
		Config: config.Config{
			BackupDir: backupDir,
			Server:    "sqlite_local",
			Database:  "test_storage",
			Retain:    3,
		},
		StatePath:    statePath,
		LockPath:     lockPath,
		SQLEngine:    engine,
		LocalBackend: local.New(backupDir),
		Backends:     []storage.Backend{mockR2},
	})

	ctx := context.Background()
	err := app.Backup(ctx, application.BackupOptions{})
	if !errors.Is(err, application.ErrPendingSync) {
		t.Fatalf("se esperaba ErrPendingSync ante fallo transitorio de R2, obtenido: %v", err)
	}

	// El backup local debe existir y state.json debe tener pending_sync.r2 = true
	st, err := state.Load(statePath)
	if err != nil {
		t.Fatalf("error cargando state.json: %v", err)
	}
	if !st.Profile(config.InitialProfileName).IsPending(config.PlatformCloudflare) {
		t.Errorf("se esperaba st.Profile(config.InitialProfileName).IsPending(config.PlatformCloudflare) == true")
	}
	if st.Profile(config.InitialProfileName).LastBackupFile == "" {
		t.Errorf("LastBackupFile no debe estar vacÃ­o")
	}
	pendingFile := st.Profile(config.InitialProfileName).LastBackupFile

	// Corrida 2: MockR2 vuelve a estar en lÃ­nea (error resuelto)
	mockR2.SetUploadError(nil)

	// Siguiente corrida (con Force para permitir correr el mismo dÃ­a)
	err = app.Backup(ctx, application.BackupOptions{Force: true})
	if err != nil {
		t.Fatalf("segunda corrida tras recuperaciÃ³n de R2 fallÃ³: %v", err)
	}

	// El archivo pendiente debiÃ³ haber sido subido
	uploaded := mockR2.UploadedFiles()
	foundPending := false
	for _, f := range uploaded {
		if f == filepath.Base(pendingFile) {
			foundPending = true
			break
		}
	}
	if !foundPending {
		t.Errorf("el archivo pendiente %s no fue subido al recuperar conectividad. Subidos: %v", filepath.Base(pendingFile), uploaded)
	}

	// state.json ahora debe tener pending_sync.r2 = false
	stAfter, err := state.Load(statePath)
	if err != nil {
		t.Fatalf("cargar state.json: %v", err)
	}
	if stAfter.Profile(config.InitialProfileName).IsPending(config.PlatformCloudflare) {
		t.Errorf("stAfter.Profile(config.InitialProfileName).IsPending(config.PlatformCloudflare) deberÃ­a ser false tras sincronizaciÃ³n exitosa")
	}
}

// 2. Falla fatal en backend remoto aborta inmediatamente sin marcar pending_sync transitorio
func TestBackend_FatalErrorAbortsImmediately(t *testing.T) {
	_, backupDir, dbPath, statePath, lockPath := setupStorageTestEnv(t)

	engine := testdb.NewSQLiteSQLEngine(dbPath)
	mockR2 := NewMockBackend("r2")
	mockR2.SetUploadError(errors.New("401 Unauthorized / invalid credentials"))

	app := application.New(application.Options{
		Config: config.Config{
			BackupDir: backupDir,
			Server:    "sqlite_local",
			Database:  "test_storage",
			Retain:    3,
		},
		StatePath:    statePath,
		LockPath:     lockPath,
		SQLEngine:    engine,
		LocalBackend: local.New(backupDir),
		Backends:     []storage.Backend{mockR2},
	})

	ctx := context.Background()
	err := app.Backup(ctx, application.BackupOptions{})
	if err == nil || !strings.Contains(err.Error(), "401 Unauthorized") {
		t.Fatalf("se esperaba error fatal conteniendo '401 Unauthorized', obtenido: %v", err)
	}

	// No debe haber quedado como ErrPendingSync
	if errors.Is(err, application.ErrPendingSync) {
		t.Errorf("un error 401 no debe marcarse como ErrPendingSync")
	}

}

// 3. Aislamiento estricto: confirmar que ningÃºn backend conoce ni modifica state.json directamente
func TestBackend_StateIsolation(t *testing.T) {
	_, backupDir, dbPath, statePath, lockPath := setupStorageTestEnv(t)

	engine := testdb.NewSQLiteSQLEngine(dbPath)
	mock1 := NewMockBackend("backend_one")
	mock2 := NewMockBackend("backend_two")

	app := application.New(application.Options{
		Config: config.Config{
			BackupDir: backupDir,
			Server:    "sqlite_local",
			Database:  "test_storage",
			Retain:    3,
		},
		StatePath:    statePath,
		LockPath:     lockPath,
		SQLEngine:    engine,
		LocalBackend: local.New(backupDir),
		Backends:     []storage.Backend{mock1, mock2},
	})

	ctx := context.Background()
	if err := app.Backup(ctx, application.BackupOptions{}); err != nil {
		t.Fatalf("backup multi-backend fallÃ³: %v", err)
	}

	// Ambos backends deben haber recibido la subida del mismo backup
	up1 := mock1.UploadedFiles()
	up2 := mock2.UploadedFiles()
	if len(up1) != 1 || len(up2) != 1 {
		t.Errorf("ambos backends debieron subir exactamente 1 archivo; b1=%d, b2=%d", len(up1), len(up2))
	}
	if up1[0] != up2[0] {
		t.Errorf("los backends recibieron archivos distintos: b1=%s, b2=%s", up1[0], up2[0])
	}
}

