package lock

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// ErrLocked se devuelve cuando otro proceso vivo tiene el lock.
var ErrLocked = errors.New("lock: otra instancia está corriendo")

// Handle representa un lock adquirido. Debe liberarse con Release.
type Handle struct {
	path string
	pid  int
}

// Acquire intenta tomar el lock en path (archivo con el PID).
// Si no existe, lo crea. Si existe y el PID sigue vivo, devuelve ErrLocked.
// Si el PID ya murió (lock huérfano) o el contenido está corrupto,
// lo reclama sobrescribiéndolo con el PID propio.
func Acquire(path string) (*Handle, error) {
	return AcquireWithCheck(path, pidAlive)
}

// AcquireWithCheck es igual a Acquire pero con la función de
// "PID vivo" inyectable. Existe para tests (no depender de PIDs reales).
func AcquireWithCheck(path string, alive func(int) bool) (*Handle, error) {
	me := os.Getpid()

	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("lock: leer %s: %w", path, err)
		}
		if err := writePid(path, me); err != nil {
			return nil, err
		}
		return &Handle{path: path, pid: me}, nil
	}

	oldPid, perr := parsePid(strings.TrimSpace(string(data)))
	if perr != nil {
		// Lock corrupto (corte de luz a mitad de escritura, edición manual):
		// se reclama como si fuera huérfano en vez de bloquear para siempre.
		if err := writePid(path, me); err != nil {
			return nil, err
		}
		return &Handle{path: path, pid: me}, nil
	}

	if oldPid == me {
		return nil, fmt.Errorf("%w (pid %d ya lo tiene este proceso)", ErrLocked, me)
	}
	if alive(oldPid) {
		return nil, fmt.Errorf("%w (pid %d)", ErrLocked, oldPid)
	}
	// Huérfano: el dueño murió sin liberar (kill/apagón).
	if err := writePid(path, me); err != nil {
		return nil, err
	}
	return &Handle{path: path, pid: me}, nil
}

// Release libera el lock, pero solo si sigue siendo nuestro
// (si otro proceso lo reclamó después, no lo borramos).
// Es idempotente: si el archivo ya no existe, devuelve nil.
func (h *Handle) Release() error {
	if h == nil {
		return fmt.Errorf("lock: handle nil")
	}
	data, err := os.ReadFile(h.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("lock: leer al liberar: %w", err)
	}
	owner, perr := parsePid(strings.TrimSpace(string(data)))
	if perr != nil {
		return fmt.Errorf("lock: contenido corrupto al liberar, no borro %s por seguridad", h.path)
	}
	if owner != h.pid {
		return fmt.Errorf("lock: %s ahora pertenece al pid %d, no al %d: no lo borro", h.path, owner, h.pid)
	}
	if err := os.Remove(h.path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("lock: borrar %s: %w", h.path, err)
	}
	return nil
}

func parsePid(s string) (int, error) {
	pid, err := strconv.Atoi(s)
	if err != nil || pid <= 0 {
		return 0, fmt.Errorf("PID inválido %q", s)
	}
	return pid, nil
}

func writePid(path string, pid int) error {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("lock: crear dir: %w", err)
		}
	}
	if err := os.WriteFile(path, []byte(strconv.Itoa(pid)+"\n"), 0o644); err != nil {
		return fmt.Errorf("lock: escribir %s: %w", path, err)
	}
	return nil
}

// pidAlive reporta si un PID sigue corriendo.
// En Windows usa tasklist (sin dependencias externas); en Unix usa
// señal 0. Ante la duda (error ejecutando el chequeo) asume que el
// proceso está vivo: es más seguro no correr duplicado que no correr.
func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	if pid == os.Getpid() {
		return true
	}
	if runtime.GOOS == "windows" {
		return windowsPidAlive(pid)
	}
	return unixPidAlive(pid)
}

// tasklistTimeout acota la espera del chequeo de PID: si tasklist se cuelga,
// el agente no queda esperándolo indefinidamente dentro de Acquire.
const tasklistTimeout = 5 * time.Second

func windowsPidAlive(pid int) bool {
if pid <= 0 {
return false
}

// tasklist existe en todo Windows moderno. Salida CSV entrecomilla el PID:
//   "sqlservr.exe","1234","Services","0","..."
ctx, cancel := context.WithTimeout(context.Background(), tasklistTimeout)
defer cancel()

cmd := exec.CommandContext(ctx, "tasklist",
"/FI", fmt.Sprintf("PID eq %d", pid),
"/FO", "CSV", "/NH",
)
// Si CommandContext mata el proceso y sus hijos no cierran stdout, WaitDelay
// fuerza el cierre para no dejar goroutines ni handles colgados.
cmd.WaitDelay = 2 * time.Second

var out bytes.Buffer
cmd.Stdout = &out
cmd.Stderr = io.Discard

if err := cmd.Run(); err != nil {
// Conservador: ante error, timeout o cancelación asumir vivo
// (es más seguro no correr duplicado que correr duplicado).
return true
}
return strings.Contains(out.String(), `"`+strconv.Itoa(pid)+`"`)
}
