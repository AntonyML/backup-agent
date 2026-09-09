package lock

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func aliveYes(int) bool { return true }

func aliveNo(int) bool { return false }

func readPid(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatalf("lock file con PID inválido: %q", data)
	}
	return pid
}

func TestAcquireRelease_HappyPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.lock")

	h, err := AcquireWithCheck(path, aliveYes)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if readPid(t, path) != os.Getpid() {
		t.Error("el lock file debería contener nuestro PID")
	}
	if err := h.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("Release debería borrar el lock file")
	}
	// Release idempotente.
	if err := h.Release(); err != nil {
		t.Errorf("doble Release debería ser nil: %v", err)
	}
}

func TestAcquire_LegitLock_ErrLocked(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.lock")
	if err := os.WriteFile(path, []byte("999999\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := AcquireWithCheck(path, aliveYes); err == nil {
		t.Fatal("con PID vivo debería devolver ErrLocked")
	} else if !isLocked(err) {
		t.Fatalf("error debería envolver ErrLocked, dio: %v", err)
	}
	// No debe haber pisado el lock ajeno.
	if readPid(t, path) != 999999 {
		t.Error("un lock legítimo no debe ser sobrescrito")
	}
}

func TestAcquire_OrphanLock_Reclaimed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.lock")
	if err := os.WriteFile(path, []byte("999998\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h, err := AcquireWithCheck(path, aliveNo)
	if err != nil {
		t.Fatalf("lock huérfano debería reclamarse: %v", err)
	}
	if readPid(t, path) != os.Getpid() {
		t.Error("al reclamar, el lock debería contener nuestro PID")
	}
	if err := h.Release(); err != nil {
		t.Fatalf("Release tras reclamar: %v", err)
	}
}

func TestAcquire_CorruptLock_Reclaimed(t *testing.T) {
	for _, bad := range []string{"", "no-un-pid\n", "0\n", "-5\n", "12.5\n"} {
		path := filepath.Join(t.TempDir(), "agent.lock")
		if err := os.WriteFile(path, []byte(bad), 0o644); err != nil {
			t.Fatal(err)
		}
		h, err := AcquireWithCheck(path, aliveYes)
		if err != nil {
			t.Errorf("lock corrupto %q debería reclamarse, dio: %v", bad, err)
			continue
		}
		if readPid(t, path) != os.Getpid() {
			t.Errorf("lock corrupto %q: debería contener nuestro PID", bad)
		}
		_ = h.Release()
	}
}

func TestRelease_StolenLock_NotDeleted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.lock")
	h, err := AcquireWithCheck(path, aliveYes)
	if err != nil {
		t.Fatal(err)
	}
	// Otro proceso reclama por encima (simulado).
	if err := os.WriteFile(path, []byte("777777\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := h.Release(); err == nil {
		t.Error("Release de un lock robado debería dar error")
	}
	if readPid(t, path) != 777777 {
		t.Error("Release no debe borrar un lock que ya es de otro")
	}
}

func isLocked(err error) bool {
	return errors.Is(err, ErrLocked)
}
