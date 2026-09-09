package hasher

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

// Reader calcula el SHA-256 de cualquier stream sin cargarlo
// completo en memoria (importante: los .bak pueden ser de GBs).
// Devuelve el hash en hexadecimal en minúsculas.
func Reader(r io.Reader) (string, error) {
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return "", fmt.Errorf("hasher: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Bytes es un helper para contenido en memoria (tests, payloads chicos).
func Bytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// File calcula el SHA-256 de un archivo en disco con lectura por stream.
// Devuelve error si el archivo no existe o no se puede leer.
func File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("hasher: abrir %s: %w", path, err)
	}
	defer f.Close()
	return Reader(f)
}
