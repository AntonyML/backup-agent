package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// DailyFileWriter es un io.Writer que escribe a un archivo diario
// rotativo: <dir>/agent-YYYY-MM-DD.log y opcionalmente espeja a out.
// Abre en append y cierra en cada escritura para flush inmediato (tail -f / Get-Content -Wait).
type DailyFileWriter struct {
	dir string
	out io.Writer
	mu  sync.Mutex
}

func NewDailyFileWriter(dir string, out io.Writer) *DailyFileWriter {
	return &DailyFileWriter{
		dir: dir,
		out: out,
	}
}

func (w *DailyFileWriter) SetOutput(out io.Writer) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.out = out
}

func (w *DailyFileWriter) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	today := time.Now().Format("2006-01-02")
	if w.dir != "" {
		if err := os.MkdirAll(w.dir, 0o755); err == nil {
			path := filepath.Join(w.dir, "agent-"+today+".log")
			f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
			if err == nil {
				_, _ = f.Write(p)
				_ = f.Close()
			} else {
				fmt.Fprintf(os.Stderr, "logging: error abriendo archivo %s: %v\n", path, err)
			}
		}
	}

	if w.out != nil {
		return w.out.Write(p)
	}
	return len(p), nil
}

// New crea un nuevo *slog.Logger configurado con el DailyFileWriter.
func New(dir string) (*slog.Logger, *DailyFileWriter) {
	w := NewDailyFileWriter(dir, os.Stdout)
	opts := &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}
	handler := slog.NewTextHandler(w, opts)
	return slog.New(handler), w
}
