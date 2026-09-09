package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// State es el estado persistente del agente (state.json junto al binario).
// last_run_date usa formato YYYY-MM-DD para la idempotencia diaria.
type State struct {
	LastRunDate    string `json:"last_run_date"`
	LastBackupFile string `json:"last_backup_file"`
	SHA256         string `json:"sha256"`
}

// dateLayout es el formato canónico de LastRunDate.
const dateLayout = "2006-01-02"

// Today devuelve la fecha de hoy en formato YYYY-MM-DD (hora local,
// que es lo que usa Task Scheduler).
func Today() string {
	return time.Now().Format(dateLayout)
}

// RanOn reporta si el estado corresponde a una corrida del día `date`
// (formato YYYY-MM-DD). Recibe la fecha por parámetro para que sea
// testeable sin depender del reloj.
func (s *State) RanOn(date string) bool {
	if s == nil {
		return false
	}
	return s.LastRunDate != "" && s.LastRunDate == date
}

// Load lee state.json. Si no existe, devuelve un State vacío y nil
// (primer arranque: "nunca se corrió", no es error).
// Si está corrupto, devuelve State vacío + error (el llamador debe
// loguearlo y tratarlo como "nunca se corrió", nunca crashear).
func Load(path string) (*State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &State{}, nil
		}
		return &State{}, fmt.Errorf("state: leer %s: %w", path, err)
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return &State{}, fmt.Errorf("state: %s corrupto: %w", path, err)
	}
	return &s, nil
}

// Save escribe state.json de forma atómica: escribe a un temporal en el
// mismo directorio y luego renombra. En Windows os.Rename no pisa el
// destino, por eso se borra primero el anterior (ventana mínima, proceso
// batch de vida corta con lock file).
func Save(path string, s *State) error {
	if s == nil {
		return fmt.Errorf("state: no se puede guardar un State nil")
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("state: serializar: %w", err)
	}
	data = append(data, '\n')

	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("state: crear dir %s: %w", dir, err)
		}
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("state: escribir temporal: %w", err)
	}
	// os.Rename en Windows falla si el destino existe.
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		_ = os.Remove(tmp)
		return fmt.Errorf("state: reemplazar %s: %w", path, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("state: renombrar a %s: %w", path, err)
	}
	return nil
}
