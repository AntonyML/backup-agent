package supabase_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"femucaribe-backup-agent/internal/storage"
	"femucaribe-backup-agent/internal/storage/supabase"
)

func TestSupabaseBackend_Lifecycle(t *testing.T) {
	var mu sync.Mutex
	objects := make(map[string][]byte)
	buckets := make(map[string]bool)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		switch {
		// GET /storage/v1/bucket/{bucket}
		case r.Method == http.MethodGet && r.URL.Path == "/storage/v1/bucket/backups":
			if buckets["backups"] {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"id":"backups","name":"backups"}`))
			} else {
				w.WriteHeader(http.StatusNotFound)
			}

		// POST /storage/v1/bucket
		case r.Method == http.MethodPost && r.URL.Path == "/storage/v1/bucket":
			buckets["backups"] = true
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"name":"backups"}`))

		// POST /storage/v1/object/{bucket}/{key...}
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/storage/v1/object/backups/CONTABILIDAD/"):
			key := strings.TrimPrefix(r.URL.Path, "/storage/v1/object/backups/")
			objects[key] = []byte("content")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"Key":"` + key + `"}`))

		// POST /storage/v1/object/list/{bucket}
		case r.Method == http.MethodPost && r.URL.Path == "/storage/v1/object/list/backups":
			var items []map[string]any
			for k := range objects {
				items = append(items, map[string]any{
					"name": filepath.Base(k),
					"id":   "id-" + k,
				})
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(items)

		// DELETE /storage/v1/object/{bucket}
		case r.Method == http.MethodDelete && r.URL.Path == "/storage/v1/object/backups":
			var req struct {
				Prefixes []string `json:"prefixes"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			for _, p := range req.Prefixes {
				delete(objects, p)
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[{"name":"deleted"}]`))

		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	client := supabase.NewClient(ts.URL, "test-api-key", 5*time.Second, ts.Client())
	backend := supabase.NewBackend(client, "backups", "CONTABILIDAD")

	if backend.Name() != "supabase" {
		t.Fatalf("backend.Name() = %s, esperado supabase", backend.Name())
	}
	if backend.Bucket() != "backups" {
		t.Fatalf("backend.Bucket() = %s, esperado backups", backend.Bucket())
	}

	tempDir := t.TempDir()
	file1 := filepath.Join(tempDir, "CONTABILIDAD_20260916_1000.bak")
	file2 := filepath.Join(tempDir, "CONTABILIDAD_20260916_1100.bak")
	file3 := filepath.Join(tempDir, "CONTABILIDAD_20260916_1200.bak")

	_ = os.WriteFile(file1, []byte("backup-content-1"), 0o644)
	_ = os.WriteFile(file2, []byte("backup-content-2"), 0o644)
	_ = os.WriteFile(file3, []byte("backup-content-3"), 0o644)

	ctx := context.Background()

	// 1. Upload file1
	if err := backend.Upload(ctx, file1); err != nil {
		t.Fatalf("Upload file1 falló: %v", err)
	}

	// 2. Upload file2
	if err := backend.Upload(ctx, file2); err != nil {
		t.Fatalf("Upload file2 falló: %v", err)
	}

	// 3. Upload file3
	if err := backend.Upload(ctx, file3); err != nil {
		t.Fatalf("Upload file3 falló: %v", err)
	}

	// 4. LatestRemote
	latest, err := backend.LatestRemote(ctx)
	if err != nil {
		t.Fatalf("LatestRemote falló: %v", err)
	}
	if latest != "CONTABILIDAD_20260916_1200.bak" {
		t.Fatalf("LatestRemote = %s, esperado CONTABILIDAD_20260916_1200.bak", latest)
	}

	// 5. Rotate: keep 2 (debe borrar file1)
	if err := backend.Rotate(ctx, 2); err != nil {
		t.Fatalf("Rotate falló: %v", err)
	}

	mu.Lock()
	count := len(objects)
	_, hasFile1 := objects["CONTABILIDAD/CONTABILIDAD_20260916_1000.bak"]
	mu.Unlock()

	if count != 2 {
		t.Fatalf("esperaba 2 objetos tras rotación, hay %d", count)
	}
	if hasFile1 {
		t.Fatalf("file1 debió haber sido eliminado por rotación")
	}
}

func TestSupabaseBackend_RetryableErrors(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"message":"service unavailable"}`))
	}))
	defer ts.Close()

	client := supabase.NewClient(ts.URL, "test-api-key", 2*time.Second, ts.Client())
	backend := supabase.NewBackend(client, "backups", "CONTABILIDAD")

	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test.bak")
	_ = os.WriteFile(testFile, []byte("data"), 0o644)

	err := backend.Upload(context.Background(), testFile)
	if err == nil {
		t.Fatalf("se esperaba error con servidor en 503")
	}

	var rErr *storage.RetryableError
	if ok := err.(*storage.RetryableError); ok == nil {
		t.Fatalf("el error debió ser RetryableError, dio %T: %v", err, err)
	}
	_ = rErr
}
