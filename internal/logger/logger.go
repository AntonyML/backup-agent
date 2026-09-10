package logger

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Logger escribe a un archivo rotativo por día:
// <dir>/agent-YYYY-MM-DD.log
//
// Diseño para proceso batch de vida corta: abre el archivo en cada
// escritura (append) y lo cierra enseguida. Eso hace que:
//   - la rotación diaria sea automática (el nombre depende de hoy),
//   - `Get-Content -Wait` (tail -f) en PowerShell vea cada línea
//     al instante (siempre hay flush, nunca queda nada en buffer),
//   - no haya handles abiertos si matan el proceso.
//
// Además espeja cada línea a out (por defecto os.Stdout) para que
// Task Scheduler / consola de soporte vean lo mismo que el archivo.
type Logger struct {
	dir string
	mu  sync.Mutex
	out io.Writer
}

// New crea un logger que escribe en dir. El directorio se crea solo.
func New(dir string) *Logger {
	return &Logger{dir: dir, out: os.Stdout}
}

// SetOutput cambia el espejo (útil en tests con io.Discard).
func (l *Logger) SetOutput(w io.Writer) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.out = w
}

// PathFor devuelve la ruta del log de un día dado (YYYY-MM-DD).
func (l *Logger) PathFor(day string) string {
	return filepath.Join(l.dir, "agent-"+day+".log")
}

func today() string { return time.Now().Format("2006-01-02") }

func timestamp() string { return time.Now().Format("2006-01-02 15:04:05") }

// Info registra una línea informativa.
func (l *Logger) Info(msg string) { l.write("INFO", msg) }

// Warn registra una línea de advertencia.
func (l *Logger) Warn(msg string) { l.write("WARN", msg) }

// Error registra una línea de error.
func (l *Logger) Error(msg string) { l.write("ERROR", msg) }

// Infof registra con formato.
func (l *Logger) Infof(format string, args ...any) { l.write("INFO", fmt.Sprintf(format, args...)) }

// Warnf registra advertencia con formato.
func (l *Logger) Warnf(format string, args ...any) { l.write("WARN", fmt.Sprintf(format, args...)) }

// Errorf registra error con formato.
func (l *Logger) Errorf(format string, args ...any) { l.write("ERROR", fmt.Sprintf(format, args...)) }

func (l *Logger) write(level, msg string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	line := fmt.Sprintf("%s [%s] %s\n", timestamp(), level, msg)

	if l.dir != "" {
		if err := os.MkdirAll(l.dir, 0o755); err == nil {
			f, err := os.OpenFile(l.PathFor(today()), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
			if err == nil {
				_, werr := io.WriteString(f, line)
				cerr := f.Close()
				if werr != nil || cerr != nil {
					fmt.Fprintf(l.outOrStderr(), "logger: no se pudo escribir el log: write=%v close=%v línea=%q\n", werr, cerr, line)
				}
			} else {
				fmt.Fprintf(l.outOrStderr(), "logger: no se pudo abrir el log: %v línea=%q\n", err, line)
			}
		}
	}
	fmt.Fprint(l.outOrStderr(), line)
}

func (l *Logger) outOrStderr() io.Writer {
	if l.out != nil {
		return l.out
	}
	return os.Stderr
}
