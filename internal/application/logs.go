package application

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// TailLogs obtiene las últimas n líneas del log del día actual.
func (a *App) TailLogs(ctx context.Context, n int) ([]string, error) {
	if n <= 0 {
		n = 50
	}
	today := time.Now().Format("2006-01-02")
	path := filepath.Join(a.logDir, "agent-"+today+".log")

	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{"(no hay registros de log para el día de hoy)"}, nil
		}
		return nil, fmt.Errorf("abrir archivo de logs %s: %w", path, err)
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("leer archivo de logs: %w", err)
	}

	if len(lines) <= n {
		return lines, nil
	}
	return lines[len(lines)-n:], nil
}
