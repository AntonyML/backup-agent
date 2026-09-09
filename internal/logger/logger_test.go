package logger

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWritesDailyFile(t *testing.T) {
	dir := t.TempDir()
	l := New(dir)
	l.SetOutput(io.Discard)

	l.Info("hola mundo")
	l.Errorf("falló %s con código %d", "algo", 42)

	want := filepath.Join(dir, "agent-"+time.Now().Format("2006-01-02")+".log")
	data, err := os.ReadFile(want)
	if err != nil {
		t.Fatalf("el log diario debería existir en %s: %v", want, err)
	}
	content := string(data)
	if !strings.Contains(content, "[INFO] hola mundo") {
		t.Errorf("falta línea INFO, contenido:\n%s", content)
	}
	if !strings.Contains(content, "[ERROR] falló algo con código 42") {
		t.Errorf("falta línea ERROR con formato, contenido:\n%s", content)
	}
	// Cada WriteString hace append + close: el archivo ya está completo
	// en disco (apto para Get-Content -Wait).
}

func TestMirrorsToOutput(t *testing.T) {
	l := New(t.TempDir())
	var sb strings.Builder
	l.SetOutput(&sb)
	l.Info("espejo")
	if !strings.Contains(sb.String(), "espejo") {
		t.Errorf("debería espejar a out, dio %q", sb.String())
	}
}
