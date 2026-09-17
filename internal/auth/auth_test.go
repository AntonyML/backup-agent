package auth_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"femucaribe-backup-agent/internal/auth"
)

func TestAuthClient_LoginSuccess(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/auth/v1/token" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("grant_type") != "password" {
			t.Fatalf("grant_type esperado password, recibido %s", r.URL.Query().Get("grant_type"))
		}

		var req map[string]string
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req["email"] != "admin@test.com" || req["password"] != "secret123" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"Invalid login credentials"}`))
			return
		}

		resp := auth.Session{
			AccessToken:  "mock-access-token",
			TokenType:    "bearer",
			ExpiresIn:    3600,
			RefreshToken: "mock-refresh-token",
			User: auth.User{
				ID:    "user-123",
				Email: "admin@test.com",
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	client := auth.NewClient(ts.URL, "test-key", ts.Client())
	ctx := context.Background()

	sess, err := client.Login(ctx, "admin@test.com", "secret123")
	if err != nil {
		t.Fatalf("Login falló inesperadamente: %v", err)
	}
	if sess.AccessToken != "mock-access-token" {
		t.Errorf("token inesperado: %s", sess.AccessToken)
	}
	if sess.User.Email != "admin@test.com" {
		t.Errorf("email inesperado: %s", sess.User.Email)
	}
	if !sess.IsValid() {
		t.Errorf("la sesión recién creada debe ser válida")
	}
}

func TestAuthClient_LoginInvalidCredentials(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"Invalid login credentials"}`))
	}))
	defer ts.Close()

	client := auth.NewClient(ts.URL, "test-key", ts.Client())
	ctx := context.Background()

	_, err := client.Login(ctx, "wrong@test.com", "wrongpass")
	if err == nil {
		t.Fatalf("se esperaba error ante credenciales incorrectas")
	}
}

func TestAuthManager_SessionPersistenceAndAutoRefresh(t *testing.T) {
	tempDir := t.TempDir()
	sessFile := filepath.Join(tempDir, "session.json")

	refreshCalled := false

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("grant_type") == "password" {
			resp := auth.Session{
				AccessToken:  "initial-token",
				TokenType:    "bearer",
				ExpiresIn:    1, // 1 segundo para forzar expiración
				RefreshToken: "initial-refresh",
				User: auth.User{
					ID:    "user-123",
					Email: "admin@test.com",
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		if r.URL.Query().Get("grant_type") == "refresh_token" {
			refreshCalled = true
			resp := auth.Session{
				AccessToken:  "refreshed-token",
				TokenType:    "bearer",
				ExpiresIn:    3600,
				RefreshToken: "new-refresh",
				User: auth.User{
					ID:    "user-123",
					Email: "admin@test.com",
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		http.NotFound(w, r)
	}))
	defer ts.Close()

	mgr := auth.NewManager(ts.URL, "test-key", sessFile, ts.Client())
	ctx := context.Background()

	// 1. Inicialmente no autenticado
	if mgr.IsAuthenticated(ctx) {
		t.Fatalf("no debería estar autenticado sin sesión previa")
	}

	// 2. Login inicial
	sess, err := mgr.Login(ctx, "admin@test.com", "pass")
	if err != nil {
		t.Fatalf("Login falló: %v", err)
	}
	if sess.AccessToken != "initial-token" {
		t.Fatalf("token inesperado: %s", sess.AccessToken)
	}

	// 3. Forzar expiración manual de la sesión guardada
	sess.ExpiresAt = time.Now().Add(-10 * time.Minute)
	if err := auth.SaveSession(sessFile, sess); err != nil {
		t.Fatalf("SaveSession falló: %v", err)
	}

	// Crear un nuevo Manager para simular un reinicio de la app
	mgr2 := auth.NewManager(ts.URL, "test-key", sessFile, ts.Client())

	// 4. GetSession debe renovar automáticamente usando refresh_token
	sess2, err := mgr2.GetSession(ctx)
	if err != nil {
		t.Fatalf("GetSession tras expiración debió renovar con éxito: %v", err)
	}
	if !refreshCalled {
		t.Fatalf("el endpoint de refresh no fue invocado")
	}
	if sess2.AccessToken != "refreshed-token" {
		t.Fatalf("esperado token refreshed-token, recibido %s", sess2.AccessToken)
	}
	if mgr2.CurrentUser(ctx) != "admin@test.com" {
		t.Fatalf("CurrentUser no coincide: %s", mgr2.CurrentUser(ctx))
	}

	// 5. Logout
	if err := mgr2.Logout(); err != nil {
		t.Fatalf("Logout falló: %v", err)
	}
	if mgr2.IsAuthenticated(ctx) {
		t.Fatalf("tras Logout no debe estar autenticado")
	}
}
