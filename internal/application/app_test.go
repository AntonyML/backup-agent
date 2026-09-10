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

func TestRemoveTmpOrphans_KillRecovery(t *testing.T) {
	tmpDir := t.TempDir()
	mustWrite := func(name string) {
		if err := os.WriteFile(filepath.Join(tmpDir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite("CONTABILIDAD_20260101_1200.bak")
	mustWrite("CONTABILIDAD_20260102_1200.bak.tmp")
	mustWrite("state.json")

	app := New(Options{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	n, err := app.removeTmpOrphans(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("debería borrar 1 huérfano, borró %d", n)
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "CONTABILIDAD_20260101_1200.bak")); err != nil {
		t.Error("el .bak final no debe tocarse")
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "CONTABILIDAD_20260102_1200.bak.tmp")); err == nil {
		t.Error("el .tmp huérfano debería haber sido borrado")
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "state.json")); err != nil {
		t.Error("state.json no debe tocarse")
	}
}

func TestAtomicRename(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a.bak.tmp")
	dst := filepath.Join(dir, "a.bak")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := atomicRename(src, dst); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(src); err == nil {
		t.Error("rename debería mover src")
	}
	if _, err := os.Stat(dst); err != nil {
		t.Error("rename debería crear dst")
	}

	// Con destino existente: reemplaza.
	if err := os.WriteFile(src, []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := atomicRename(src, dst); err != nil {
		t.Fatalf("rename con destino existente: %v", err)
	}
	content, _ := os.ReadFile(dst)
	if string(content) != "y" {
		t.Errorf("debería haber reemplazado el contenido")
	}
}

func TestGetTUIStatus(t *testing.T) {
	mockR2 := &mockBackend{name: "r2"}
	app, statePath, _ := setupTestApp(t, &mockSQLEngine{}, []storage.Backend{mockR2})

	// Caso 1: Never run
	bStatus, backends, err := app.GetTUIStatus(context.Background())
	if err != nil {
		t.Fatalf("GetTUIStatus falló: %v", err)
	}
	if bStatus.Result != "never_run" {
		t.Errorf("esperaba never_run, dio: %s", bStatus.Result)
	}
	if len(backends) != 3 {
		t.Fatalf("esperaba 3 backends (Local, R2, Server), dio %d", len(backends))
	}
	if backends[0].Name != "Local" || backends[1].Name != "R2" || backends[2].Name != "Server" {
		t.Errorf("nombres de backends inesperados: %+v", backends)
	}
	if backends[2].Configured {
		t.Errorf("Server debería figurar como no configurado")
	}

	// Caso 2: Con estado exitoso
	tmpFile := filepath.Join(t.TempDir(), "CONTABILIDAD_20260910_1000.bak")
	_ = os.WriteFile(tmpFile, []byte("ok"), 0o644)
	_ = state.Save(statePath, &state.State{
		LastRunDate:      "2026-09-10",
		LastBackupFile:   tmpFile,
		SHA256:           "hash123",
		PendingSync:      state.PendingSync{R2: false},
		R2LastSyncedFile: "CONTABILIDAD_20260910_1000.bak",
	})

	bStatus, backends, err = app.GetTUIStatus(context.Background())
	if err != nil {
		t.Fatalf("GetTUIStatus falló: %v", err)
	}
	if bStatus.Result != "success" {
		t.Errorf("esperaba success, dio: %s", bStatus.Result)
	}
	if backends[1].StatusText != "OK" {
		t.Errorf("esperaba R2 OK, dio: %s", backends[1].StatusText)
	}

	// Caso 3: Con pending_sync
	_ = state.Save(statePath, &state.State{
		LastRunDate:    "2026-09-10",
		LastBackupFile: tmpFile,
		PendingSync:    state.PendingSync{R2: true},
	})
	bStatus, backends, _ = app.GetTUIStatus(context.Background())
	if bStatus.Result != "pending_sync" {
		t.Errorf("esperaba pending_sync, dio: %s", bStatus.Result)
	}
	if backends[1].StatusText != "PENDING" {
		t.Errorf("esperaba R2 PENDING, dio: %s", backends[1].StatusText)
	}
}

func TestSaveAndGetR2Credentials(t *testing.T) {
	tmpDir := t.TempDir()
	datPath := filepath.Join(tmpDir, "config.dat")

	app := New(Options{
		SecretsPath: datPath,
	})

	err := app.SaveR2Credentials("https://example.r2.cloudflarestorage.com", "my-bucket", "accKey", "secKey")
	if err != nil {
		t.Fatalf("SaveR2Credentials falló: %v", err)
	}

	creds, err := app.GetR2Credentials()
	if err != nil {
		t.Fatalf("GetR2Credentials falló: %v", err)
	}
	if creds.Endpoint != "https://example.r2.cloudflarestorage.com" || creds.Bucket != "my-bucket" || creds.AccessKeyID != "accKey" || creds.SecretAccessKey != "secKey" {
		t.Errorf("credenciales recuperadas no coinciden: %+v", creds)
	}
}

func TestBackup_ServerRetryableError_SetsPendingSyncServer(t *testing.T) {
	r2Backend := &mockBackend{name: "r2"}
	serverBackend := &mockBackend{
		name:      "server",
		uploadErr: storage.NewRetryableError(errors.New("servidor UNC inaccesible")),
	}

	app, statePath, _ := setupTestApp(t, &mockSQLEngine{}, []storage.Backend{r2Backend, serverBackend})

	ctx := context.Background()
	err := app.Backup(ctx, BackupOptions{})
	if !errors.Is(err, ErrPendingSync) {
		t.Fatalf("esperaba ErrPendingSync, dio: %v", err)
	}

	st, err := state.Load(statePath)
	if err != nil {
		t.Fatalf("Load state falló: %v", err)
	}

	// Local debe ser exitoso
	if st.LastBackupFile == "" || st.SHA256 == "" {
		t.Errorf("backup local debió confirmarse en state")
	}

	// R2 exitoso (no pendiente)
	if st.PendingSync.R2 {
		t.Errorf("PendingSync.R2 debió ser false")
	}
	if st.R2LastSyncedFile == "" {
		t.Errorf("R2LastSyncedFile debió registrarse")
	}

	// Server falló transitoriamente (pendiente)
	if !st.PendingSync.Server {
		t.Errorf("PendingSync.Server debió ser true")
	}
	if st.ServerLastSyncedFile != "" {
		t.Errorf("ServerLastSyncedFile debió permanecer vacío")
	}
}

func TestSync_ServerPendingSync_RetriesOnlyPending(t *testing.T) {
	r2Backend := &mockBackend{name: "r2"}
	serverBackend := &mockBackend{name: "server"}

	app, statePath, _ := setupTestApp(t, &mockSQLEngine{}, []storage.Backend{r2Backend, serverBackend})

	tmpFile := filepath.Join(t.TempDir(), "CONTABILIDAD_20260910_1000.bak")
	_ = os.WriteFile(tmpFile, []byte("contenido-valido"), 0o644)

	// Estado previo: R2 sincronizado, Server pendiente
	_ = state.Save(statePath, &state.State{
		LastRunDate:      "2026-09-10",
		LastBackupFile:   tmpFile,
		SHA256:           "hash123",
		PendingSync:      state.PendingSync{R2: false, Server: true},
		R2LastSyncedFile: "CONTABILIDAD_20260910_1000.bak",
	})

	err := app.Sync(context.Background(), SyncOptions{})
	if err != nil {
		t.Fatalf("Sync falló: %v", err)
	}

	// R2 no debió ser llamado porque ya estaba sincronizado
	if r2Backend.uploadCalls != 0 {
		t.Errorf("R2 no debió ser re-subido, llamadas=%d", r2Backend.uploadCalls)
	}

	// Server sí debió ser llamado
	if serverBackend.uploadCalls != 1 {
		t.Errorf("Server debió recibir 1 llamada de sync, recibió=%d", serverBackend.uploadCalls)
	}

	// Tras la sincronización, Server ya no debe estar pendiente
	st, _ := state.Load(statePath)
	if st.PendingSync.Server {
		t.Errorf("PendingSync.Server debió ser false tras Sync exitoso")
	}
	if st.ServerLastSyncedFile != "CONTABILIDAD_20260910_1000.bak" {
		t.Errorf("ServerLastSyncedFile no coincide: %s", st.ServerLastSyncedFile)
	}
}

func TestBackup_PreSync_DeferredRetryBeforeDaily(t *testing.T) {
	serverBackend := &mockBackend{name: "server"}
	app, statePath, _ := setupTestApp(t, &mockSQLEngine{}, []storage.Backend{serverBackend})

	oldBackup := filepath.Join(t.TempDir(), "CONTABILIDAD_20260909_1000.bak")
	_ = os.WriteFile(oldBackup, []byte("backup-ayer"), 0o644)

	_ = state.Save(statePath, &state.State{
		LastRunDate:    "2026-09-09",
		LastBackupFile: oldBackup,
		SHA256:         "hash-ayer",
		PendingSync:    state.PendingSync{Server: true},
	})

	// Ejecutar backup del día siguiente
	err := app.Backup(context.Background(), BackupOptions{Force: true})
	if err != nil {
		t.Fatalf("Backup falló: %v", err)
	}

	// Server debió recibir 2 llamadas: 1 para el pendiente de ayer + 1 para el de hoy
	if serverBackend.uploadCalls != 2 {
		t.Errorf("esperaba 2 subidas al servidor (pendiente previo + nuevo del día), hubo %d", serverBackend.uploadCalls)
	}

	st, _ := state.Load(statePath)
	if st.PendingSync.Server {
		t.Errorf("PendingSync.Server debió quedar en false")
	}
}

func TestGetTUIStatus_ServerConfiguredAndPending(t *testing.T) {
	serverBackend := &mockBackend{name: "server"}
	app, statePath, _ := setupTestApp(t, &mockSQLEngine{}, []storage.Backend{serverBackend})

	tmpFile := filepath.Join(t.TempDir(), "CONTABILIDAD_20260910_1000.bak")
	_ = os.WriteFile(tmpFile, []byte("ok"), 0o644)

	// Caso: Server configurado y PENDING
	_ = state.Save(statePath, &state.State{
		LastRunDate:    "2026-09-10",
		LastBackupFile: tmpFile,
		PendingSync:    state.PendingSync{Server: true},
	})

	bStatus, backends, err := app.GetTUIStatus(context.Background())
	if err != nil {
		t.Fatalf("GetTUIStatus falló: %v", err)
	}

	if bStatus.Result != "pending_sync" {
		t.Errorf("esperaba pending_sync por server pendiente, dio: %s", bStatus.Result)
	}

	var serverStatus BackendStatus
	for _, b := range backends {
		if b.Name == "Server" {
			serverStatus = b
			break
		}
	}

	if !serverStatus.Configured {
		t.Errorf("Server debería figurar como configurado")
	}
	if serverStatus.StatusText != "PENDING" {
		t.Errorf("esperaba status PENDING para Server, dio: %s", serverStatus.StatusText)
	}
	if !serverStatus.PendingSync {
		t.Errorf("PendingSync en ServerStatus debería ser true")
	}

	// Caso: Server OK
	_ = state.Save(statePath, &state.State{
		LastRunDate:          "2026-09-10",
		LastBackupFile:       tmpFile,
		PendingSync:          state.PendingSync{Server: false},
		ServerLastSyncedFile: "CONTABILIDAD_20260910_1000.bak",
	})

	bStatus, backends, _ = app.GetTUIStatus(context.Background())
	if bStatus.Result != "success" {
		t.Errorf("esperaba success, dio: %s", bStatus.Result)
	}
	for _, b := range backends {
		if b.Name == "Server" {
			serverStatus = b
			break
		}
	}
	if serverStatus.StatusText != "OK" {
		t.Errorf("esperaba status OK para Server, dio: %s", serverStatus.StatusText)
	}
	if !serverStatus.LastSyncOK {
		t.Errorf("LastSyncOK en ServerStatus debería ser true")
	}
}



