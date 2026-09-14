package supabase_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"femucaribe-backup-agent/internal/application"
	"femucaribe-backup-agent/internal/config"
	"femucaribe-backup-agent/internal/events"
	"femucaribe-backup-agent/internal/state"
	"femucaribe-backup-agent/internal/storage/local"
	"femucaribe-backup-agent/test/testdb"
	"femucaribe-backup-agent/test/testenv"
	_ "modernc.org/sqlite"
)

// TestSupabase_ResilienceLifecycle valida el ciclo de vida completo de resiliencia:
// 1. EnvÃ­o normal a Supabase.
// 2. CaÃ­da de Supabase -> Backup no falla, evento se guarda en state.PendingEvents.
// 3. RecuperaciÃ³n -> Sync reintenta y envÃ­a eventos pendientes a Supabase.
// 4. Idempotencia -> EnvÃ­o duplicado del mismo EventID no genera error.
func TestSupabase_ResilienceLifecycle(t *testing.T) {
	tempDir := t.TempDir()
	backupDir := filepath.Join(tempDir, "backups")
	dbPath := filepath.Join(tempDir, "source.sqlite")
	statePath := filepath.Join(tempDir, "state.json")
	lockPath := filepath.Join(tempDir, "test.lock")

	_ = os.MkdirAll(backupDir, 0o755)

	if err := testenv.ValidateSafety(testenv.TestEnvironmentConfig{
		DatabaseDriver: "sqlite",
		DatabaseName:   "test_contabilidad",
		ServerInstance: "sqlite_local",
		BackupRoot:     backupDir,
		TestMode:       true,
	}); err != nil {
		t.Fatalf("seguridad: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	sourceDB, err := testdb.NewSQLiteFile(ctx, dbPath)
	if err != nil {
		t.Fatalf("crear SQLite: %v", err)
	}
	defer sourceDB.Close()
	_ = sourceDB.Seed(ctx)

	var serverDown atomic.Bool
	var receivedCount atomic.Int32

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serverDown.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"message": "Service Unavailable"}`))
			return
		}
		// Verificar endpoint de PostgREST
		if r.URL.Path != "/rest/v1/backup_events" {
			http.NotFound(w, r)
			return
		}
		receivedCount.Add(1)
		w.WriteHeader(http.StatusCreated)
	}))
	defer ts.Close()

	spConfig := config.SupabaseConfig{
		Enabled:    true,
		URL:        ts.URL,
		TimeoutSec: 5,
	}

	eventRepo := events.NewSupabaseRepository(spConfig, "test-api-key", ts.Client())

	app := application.New(application.Options{
		Config: config.Config{
			BackupDir:        backupDir,
			Server:           "sqlite_local",
			Database:         "test_contabilidad",
			Retain:           3,
			LoginTimeoutSec:  5,
			BackupTimeoutSec: 10,
			Supabase:         spConfig,
		},
		SQLEngine:    testdb.NewSQLiteSQLEngine(dbPath),
		LocalBackend: local.New(backupDir),
		StatePath:    statePath,
		LockPath:     lockPath,
		EventRepo:    eventRepo,
	})

	// Paso 1: EjecuciÃ³n con Supabase ONLINE -> Todo OK
	t.Run("Paso1_SupabaseOnline_BackupExitoso", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		err := app.Backup(ctx, application.BackupOptions{Force: true})
		if err != nil {
			t.Fatalf("Backup fallÃ³ con Supabase online: %v", err)
		}
		st, err := state.Load(statePath)
		if err != nil {
			t.Fatalf("cargar state: %v", err)
		}
		if len(st.PendingEvents) != 0 {
			t.Errorf("no deberÃ­a haber eventos pendientes con Supabase online, dio %d", len(st.PendingEvents))
		}
		if receivedCount.Load() == 0 {
			t.Errorf("servidor debiÃ³ haber recibido eventos")
		}
	})

	// Paso 2: CaÃ­da de Supabase -> Backup DEBE CONTINUAR y guardar en state.PendingEvents
	t.Run("Paso2_CaidaSupabase_BackupContinuaYGuardaPendientes", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		serverDown.Store(true)

		// Limpiar estado de ejecuciÃ³n previa para permitir nueva corrida
		st, _ := state.Load(statePath)
		st.Profile(config.InitialProfileName).LastRunDate = "2026-01-01"
		_ = state.Save(statePath, st)

		err := app.Backup(ctx, application.BackupOptions{Force: true})
		if err != nil {
			t.Fatalf("el backup NO debe fallar cuando Supabase estÃ¡ caÃ­do: %v", err)
		}

		st, err = state.Load(statePath)
		if err != nil {
			t.Fatalf("cargar state: %v", err)
		}
		if len(st.PendingEvents) == 0 {
			t.Fatalf("se esperaban eventos acumulados en PendingEvents tras caÃ­da de Supabase")
		}
		t.Logf("Eventos pendientes acumulados en state: %d", len(st.PendingEvents))
	})

	// Paso 3: RecuperaciÃ³n -> Sync vacÃ­a PendingEvents hacia Supabase
	t.Run("Paso3_Recuperacion_SyncVaciaPendientes", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		serverDown.Store(false)
		prevCount := receivedCount.Load()

		err := app.Sync(ctx, application.SyncOptions{Force: false})
		if err != nil {
			t.Fatalf("Sync fallÃ³ tras recuperaciÃ³n de Supabase: %v", err)
		}

		st, err := state.Load(statePath)
		if err != nil {
			t.Fatalf("cargar state: %v", err)
		}
		if len(st.PendingEvents) != 0 {
			t.Errorf("PendingEvents deberÃ­a quedar vacÃ­o tras Sync, tiene %d", len(st.PendingEvents))
		}
		if receivedCount.Load() <= prevCount {
			t.Errorf("Sync debiÃ³ enviar los eventos pendientes al servidor")
		}
	})

	// Paso 4: Probar Idempotencia / Duplicados con PostgREST
	t.Run("Paso4_Idempotencia_DuplicadosIgnorados", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// Enviar el mismo evento dos veces directamente a travÃ©s del repository
		evt := events.NewEvent(events.TypeBackupCompleted, events.StatusSuccess)
		evt.EventID = "evt_test_idempotency_123"

		// Primer envÃ­o
		if err := eventRepo.Append(ctx, evt); err != nil {
			t.Fatalf("primer envÃ­o de evento fallÃ³: %v", err)
		}

		// Segundo envÃ­o idÃ©ntico (simulando reintento por network glitch)
		if err := eventRepo.Append(ctx, evt); err != nil {
			t.Fatalf("segundo envÃ­o idÃ©ntico no debe fallar (idempotencia): %v", err)
		}
	})
}

// TestSupabase_LiveRemoteProject prueba la integraciÃ³n real con Supabase en la nube
// (oxpxyiucnzpedawwkosy) con la anon key.
func TestSupabase_LiveRemoteProject(t *testing.T) {
	if os.Getenv("SUPABASE_LIVE_TEST") != "true" {
		t.Skip("omitiendo test remoto en vivo; definir SUPABASE_LIVE_TEST=true para ejecutar contra la nube")
	}

	anonKey := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJzdXBhYmFzZSIsInJlZiI6Im94cHh5aXVjbnpwZWRhd3drb3N5Iiwicm9sZSI6ImFub24iLCJpYXQiOjE3ODkwNTkwODAsImV4cCI6MjEwNDYzNTA4MH0.ryk8Ir6KKevqLFGIgSgm2ISb7tYLL3xzHzmy-58RBeI"
	remoteURL := "https://oxpxyiucnzpedawwkosy.supabase.co"

	repo := events.NewSupabaseRepository(config.SupabaseConfig{
		Enabled:    true,
		URL:        remoteURL,
		TimeoutSec: 10,
	}, anonKey, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 1. Insertar evento de prueba
	testEventID := fmt.Sprintf("evt_live_test_%d", time.Now().UnixNano())
	evt := events.NewEvent(events.TypeAgentStarted, events.StatusRunning)
	evt.EventID = testEventID
	evt.DatabaseName = "TEST_INTEGRATION"
	evt.ErrorMessage = ""

	err := repo.Append(ctx, evt)
	if err != nil {
		t.Fatalf("fallÃ³ la inserciÃ³n en Supabase remoto: %v", err)
	}

	// 2. Probar duplicado (idempotencia con Prefer: resolution=ignore-duplicates)
	err = repo.Append(ctx, evt)
	if err != nil {
		t.Fatalf("fallÃ³ el manejo de duplicado en Supabase remoto: %v", err)
	}

	t.Logf("Evento remoto insertado y verificado con idempotencia: %s", testEventID)
}

