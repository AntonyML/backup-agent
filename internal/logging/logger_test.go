package logging

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDailyFileWriter_WritesToFileAndOutput(t *testing.T) {
	dir := t.TempDir()
	var buf bytes.Buffer
	logger, dfw := New(dir)
	dfw.SetOutput(&buf)

	logger.Info("prueba estructurada", "backend", "r2", "duracion_ms", 123)

	// Verificar salida en memoria
	outStr := buf.String()
	if !strings.Contains(outStr, "prueba estructurada") || !strings.Contains(outStr, "backend=r2") {
		t.Errorf("salida en memoria inesperada: %s", outStr)
	}

	// Verificar archivo rotativo del día
	today := time.Now().Format("2006-01-02")
	logFile := filepath.Join(dir, "agent-"+today+".log")
	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("no se pudo leer el archivo de log %s: %v", logFile, err)
	}
	fileStr := string(data)
	if !strings.Contains(fileStr, "prueba estructurada") || !strings.Contains(fileStr, "backend=r2") {
		t.Errorf("contenido del archivo de log inesperado: %s", fileStr)
	}
}
