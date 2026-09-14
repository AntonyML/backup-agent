package scheduler

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ErrNotInstalled indica que la tarea no existe en el Programador.
var ErrNotInstalled = errors.New("scheduler: la tarea no está instalada")

// ErrPermission es el centinela de falta de permisos.
var ErrPermission = errors.New("scheduler: permisos insuficientes")

// PermissionError señala que schtasks falló por falta de permisos (D7). Lleva
// el comando exacto para que la TUI/CLI lo muestren al operador y pueda
// ejecutarlo a mano con una cuenta administradora, nunca un error crudo.
type PermissionError struct {
	Command string
	Detail  string
}

func (e *PermissionError) Error() string {
	return fmt.Sprintf("scheduler: falta permiso de administrador para gestionar la tarea; ejecutá manualmente: %s", e.Command)
}

// Unwrap permite errors.As/Is sobre la cadena de errores.
func (e *PermissionError) Unwrap() error { return ErrPermission }

// Status describe el estado de una tarea en el Programador.
type Status struct {
	TaskName  string
	Installed bool
	Enabled   bool
	Running   bool
	StateText string // texto crudo informado por schtasks
}

// executor abstrae la invocación de schtasks.exe para poder testear sin Windows.
type executor interface {
	run(ctx context.Context, args ...string) (stdout string, stderr string, err error)
}

// SchtasksExec es el executor productivo: ejecuta schtasks.exe.
type SchtasksExec struct{}

func (SchtasksExec) run(ctx context.Context, args ...string) (string, string, error) {
	cmd := exec.CommandContext(ctx, "schtasks.exe", args...)
	var out, errBuf strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	err := cmd.Run()
	return out.String(), errBuf.String(), err
}

// Manager gestiona la tarea del perfil en el Programador de Windows.
type Manager struct {
	exec executor
	// now es inyectable para tests deterministas del StartBoundary.
	now func() time.Time
}

// New devuelve un Manager productivo sobre schtasks.exe.
func New() *Manager {
	return &Manager{exec: SchtasksExec{}, now: time.Now}
}

// NewWithExecutor permite inyectar un executor (tests).
func NewWithExecutor(exec executor) *Manager {
	return &Manager{exec: exec, now: time.Now}
}

// Install crea la tarea desde el XML del spec. Es idempotente: /F sobrescribe
// una tarea existente, por lo que también sirve como Update.
func (m *Manager) Install(ctx context.Context, spec Spec) error {
	xml, err := TaskXML(spec, m.now())
	if err != nil {
		return err
	}
	xmlPath, cleanup, err := writeTempXML(spec.TaskName, xml)
	if err != nil {
		return err
	}
	defer cleanup()

	_, stderr, err := m.exec.run(ctx, "/Create", "/TN", spec.TaskName, "/XML", xmlPath, "/F")
	if err != nil {
		if isPermissionDenied(stderr + " " + err.Error()) {
			return &PermissionError{Command: CommandLine(spec), Detail: strings.TrimSpace(stderr)}
		}
		return fmt.Errorf("scheduler: crear tarea %q: %w (%s)", spec.TaskName, err, strings.TrimSpace(stderr))
	}
	return nil
}

// Update reinstala la tarea con la definición actual (mismo camino que Install).
func (m *Manager) Update(ctx context.Context, spec Spec) error {
	return m.Install(ctx, spec)
}

// Delete elimina la tarea si existe.
func (m *Manager) Delete(ctx context.Context, taskName string) error {
	_, stderr, err := m.exec.run(ctx, "/Delete", "/TN", taskName, "/F")
	if err != nil {
		if isPermissionDenied(stderr + " " + err.Error()) {
			return &PermissionError{Command: CommandLineDelete(taskName), Detail: strings.TrimSpace(stderr)}
		}
		if isNotFound(stderr) {
			return ErrNotInstalled
		}
		return fmt.Errorf("scheduler: borrar tarea %q: %w (%s)", taskName, err, strings.TrimSpace(stderr))
	}
	return nil
}

// SetEnabled habilita o deshabilita la tarea sin borrarla.
func (m *Manager) SetEnabled(ctx context.Context, taskName string, enabled bool) error {
	flag := "/ENABLE"
	if !enabled {
		flag = "/DISABLE"
	}
	_, stderr, err := m.exec.run(ctx, "/Change", "/TN", taskName, flag)
	if err != nil {
		if isPermissionDenied(stderr + " " + err.Error()) {
			return &PermissionError{
				Command: fmt.Sprintf("schtasks /Change /TN %q %s", taskName, flag),
				Detail:  strings.TrimSpace(stderr),
			}
		}
		if isNotFound(stderr) {
			return ErrNotInstalled
		}
		return fmt.Errorf("scheduler: %s tarea %q: %w (%s)", flag, taskName, err, strings.TrimSpace(stderr))
	}
	return nil
}

// Status consulta si la tarea existe y en qué estado está.
func (m *Manager) Status(ctx context.Context, taskName string) (Status, error) {
	stdout, stderr, err := m.exec.run(ctx, "/Query", "/TN", taskName, "/FO", "LIST", "/V")
	if err != nil {
		if isNotFound(stderr) {
			return Status{TaskName: taskName, Installed: false, StateText: "No instalada"}, nil
		}
		if isPermissionDenied(stderr + " " + err.Error()) {
			return Status{}, &PermissionError{
				Command: fmt.Sprintf("schtasks /Query /TN %q /FO LIST /V", taskName),
				Detail:  strings.TrimSpace(stderr),
			}
		}
		return Status{}, fmt.Errorf("scheduler: consultar tarea %q: %w (%s)", taskName, err, strings.TrimSpace(stderr))
	}
	return parseStatus(taskName, stdout), nil
}

// parseStatus interpreta la salida localizada de "schtasks /Query /FO LIST /V".
func parseStatus(taskName, out string) Status {
	st := Status{TaskName: taskName, Installed: true, Enabled: true}
	for _, line := range strings.Split(out, "\n") {
		key, value, ok := splitStatusLine(line)
		if !ok {
			continue
		}
		switch strings.ToLower(key) {
		case "status", "estado":
			st.StateText = value
			lower := strings.ToLower(value)
			if containsAny(lower, "disabled", "deshabilitad") {
				st.Enabled = false
			}
			if containsAny(lower, "running", "en ejecución", "en ejecucion", "ejecutando") {
				st.Running = true
			}
		}
	}
	if st.StateText == "" {
		st.StateText = "Desconocido"
	}
	return st
}

// splitStatusLine separa "Clave: valor" ignorando líneas vacías.
func splitStatusLine(line string) (string, string, bool) {
	idx := strings.Index(line, ":")
	if idx <= 0 {
		return "", "", false
	}
	key := strings.TrimSpace(line[:idx])
	value := strings.TrimSpace(line[idx+1:])
	if key == "" {
		return "", "", false
	}
	return key, value, true
}

// isPermissionDenied reconoce el fallo por privilegios en Windows localizado.
func isPermissionDenied(s string) bool {
	lower := strings.ToLower(s)
	return containsAny(lower,
		"access is denied", "acceso denegado", "denied",
		"error: 5", "error 5", "0x80070005", "privileg",
	)
}

// isNotFound reconoce la ausencia de la tarea en Windows localizado.
func isNotFound(s string) bool {
	lower := strings.ToLower(s)
	return containsAny(lower,
		"cannot find the file specified", "no se encuentra",
		"no existe", "the system cannot find",
	)
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// writeTempXML escribe el XML en UTF-16LE y devuelve la ruta y el cleanup.
func writeTempXML(taskName, xml string) (string, func(), error) {
	dir, err := os.MkdirTemp("", "femucaribe-task")
	if err != nil {
		return "", nil, fmt.Errorf("scheduler: crear directorio temporal: %w", err)
	}
	safe := strings.NewReplacer(`\`, "-", `/`, "-", `:`, "-", " ", "_").Replace(taskName)
	path := filepath.Join(dir, "task-"+safe+".xml")
	if err := os.WriteFile(path, EncodeUTF16LE(xml), 0o600); err != nil {
		_ = os.RemoveAll(dir)
		return "", nil, fmt.Errorf("scheduler: escribir XML %s: %w", path, err)
	}
	return path, func() { _ = os.RemoveAll(dir) }, nil
}