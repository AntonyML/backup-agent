package rotation

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Plan decide qué archivos borrar para quedarse con las `keep` copias
// más recientes. Es pura (no toca disco) para que sea fácil de testear.
//
// Solo considera archivos .bak finales. Los .tmp huérfanos, state.json,
// logs, etc. se ignoran acá: la limpieza de .tmp es responsabilidad del
// arranque del agente, no de la rotación.
//
// El orden cronológico se asume lexicográfico, válido porque el formato
// es CONTABILIDAD_YYYYMMDD_HHMM.bak (fecha de ancho fijo, ordenable).
func Plan(files []string, keep int) ([]string, error) {
	if keep < 1 {
		return nil, fmt.Errorf("rotation: keep debe ser >= 1, recibí %d", keep)
	}
	baks := make([]string, 0, len(files))
	for _, f := range files {
		if isBackupFile(f) {
			baks = append(baks, f)
		}
	}
	sort.Strings(baks)
	if len(baks) <= keep {
		return nil, nil
	}
	toDelete := make([]string, len(baks)-keep)
	copy(toDelete, baks[:len(baks)-keep])
	return toDelete, nil
}

// List devuelve los .bak finales en dir, ordenados de más viejo a más nuevo
// (orden lexicográfico del nombre). No es recursivo.
func List(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("rotation: leer %s: %w", dir, err)
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if isBackupFile(e.Name()) {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}

// Rotate lista los .bak de dir y borra físicamente los más viejos,
// quedándose con las `keep` copias más recientes.
// Devuelve la lista de archivos borrados (solo nombres base).
// Si hay menos o igual que `keep`, no borra nada y devuelve nil.
func Rotate(dir string, keep int) (deleted []string, err error) {
	files, err := List(dir)
	if err != nil {
		return nil, err
	}
	toDelete, err := Plan(files, keep)
	if err != nil {
		return nil, err
	}
	for _, name := range toDelete {
		full := filepath.Join(dir, name)
		if rerr := os.Remove(full); rerr != nil {
			return deleted, fmt.Errorf("rotation: borrar %s: %w", full, rerr)
		}
		deleted = append(deleted, name)
	}
	return deleted, nil
}

func isBackupFile(name string) bool {
	lower := strings.ToLower(name)
	// .bak.tmp termina en .tmp -> excluido automáticamente.
	return strings.HasSuffix(lower, ".bak")
}
