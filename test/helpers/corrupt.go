package helpers

import (
	"fmt"
	"os"
)

// CorruptFile sobreescribe el inicio del archivo con bytes no válidos para simular corrupción de datos o header.
func CorruptFile(path string) error {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("abrir archivo para corromper: %w", err)
	}
	defer f.Close()

	garbage := []byte("CORRUPTED_GARBAGE_PAYLOAD_INVALIDATING_ALL_DATA_BLOCKS_AND_CHECKSUMS")
	if _, err := f.WriteAt(garbage, 0); err != nil {
		return fmt.Errorf("escribir payload corrupto: %w", err)
	}
	return nil
}

// TruncateFile corta un archivo a un tamaño reducido para simular interrupción abrupta a nivel de filesystem.
func TruncateFile(path string, newSize int64) error {
	return os.Truncate(path, newSize)
}
