package application

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"femucaribe-backup-agent/internal/config"
	"femucaribe-backup-agent/internal/lock"
	"femucaribe-backup-agent/internal/state"
	"femucaribe-backup-agent/internal/storage"
)

type mockCloser struct{}

func (m mockCloser) Close() error { return nil }

type mockSQLEngine struct {
	backupErr error
	verifyErr error
}

func (m *mockSQLEngine) Open(server string, loginTimeoutSec int) (io.Closer, error) {
	return mockCloser{}, nil
}

func (m *mockSQLEngine) DatabaseSizeBytes(ctx context.Context, db io.Closer, database string) (int64, error) {
	return 1024, nil
}

func (m *mockSQLEngine) EnsureFreeSpace(dir string, neededBytes int64) error {
	return nil
}

func (m *mockSQLEngine) BackupDatabase(ctx context.Context, db io.Closer, database, targetPath string) error {
	if m.backupErr != nil {
		return m.backupErr
	}
	return os.WriteFile(targetPath, []byte("backup-valido-para-test"), 0o644)
}

func (m *mockSQLEngine) VerifyBackup(ctx context.Context, db io.Closer, targetPath string) error {
	return m.verifyErr
}

type mockBackend struct {
	name       string
	uploadErr  error
	uploadCalls int
	rotateCalls int
}

func (m *mockBackend) Name() string {
	return m.name
}

func (m *mockBackend) Upload(ctx context.Context, localPath string) error {
	m.uploadCalls++
	return m.uploadErr
}

func (m *mockBackend) Rotate(ctx context.Context, keep int) error {
	m.rotateCalls++
	return nil
}

func (m *mockBackend) LatestRemote(ctx context.Context) (string, error) {
	return "CONTABILIDAD_20260101_1000.bak", nil
}

func setupTestApp(t *testing.T, sqlEngine SQLEngine, backends []storage.Backend) (*App, string, string) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	lockPath := filepath.Join(tmpDir, "agent.lock")
	backupDir := filepath.Join(tmpDir, "backups")
	logDir := filepath.Join(tmpDir, "logs")

	cfg := config.Config{
		BackupDir:        backupDir,
		Server:           "localhost",
		Database:         "CONTABILIDAD",
		Retain:           3,
		LoginTimeoutSec:  15,
		BackupTimeoutSec: 60,
	}

	discardLogger := slog.New(slog.NewTextHandler(io.Discard, nil))

	app := New(Options{
		Config:       cfg,
		StatePath:    statePath,
		LockPath:     lockPath,
		LogDir:       logDir,
		Backends:     backends,
		LocalBackend: &mockBackend{name: "local"},
		SQLEngine:    sqlEngine,
		Logger:       discardLogger,
	})

	return app, statePath, lockPath
}

func TestBackup_Success(t *testing.T) {
	mockR2 := &mockBackend{name: "r2"}
	app, statePath, _ := setupTestApp(t, &mockSQLEngine{}, []storage.Backend{mockR2})

	err := app.Backup(context.Background(), BackupOptions{})
	if err != nil {
		t.Fatalf("Backup falló inesperadamente: %v", err)
	}

	st, err := state.Load(statePath)
	if err != nil {
		t.Fatalf("Load state falló: %v", err)
	}

	if st.LastRunDate != state.Today() {
		t.Errorf("LastRunDate no es hoy: %s", st.LastRunDate)
	}
	if st.PendingSync.R2 {
		t.Errorf("PendingSync.R2 debería ser false")
	}
	if mockR2.uploadCalls != 1 {
		t.Errorf("R2 upload debería haberse llamado 1 vez, dio %d", mockR2.uploadCalls)
	}
}

func TestBackup_RetryableError_SetsPendingSync(t *testing.T) {
	mockR2 := &mockBackend{
		name:      "r2",
		uploadErr: storage.NewRetryableError(errors.New("timeout R2")),
	}
	app, statePath, _ := setupTestApp(t, &mockSQLEngine{}, []storage.Backend{mockR2})

	err := app.Backup(context.Background(), BackupOptions{})
	if !errors.Is(err, ErrPendingSync) {
		t.Fatalf("esperaba ErrPendingSync, dio: %v", err)
	}

	st, err := state.Load(statePath)
	if err != nil {
		t.Fatalf("Load state falló: %v", err)
	}

	// El backup local debe haberse completado exitosamente
	if st.LastRunDate != state.Today() {
		t.Errorf("LastRunDate debe actualizarse con el backup local")
	}
	if !st.PendingSync.R2 {
		t.Errorf("PendingSync.R2 debe ser true ante RetryableError")
	}
}

func TestBackup_Idempotency(t *testing.T) {
	mockR2 := &mockBackend{name: "r2"}
	app, statePath, _ := setupTestApp(t, &mockSQLEngine{}, []storage.Backend{mockR2})

	// Pre-configurar state con fecha de hoy
	_ = state.Save(statePath, &state.State{
		LastRunDate:    state.Today(),
		LastBackupFile: "fake.bak",
	})

	err := app.Backup(context.Background(), BackupOptions{Force: false})
	if !errors.Is(err, ErrAlreadyRanToday) {
		t.Fatalf("esperaba ErrAlreadyRanToday, dio: %v", err)
	}

	// Con Force: true, debe ejecutarse sin respetar la idempotencia
	err = app.Backup(context.Background(), BackupOptions{Force: true})
	if err != nil {
		t.Fatalf("con Force: true no debería fallar: %v", err)
	}
}

func TestBackup_Locked(t *testing.T) {
	mockR2 := &mockBackend{name: "r2"}
	app, _, lockPath := setupTestApp(t, &mockSQLEngine{}, []storage.Backend{mockR2})

	// Adquirir lock previamente
	lh, err := lock.Acquire(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer lh.Release()

	err = app.Backup(context.Background(), BackupOptions{})
	if !errors.Is(err, ErrLocked) {
		t.Fatalf("esperaba ErrLocked, dio: %v", err)
	}
}

func TestSync_PendingSuccess(t *testing.T) {
	tmpDir := t.TempDir()
	pendingFile := filepath.Join(tmpDir, "CONTABILIDAD_20260101_1000.bak")
	if err := os.WriteFile(pendingFile, []byte("datos"), 0o644); err != nil {
		t.Fatal(err)
	}

	mockR2 := &mockBackend{name: "r2"}
	app, statePath, _ := setupTestApp(t, &mockSQLEngine{}, []storage.Backend{mockR2})

	_ = state.Save(statePath, &state.State{
		LastRunDate:    "2026-01-01",
		LastBackupFile: pendingFile,
		PendingSync: state.PendingSync{
			R2: true,
		},
	})

	err := app.Sync(context.Background(), SyncOptions{})
	if err != nil {
		t.Fatalf("Sync falló: %v", err)
	}

	st, _ := state.Load(statePath)
	if st.PendingSync.R2 {
		t.Errorf("PendingSync.R2 debería haberse limpiado a false")
	}
	if mockR2.uploadCalls != 1 {
		t.Errorf("R2 upload debió ejecutarse 1 vez, dio %d", mockR2.uploadCalls)
	}
}

func TestStatus_Report(t *testing.T) {
	app, statePath, _ := setupTestApp(t, &mockSQLEngine{}, nil)

	_ = state.Save(statePath, &state.State{
		LastRunDate:    "2026-09-10",
		LastBackupFile: `C:\Backups\test.bak`,
		SHA256:         "hash123",
		PendingSync:    state.PendingSync{R2: false},
	})

	report, err := app.Status(context.Background())
	if err != nil {
		t.Fatalf("Status falló: %v", err)
	}

	if report.LastRunDate != "2026-09-10" || report.SHA256 != "hash123" {
		t.Errorf("datos del reporte incorrectos: %+v", report)
	}
}

func TestTailLogs(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "logs")
	_ = os.MkdirAll(logDir, 0o755)

	today := time.Now().Format("2006-01-02")
	logFile := filepath.Join(logDir, "agent-"+today+".log")
	content := "linea 1\nlinea 2\nlinea 3\nlinea 4\nlinea 5\n"
	_ = os.WriteFile(logFile, []byte(content), 0o644)

	app := New(Options{
		LogDir: logDir,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})

	lines, err := app.TailLogs(context.Background(), 3)
	if err != nil {
		t.Fatalf("TailLogs falló: %v", err)
	}
	if len(lines) != 3 {
		t.Fatalf("esperaba 3 líneas, dio %d", len(lines))
	}
	if lines[2] != "linea 5" {
		t.Errorf("última línea incorrecta: %s", lines[2])
	}
}
