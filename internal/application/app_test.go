package application

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"femucaribe-backup-agent/internal/config"
	"femucaribe-backup-agent/internal/events"
	"femucaribe-backup-agent/internal/lock"
	"femucaribe-backup-agent/internal/sqlbackup"
	"femucaribe-backup-agent/internal/state"
	"femucaribe-backup-agent/internal/storage"
)

type mockEventRepo struct {
	events []events.Event
	err    error
}

func (m *mockEventRepo) Append(ctx context.Context, event events.Event) error {
	if m.err != nil {
		return m.err
	}
	m.events = append(m.events, event)
	return nil
}

type mockCloser struct{}

func (m mockCloser) Close() error { return nil }

type mockSQLEngine struct {
	backupErr error
	verifyErr error
}

func (m *mockSQLEngine) Open(opts sqlbackup.ConnectOptions) (io.Closer, error) {
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
		// D9: default productivo true (config cargada de archivo lo hereda de Default()).
		Schedule: config.ScheduleConfig{SyncAfterBackup: true},
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
		t.Fatalf("Backup fallÃ³ inesperadamente: %v", err)
	}

	st, err := state.Load(statePath)
	if err != nil {
		t.Fatalf("Load state fallÃ³: %v", err)
	}

	if st.Profile(config.InitialProfileName).LastRunDate != state.Today() {
		t.Errorf("LastRunDate no es hoy: %s", st.Profile(config.InitialProfileName).LastRunDate)
	}
	if st.Profile(config.InitialProfileName).IsPending(config.PlatformCloudflare) {
		t.Errorf("PendingSync.R2 deberÃ­a ser false")
	}
	if mockR2.uploadCalls != 1 {
		t.Errorf("R2 upload deberÃ­a haberse llamado 1 vez, dio %d", mockR2.uploadCalls)
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
		t.Fatalf("Load state fallÃ³: %v", err)
	}

	// El backup local debe haberse completado exitosamente
	if st.Profile(config.InitialProfileName).LastRunDate != state.Today() {
		t.Errorf("LastRunDate debe actualizarse con el backup local")
	}
	if !st.Profile(config.InitialProfileName).IsPending(config.PlatformCloudflare) {
		t.Errorf("PendingSync.R2 debe ser true ante RetryableError")
	}
}

func TestBackup_Idempotency(t *testing.T) {
	mockR2 := &mockBackend{name: "r2"}
	app, statePath, _ := setupTestApp(t, &mockSQLEngine{}, []storage.Backend{mockR2})

	// Pre-configurar state con fecha de hoy
	_ = state.Save(statePath, &state.State{
	Profiles: map[string]*state.ProfileState{
		config.InitialProfileName: {LastRunDate: state.Today(), LastBackupFile: "fake.bak"},
	},
})

	err := app.Backup(context.Background(), BackupOptions{Force: false})
	if !errors.Is(err, ErrAlreadyRanToday) {
		t.Fatalf("esperaba ErrAlreadyRanToday, dio: %v", err)
	}

	// Con Force: true, debe ejecutarse sin respetar la idempotencia
	err = app.Backup(context.Background(), BackupOptions{Force: true})
	if err != nil {
		t.Fatalf("con Force: true no deberÃ­a fallar: %v", err)
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
	Profiles: map[string]*state.ProfileState{
		config.InitialProfileName: {
			LastRunDate:    "2026-01-01",
			LastBackupFile: pendingFile,
			PendingSync:    map[string]bool{config.PlatformCloudflare: true},
		},
	},
})

	err := app.Sync(context.Background(), SyncOptions{})
	if err != nil {
		t.Fatalf("Sync fallÃ³: %v", err)
	}

	st, _ := state.Load(statePath)
	if st.Profile(config.InitialProfileName).IsPending(config.PlatformCloudflare) {
		t.Errorf("PendingSync.R2 deberÃ­a haberse limpiado a false")
	}
	if mockR2.uploadCalls != 1 {
		t.Errorf("R2 upload debiÃ³ ejecutarse 1 vez, dio %d", mockR2.uploadCalls)
	}
}

func TestStatus_Report(t *testing.T) {
	app, statePath, _ := setupTestApp(t, &mockSQLEngine{}, nil)

	_ = state.Save(statePath, &state.State{
		Profiles: map[string]*state.ProfileState{
			config.InitialProfileName: {
				LastRunDate:    "2026-09-10",
				LastBackupFile: `C:\Backups\test.bak`,
				SHA256:         "hash123",
			},
		},
	})

	report, err := app.Status(context.Background())
	if err != nil {
		t.Fatalf("Status fallÃ³: %v", err)
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
		t.Fatalf("TailLogs fallÃ³: %v", err)
	}
	if len(lines) != 3 {
		t.Fatalf("esperaba 3 lÃ­neas, dio %d", len(lines))
	}
	if lines[2] != "linea 5" {
		t.Errorf("Ãºltima lÃ­nea incorrecta: %s", lines[2])
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
		t.Errorf("deberÃ­a borrar 1 huÃ©rfano, borrÃ³ %d", n)
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "CONTABILIDAD_20260101_1200.bak")); err != nil {
		t.Error("el .bak final no debe tocarse")
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "CONTABILIDAD_20260102_1200.bak.tmp")); err == nil {
		t.Error("el .tmp huÃ©rfano deberÃ­a haber sido borrado")
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
		t.Error("rename deberÃ­a mover src")
	}
	if _, err := os.Stat(dst); err != nil {
		t.Error("rename deberÃ­a crear dst")
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
		t.Errorf("deberÃ­a haber reemplazado el contenido")
	}
}

func TestGetTUIStatus(t *testing.T) {
	mockR2 := &mockBackend{name: "r2"}
	app, statePath, _ := setupTestApp(t, &mockSQLEngine{}, []storage.Backend{mockR2})

	// Caso 1: Never run
	bStatus, backends, err := app.GetTUIStatus(context.Background())
	if err != nil {
		t.Fatalf("GetTUIStatus fallÃ³: %v", err)
	}
	if bStatus.Result != "never_run" {
		t.Errorf("esperaba never_run, dio: %s", bStatus.Result)
	}
	if len(backends) != 4 {
		t.Fatalf("esperaba 4 backends (Local, R2, Server, Supabase), dio %d", len(backends))
	}
	if backends[0].Name != "Local" || backends[1].Name != "R2" || backends[2].Name != "Server" || backends[3].Name != "Supabase" {
		t.Errorf("nombres de backends inesperados: %+v", backends)
	}
	if backends[2].Configured {
		t.Errorf("Server deberÃ­a figurar como no configurado")
	}
	if backends[3].Configured {
		t.Errorf("Supabase deberÃ­a figurar como no configurado")
	}

	// Caso 2: Con estado exitoso
	tmpFile := filepath.Join(t.TempDir(), "CONTABILIDAD_20260910_1000.bak")
	_ = os.WriteFile(tmpFile, []byte("ok"), 0o644)
	_ = state.Save(statePath, &state.State{
		Profiles: map[string]*state.ProfileState{
			config.InitialProfileName: {
				LastRunDate:     "2026-09-10",
				LastBackupFile:  tmpFile,
				SHA256:          "hash123",
				LastSyncedFiles: map[string]string{config.PlatformCloudflare: "CONTABILIDAD_20260910_1000.bak"},
			},
		},
	})

	bStatus, backends, err = app.GetTUIStatus(context.Background())
	if err != nil {
		t.Fatalf("GetTUIStatus fallÃ³: %v", err)
	}
	if bStatus.Result != "success" {
		t.Errorf("esperaba success, dio: %s", bStatus.Result)
	}
	if backends[1].StatusText != "OK" {
		t.Errorf("esperaba R2 OK, dio: %s", backends[1].StatusText)
	}

	// Caso 3: Con pending_sync
	_ = state.Save(statePath, &state.State{
		Profiles: map[string]*state.ProfileState{
			config.InitialProfileName: {
				LastRunDate:    "2026-09-10",
				LastBackupFile: tmpFile,
				PendingSync:    map[string]bool{config.PlatformCloudflare: true},
			},
		},
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
		t.Fatalf("SaveR2Credentials fallÃ³: %v", err)
	}

	creds, err := app.GetR2Credentials()
	if err != nil {
		t.Fatalf("GetR2Credentials fallÃ³: %v", err)
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
		t.Fatalf("Load state fallÃ³: %v", err)
	}

	// Local debe ser exitoso
	if st.Profile(config.InitialProfileName).LastBackupFile == "" || st.Profile(config.InitialProfileName).SHA256 == "" {
		t.Errorf("backup local debiÃ³ confirmarse en state")
	}

	// R2 exitoso (no pendiente)
	if st.Profile(config.InitialProfileName).IsPending(config.PlatformCloudflare) {
		t.Errorf("PendingSync.R2 debiÃ³ ser false")
	}
	if st.Profile(config.InitialProfileName).LastSyncedFiles[config.PlatformCloudflare] == "" {
		t.Errorf("R2LastSyncedFile debiÃ³ registrarse")
	}

	// Server fallÃ³ transitoriamente (pendiente)
	if !st.Profile(config.InitialProfileName).IsPending(config.PlatformServer) {
		t.Errorf("PendingSync.Server debiÃ³ ser true")
	}
	if st.Profile(config.InitialProfileName).LastSyncedFiles[config.PlatformServer] != "" {
		t.Errorf("ServerLastSyncedFile debiÃ³ permanecer vacÃ­o")
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
		Profiles: map[string]*state.ProfileState{
			config.InitialProfileName: {
				LastRunDate:     "2026-09-10",
				LastBackupFile:  tmpFile,
				SHA256:          "hash123",
				PendingSync:     map[string]bool{config.PlatformServer: true},
				LastSyncedFiles: map[string]string{config.PlatformCloudflare: "CONTABILIDAD_20260910_1000.bak"},
			},
		},
	})

	err := app.Sync(context.Background(), SyncOptions{})
	if err != nil {
		t.Fatalf("Sync fallÃ³: %v", err)
	}

	// R2 no debiÃ³ ser llamado porque ya estaba sincronizado
	if r2Backend.uploadCalls != 0 {
		t.Errorf("R2 no debiÃ³ ser re-subido, llamadas=%d", r2Backend.uploadCalls)
	}

	// Server sÃ­ debiÃ³ ser llamado
	if serverBackend.uploadCalls != 1 {
		t.Errorf("Server debiÃ³ recibir 1 llamada de sync, recibiÃ³=%d", serverBackend.uploadCalls)
	}

	// Tras la sincronizaciÃ³n, Server ya no debe estar pendiente
	st, _ := state.Load(statePath)
	if st.Profile(config.InitialProfileName).IsPending(config.PlatformServer) {
		t.Errorf("PendingSync.Server debiÃ³ ser false tras Sync exitoso")
	}
	if st.Profile(config.InitialProfileName).LastSyncedFiles[config.PlatformServer] != "CONTABILIDAD_20260910_1000.bak" {
		t.Errorf("ServerLastSyncedFile no coincide: %s", st.Profile(config.InitialProfileName).LastSyncedFiles[config.PlatformServer])
	}
}

func TestBackup_PreSync_DeferredRetryBeforeDaily(t *testing.T) {
	serverBackend := &mockBackend{name: "server"}
	app, statePath, _ := setupTestApp(t, &mockSQLEngine{}, []storage.Backend{serverBackend})

	oldBackup := filepath.Join(t.TempDir(), "CONTABILIDAD_20260909_1000.bak")
	_ = os.WriteFile(oldBackup, []byte("backup-ayer"), 0o644)

	_ = state.Save(statePath, &state.State{
		Profiles: map[string]*state.ProfileState{
			config.InitialProfileName: {
				LastRunDate:    "2026-09-09",
				LastBackupFile: oldBackup,
				SHA256:         "hash-ayer",
				PendingSync:    map[string]bool{config.PlatformServer: true},
			},
		},
	})

	// Ejecutar backup del dÃ­a siguiente
	err := app.Backup(context.Background(), BackupOptions{Force: true})
	if err != nil {
		t.Fatalf("Backup fallÃ³: %v", err)
	}

	// Server debiÃ³ recibir 2 llamadas: 1 para el pendiente de ayer + 1 para el de hoy
	if serverBackend.uploadCalls != 2 {
		t.Errorf("esperaba 2 subidas al servidor (pendiente previo + nuevo del dÃ­a), hubo %d", serverBackend.uploadCalls)
	}

	st, _ := state.Load(statePath)
	if st.Profile(config.InitialProfileName).IsPending(config.PlatformServer) {
		t.Errorf("PendingSync.Server debiÃ³ quedar en false")
	}
}

func TestGetTUIStatus_ServerConfiguredAndPending(t *testing.T) {
	serverBackend := &mockBackend{name: "server"}
	app, statePath, _ := setupTestApp(t, &mockSQLEngine{}, []storage.Backend{serverBackend})

	tmpFile := filepath.Join(t.TempDir(), "CONTABILIDAD_20260910_1000.bak")
	_ = os.WriteFile(tmpFile, []byte("ok"), 0o644)

	// Caso: Server configurado y PENDING
	_ = state.Save(statePath, &state.State{
		Profiles: map[string]*state.ProfileState{
			config.InitialProfileName: {
				LastRunDate:    "2026-09-10",
				LastBackupFile: tmpFile,
				PendingSync:    map[string]bool{config.PlatformServer: true},
			},
		},
	})

	bStatus, backends, err := app.GetTUIStatus(context.Background())
	if err != nil {
		t.Fatalf("GetTUIStatus fallÃ³: %v", err)
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
		t.Errorf("Server deberÃ­a figurar como configurado")
	}
	if serverStatus.StatusText != "PENDING" {
		t.Errorf("esperaba status PENDING para Server, dio: %s", serverStatus.StatusText)
	}
	if !serverStatus.PendingSync {
		t.Errorf("PendingSync en ServerStatus deberÃ­a ser true")
	}

	// Caso: Server OK
	_ = state.Save(statePath, &state.State{
		Profiles: map[string]*state.ProfileState{
			config.InitialProfileName: {
				LastRunDate:     "2026-09-10",
				LastBackupFile:  tmpFile,
				LastSyncedFiles: map[string]string{config.PlatformServer: "CONTABILIDAD_20260910_1000.bak"},
			},
		},
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
		t.Errorf("LastSyncOK en ServerStatus deberÃ­a ser true")
	}
}

func TestBackup_EmitsOperationalEvents(t *testing.T) {
	sqlEngine := &mockSQLEngine{}
	r2Backend := &mockBackend{name: "r2"}
	serverBackend := &mockBackend{name: "server"}
	backends := []storage.Backend{r2Backend, serverBackend}

	app, statePath, _ := setupTestApp(t, sqlEngine, backends)
	eventRepo := &mockEventRepo{}
	app.eventRepo = eventRepo
	app.cfg.Supabase.Enabled = true
	app.cfg.Supabase.URL = "http://localhost:54321"

	ctx := context.Background()
	if err := app.Backup(ctx, BackupOptions{Force: true}); err != nil {
		t.Fatalf("Backup fallÃ³: %v", err)
	}

	typesReceived := make(map[string]bool)
	for _, e := range eventRepo.events {
		typesReceived[e.EventType] = true
	}

	expectedTypes := []string{
		events.TypeAgentStarted,
		events.TypeBackupStarted,
		events.TypeLocalBackupCompleted,
		events.TypeLocalRotationCompleted,
		events.TypeR2SyncCompleted,
		events.TypeServerSyncCompleted,
		events.TypeServerRotationCompleted,
		events.TypeBackupCompleted,
		events.TypeAgentFinished,
	}

	for _, exp := range expectedTypes {
		if !typesReceived[exp] {
			t.Errorf("evento esperado no emitido: %s", exp)
		}
	}

	st, err := state.Load(statePath)
	if err != nil {
		t.Fatalf("cargar state: %v", err)
	}
	if len(st.PendingEvents) != 0 {
		t.Errorf("no deberÃ­a haber eventos pendientes tras envÃ­o exitoso, dio: %d", len(st.PendingEvents))
	}
}

func TestBackup_EmitsBackupFailedEvent(t *testing.T) {
	sqlEngine := &mockSQLEngine{backupErr: errors.New("falla simulada en BACKUP DATABASE")}
	app, _, _ := setupTestApp(t, sqlEngine, nil)
	eventRepo := &mockEventRepo{}
	app.eventRepo = eventRepo
	app.cfg.Supabase.Enabled = true
	app.cfg.Supabase.URL = "http://localhost:54321"

	ctx := context.Background()
	err := app.Backup(ctx, BackupOptions{Force: true})
	if err == nil {
		t.Fatal("se esperaba error de backup")
	}

	var foundBackupFailed, foundAgentFinished bool
	for _, e := range eventRepo.events {
		if e.EventType == events.TypeBackupFailed {
			foundBackupFailed = true
			if e.Status != events.StatusFailed {
				t.Errorf("status de backup_failed debe ser FAILED, dio %s", e.Status)
			}
			if !strings.Contains(e.ErrorMessage, "falla simulada") {
				t.Errorf("mensaje de error no contiene texto esperado: %s", e.ErrorMessage)
			}
		}
		if e.EventType == events.TypeAgentFinished {
			foundAgentFinished = true
			if e.Status != events.StatusFailed {
				t.Errorf("status de agent_finished debe ser FAILED, dio %s", e.Status)
			}
		}
	}

	if !foundBackupFailed {
		t.Error("evento backup_failed no fue emitido")
	}
	if !foundAgentFinished {
		t.Error("evento agent_finished no fue emitido")
	}
}

func TestBackup_SupabaseFailureDoesNotFailBackup(t *testing.T) {
	sqlEngine := &mockSQLEngine{}
	app, statePath, _ := setupTestApp(t, sqlEngine, nil)
	// Event repo devuelve error de red recuperable
	eventRepo := &mockEventRepo{err: errors.New("connection refused: 503")}
	app.eventRepo = eventRepo
	app.cfg.Supabase.Enabled = true
	app.cfg.Supabase.URL = "http://localhost:54321"

	ctx := context.Background()
	// El backup debe ser 100% exitoso aunque Supabase estÃ© caÃ­do
	if err := app.Backup(ctx, BackupOptions{Force: true}); err != nil {
		t.Fatalf("el backup no debe fallar si Supabase estÃ¡ caÃ­do: %v", err)
	}

	st, err := state.Load(statePath)
	if err != nil {
		t.Fatalf("cargar state: %v", err)
	}

	// Debe haber acumulado eventos en pending_events
	if len(st.PendingEvents) == 0 {
		t.Error("se esperaba que los eventos fallidos se guardaran en state.PendingEvents")
	}
}

func TestSync_FlushesPendingEvents(t *testing.T) {
	app, statePath, _ := setupTestApp(t, &mockSQLEngine{}, nil)
	eventRepo := &mockEventRepo{}
	app.eventRepo = eventRepo
	app.cfg.Supabase.Enabled = true

	// Guardar estado con eventos pendientes
	initialState := &state.State{
		Profiles: map[string]*state.ProfileState{
			config.InitialProfileName: {LastRunDate: "2026-09-10"},
		},
		PendingEvents: []events.Event{
			{EventID: "evt_p1", EventType: events.TypeBackupCompleted},
			{EventID: "evt_p2", EventType: events.TypeAgentFinished},
		},
	}
	if err := state.Save(statePath, initialState); err != nil {
		t.Fatalf("guardar state inicial: %v", err)
	}

	ctx := context.Background()
	// Sync debe vaciar los eventos pendientes enviÃ¡ndolos a Supabase
	if err := app.Sync(ctx, SyncOptions{Force: false}); err != nil {
		t.Fatalf("Sync fallÃ³ al sincronizar eventos pendientes: %v", err)
	}

	if len(eventRepo.events) != 2 {
		t.Errorf("se esperaban 2 eventos enviados a Supabase, dio: %d", len(eventRepo.events))
	}

	st, err := state.Load(statePath)
	if err != nil {
		t.Fatalf("cargar state: %v", err)
	}
	if len(st.PendingEvents) != 0 {
		t.Errorf("PendingEvents deberÃ­a quedar vacÃ­o tras Sync, tiene %d", len(st.PendingEvents))
	}
}

func TestGetTUIStatus_Supabase(t *testing.T) {
	app, statePath, _ := setupTestApp(t, &mockSQLEngine{}, nil)

	// Caso: Supabase disabled
	app.cfg.Supabase.Enabled = false
	_, backends, _ := app.GetTUIStatus(context.Background())
	var spStatus BackendStatus
	for _, b := range backends {
		if b.Name == "Supabase" {
			spStatus = b
			break
		}
	}
	if spStatus.StatusText != "Disabled" {
		t.Errorf("esperaba status Disabled, dio: %s", spStatus.StatusText)
	}

	// Caso: Supabase enabled y conectado
	app.cfg.Supabase.Enabled = true
	app.eventRepo = &mockEventRepo{}
	_, backends, _ = app.GetTUIStatus(context.Background())
	for _, b := range backends {
		if b.Name == "Supabase" {
			spStatus = b
			break
		}
	}
	if spStatus.StatusText != "Connected" {
		t.Errorf("esperaba status Connected, dio: %s", spStatus.StatusText)
	}

	// Caso: Supabase con eventos pendientes
	_ = state.Save(statePath, &state.State{
		PendingEvents: []events.Event{{EventID: "evt_1"}},
	})
	_, backends, _ = app.GetTUIStatus(context.Background())
	for _, b := range backends {
		if b.Name == "Supabase" {
			spStatus = b
			break
		}
	}
	if spStatus.StatusText != "PENDING" {
		t.Errorf("esperaba status PENDING, dio: %s", spStatus.StatusText)
	}
}

func TestApp_SaveSettings_SupabaseHotReload(t *testing.T) {
	tempDir := t.TempDir()
	cfgPath := filepath.Join(tempDir, "config.json")
	statePath := filepath.Join(tempDir, "state.json")

	cfg := config.Default()
	cfg.Supabase.Enabled = false
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}

	app := New(Options{
		Config:     cfg,
		ConfigPath: cfgPath,
		StatePath:  statePath,
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})

	if app.eventRepo != nil {
		t.Errorf("esperaba eventRepo nil inicialmente")
	}

	// Guardar con Supabase habilitado y API key
	newCfg := cfg
	newCfg.Supabase.Enabled = true
	newCfg.Supabase.URL = "https://example.supabase.co"
	newCfg.Supabase.APIKey = "test_key_123"

	if err := app.SaveSettings(newCfg); err != nil {
		t.Fatalf("SaveSettings falló: %v", err)
	}

	if app.eventRepo == nil {
		t.Errorf("esperaba eventRepo inicializado tras SaveSettings con Supabase habilitado y API key")
	}

	// Guardar con Supabase deshabilitado
	newCfg.Supabase.Enabled = false
	if err := app.SaveSettings(newCfg); err != nil {
		t.Fatalf("SaveSettings falló: %v", err)
	}

	if app.eventRepo != nil {
		t.Errorf("esperaba eventRepo nil tras deshabilitar Supabase")
	}
}















