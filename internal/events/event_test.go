package events

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"femucaribe-backup-agent/internal/config"
	"femucaribe-backup-agent/internal/version"
)

func TestEventSerialization(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	evt := Event{
		EventID:      "evt_123456",
		Timestamp:    now,
		EventType:    TypeBackupCompleted,
		Status:       StatusSuccess,
		Backend:      "local",
		Hostname:     "SRV-BACKUP",
		DatabaseName: "CONTABILIDAD_TEST",
		FileName:     "CONTABILIDAD_TEST_20260910_1200.bak",
		SizeBytes:    1048576,
		DurationMs:   1500,
		ErrorMessage: "",
		AgentVersion: version.Current,
	}

	data, err := json.Marshal(evt)
	if err != nil {
		t.Fatalf("Marshal falló: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal map falló: %v", err)
	}

	expectedKeys := []string{
		"event_id", "timestamp", "event_type", "status", "backend",
		"hostname", "database_name", "filename", "size_bytes",
		"duration_ms", "agent_version",
	}

	for _, k := range expectedKeys {
		if _, ok := m[k]; !ok {
			t.Errorf("clave faltante en JSON serializado: %s", k)
		}
	}

	if m["event_id"] != "evt_123456" {
		t.Errorf("event_id no coincide: %v", m["event_id"])
	}
	if m["status"] != StatusSuccess {
		t.Errorf("status no coincide: %v", m["status"])
	}
}

func TestGenerateID(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		id := GenerateID()
		if !strings.HasPrefix(id, "evt_") {
			t.Fatalf("id no tiene prefijo 'evt_': %s", id)
		}
		if len(id) != 36 { // 'evt_' (4) + 32 hex chars
			t.Fatalf("longitud de id inesperada: %d para %s", len(id), id)
		}
		if seen[id] {
			t.Fatalf("colisión de ID detectada en iteración %d: %s", i, id)
		}
		seen[id] = true
	}
}

func TestNewEvent_Defaults(t *testing.T) {
	evt := NewEvent(TypeBackupStarted, StatusRunning)
	if !strings.HasPrefix(evt.EventID, "evt_") {
		t.Errorf("EventID inválido: %s", evt.EventID)
	}
	if evt.Timestamp.IsZero() {
		t.Error("Timestamp no debería estar vacío")
	}
	if evt.AgentVersion != version.Current {
		t.Errorf("AgentVersion no coincide: %s vs %s", evt.AgentVersion, version.Current)
	}
	if evt.EventType != TypeBackupStarted {
		t.Errorf("EventType incorrecto: %s", evt.EventType)
	}
	if evt.Status != StatusRunning {
		t.Errorf("Status incorrecto: %s", evt.Status)
	}
}

func TestSupabaseRepository_Success(t *testing.T) {
	var receivedBody []byte
	var receivedHeaders http.Header

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		receivedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	cfg := config.SupabaseConfig{
		Enabled:    true,
		URL:        server.URL,
		TimeoutSec: 5,
	}
	repo := NewSupabaseRepository(cfg, "test-api-key", nil)

	evt := NewEvent(TypeBackupCompleted, StatusSuccess)
	evt.DatabaseName = "CONTABILIDAD_TEST"

	ctx := context.Background()
	if err := repo.Append(ctx, evt); err != nil {
		t.Fatalf("Append falló: %v", err)
	}

	if receivedHeaders.Get("apikey") != "test-api-key" {
		t.Errorf("header apikey faltante o incorrecto: %s", receivedHeaders.Get("apikey"))
	}
	if receivedHeaders.Get("Authorization") != "Bearer test-api-key" {
		t.Errorf("header Authorization incorrecto: %s", receivedHeaders.Get("Authorization"))
	}
	if !strings.Contains(receivedHeaders.Get("Prefer"), "resolution=ignore-duplicates") {
		t.Errorf("header Prefer no contiene resolution=ignore-duplicates: %s", receivedHeaders.Get("Prefer"))
	}

	var parsed Event
	if err := json.Unmarshal(receivedBody, &parsed); err != nil {
		t.Fatalf("no se pudo deserializar el body recibido: %v", err)
	}
	if parsed.EventID != evt.EventID {
		t.Errorf("EventID recibido no coincide: %s vs %s", parsed.EventID, evt.EventID)
	}
}

func TestSupabaseRepository_Idempotency(t *testing.T) {
	var callCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		// PostgREST devuelve 201 o 200 con resolution=ignore-duplicates
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	cfg := config.SupabaseConfig{
		Enabled:    true,
		URL:        server.URL,
		TimeoutSec: 5,
	}
	repo := NewSupabaseRepository(cfg, "test-api-key", nil)

	evt := NewEvent(TypeBackupCompleted, StatusSuccess)
	ctx := context.Background()

	// Primera llamada
	if err := repo.Append(ctx, evt); err != nil {
		t.Fatalf("primera llamada falló: %v", err)
	}
	// Reintento idéntico (simulando timeout o replay)
	if err := repo.Append(ctx, evt); err != nil {
		t.Fatalf("reintento idempotente falló: %v", err)
	}

	if atomic.LoadInt32(&callCount) != 2 {
		t.Errorf("se esperaban 2 peticiones recibidas, se registraron: %d", callCount)
	}
}

func TestSupabaseRepository_Errors(t *testing.T) {
	cases := []struct {
		name          string
		statusCode    int
		respBody      string
		expectedErr   error
		isRetryable   bool
	}{
		{
			name:        "HTTP 401 Unauthorized",
			statusCode:  http.StatusUnauthorized,
			respBody:    `{"message":"Invalid API key"}`,
			expectedErr: ErrAuthFailed,
			isRetryable: false,
		},
		{
			name:        "HTTP 403 Forbidden",
			statusCode:  http.StatusForbidden,
			respBody:    `{"message":"RLS violation"}`,
			expectedErr: ErrAuthFailed,
			isRetryable: false,
		},
		{
			name:        "HTTP 429 Rate Limited",
			statusCode:  http.StatusTooManyRequests,
			respBody:    `{"message":"Too many requests"}`,
			expectedErr: ErrRateLimited,
			isRetryable: true,
		},
		{
			name:        "HTTP 400 Bad Request",
			statusCode:  http.StatusBadRequest,
			respBody:    `{"message":"Invalid column"}`,
			expectedErr: ErrSchemaInvalid,
			isRetryable: false,
		},
		{
			name:        "HTTP 422 Unprocessable Entity",
			statusCode:  http.StatusUnprocessableEntity,
			respBody:    `{"message":"Cannot parse payload"}`,
			expectedErr: ErrSchemaInvalid,
			isRetryable: false,
		},
		{
			name:        "HTTP 500 Internal Server Error",
			statusCode:  http.StatusInternalServerError,
			respBody:    `{"message":"Internal error"}`,
			expectedErr: nil,
			isRetryable: true,
		},
		{
			name:        "HTTP 503 Service Unavailable",
			statusCode:  http.StatusServiceUnavailable,
			respBody:    `{"message":"Database restarting"}`,
			expectedErr: nil,
			isRetryable: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(c.statusCode)
				_, _ = w.Write([]byte(c.respBody))
			}))
			defer server.Close()

			cfg := config.SupabaseConfig{
				Enabled:    true,
				URL:        server.URL,
				TimeoutSec: 5,
			}
			repo := NewSupabaseRepository(cfg, "key", nil)
			err := repo.Append(context.Background(), NewEvent(TypeBackupFailed, StatusFailed))

			if err == nil {
				t.Fatalf("se esperaba error para status %d", c.statusCode)
			}
			if c.expectedErr != nil && !errors.Is(err, c.expectedErr) {
				t.Errorf("error esperado %v, recibido %v", c.expectedErr, err)
			}
			if IsRetryable(err) != c.isRetryable {
				t.Errorf("IsRetryable(%v) = %v; esperado %v", err, IsRetryable(err), c.isRetryable)
			}
		})
	}
}

func TestSupabaseRepository_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := config.SupabaseConfig{
		Enabled:    true,
		URL:        server.URL,
		TimeoutSec: 1,
	}
	repo := NewSupabaseRepository(cfg, "key", nil)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	err := repo.Append(ctx, NewEvent(TypeBackupStarted, StatusRunning))
	if err == nil {
		t.Fatal("se esperaba error por timeout de contexto")
	}
	if !IsRetryable(err) {
		t.Errorf("error por timeout debería ser retryable: %v", err)
	}
}

func TestSupabaseRepository_NetworkError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	url := server.URL
	server.Close() // Cerrar inmediatamente para forzar caída de red

	cfg := config.SupabaseConfig{
		Enabled:    true,
		URL:        url,
		TimeoutSec: 1,
	}
	repo := NewSupabaseRepository(cfg, "key", nil)

	err := repo.Append(context.Background(), NewEvent(TypeBackupStarted, StatusRunning))
	if err == nil {
		t.Fatal("se esperaba error de red")
	}
	if !IsRetryable(err) {
		t.Errorf("error de conexión caída debería ser retryable: %v", err)
	}
}

func TestSupabaseRepository_Disabled(t *testing.T) {
	cfg := config.SupabaseConfig{
		Enabled: false,
		URL:     "http://localhost:54321",
	}
	repo := NewSupabaseRepository(cfg, "key", nil)

	err := repo.Append(context.Background(), NewEvent(TypeBackupStarted, StatusRunning))
	if !errors.Is(err, ErrDisabled) {
		t.Errorf("se esperaba ErrDisabled, recibido %v", err)
	}
	if IsRetryable(err) {
		t.Error("ErrDisabled no debe ser retryable")
	}
}

func TestSupabaseRepository_MissingAPIKey(t *testing.T) {
	cfg := config.SupabaseConfig{
		Enabled: true,
		URL:     "http://localhost:54321",
	}
	repo := NewSupabaseRepository(cfg, "", nil)

	err := repo.Append(context.Background(), NewEvent(TypeBackupStarted, StatusRunning))
	if !errors.Is(err, ErrAuthFailed) {
		t.Errorf("se esperaba ErrAuthFailed cuando falta la api_key, recibido %v", err)
	}
}

func TestSupabaseRepository_URLSanitization(t *testing.T) {
	var requestedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	// Probar URL que ya incluye /rest/v1/
	cfg := config.SupabaseConfig{
		Enabled: true,
		URL:     srv.URL + "/rest/v1/",
	}
	repo := NewSupabaseRepository(cfg, "test-key", srv.Client())

	err := repo.Append(context.Background(), NewEvent(TypeBackupStarted, StatusRunning))
	if err != nil {
		t.Fatalf("Append falló: %v", err)
	}
	if requestedPath != "/rest/v1/backup_events" {
		t.Errorf("ruta esperada /rest/v1/backup_events, se obtuvo: %s", requestedPath)
	}
}
