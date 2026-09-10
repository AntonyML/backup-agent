package local

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"femucaribe-backup-agent/internal/rotation"
)

// Backend implementa storage.Backend para almacenamiento en el filesystem local.
type Backend struct {
	backupDir string
}

func New(backupDir string) *Backend {
	return &Backend{
		backupDir: backupDir,
	}
}

func (b *Backend) Name() string {
	return "local"
}

// Upload asegura que el archivo esté en backupDir. Si localPath ya está en backupDir,
// valida que exista. Si está en otra ruta, lo copia atómicamente a backupDir.
func (b *Backend) Upload(ctx context.Context, localPath string) error {
	fi, err := os.Stat(localPath)
	if err != nil {
		return fmt.Errorf("local: inspeccionar %s: %w", localPath, err)
	}
	if fi.IsDir() {
		return fmt.Errorf("local: %s es un directorio", localPath)
	}

	target := filepath.Join(b.backupDir, filepath.Base(localPath))
	if filepath.Clean(localPath) == filepath.Clean(target) {
		return nil
	}

	if err := os.MkdirAll(b.backupDir, 0o755); err != nil {
		return fmt.Errorf("local: crear directorio %s: %w", b.backupDir, err)
	}

	src, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("local: abrir origen %s: %w", localPath, err)
	}
	defer src.Close()

	tmpTarget := target + ".tmp"
	dst, err := os.OpenFile(tmpTarget, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("local: crear destino temporal %s: %w", tmpTarget, err)
	}

	if _, err := io.Copy(dst, src); err != nil {
		_ = dst.Close()
		_ = os.Remove(tmpTarget)
		return fmt.Errorf("local: copiar contenido: %w", err)
	}
	if err := dst.Close(); err != nil {
		_ = os.Remove(tmpTarget)
		return fmt.Errorf("local: cerrar destino %s: %w", tmpTarget, err)
	}

	_ = os.Remove(target)
	if err := os.Rename(tmpTarget, target); err != nil {
		_ = os.Remove(tmpTarget)
		return fmt.Errorf("local: renombrar a destino final %s: %w", target, err)
	}

	return nil
}

// Rotate ejecuta la rotación sobre backupDir conservando keep copias.
func (b *Backend) Rotate(ctx context.Context, keep int) error {
	_, err := rotation.Rotate(b.backupDir, keep)
	if err != nil {
		return fmt.Errorf("local: rotación: %w", err)
	}
	return nil
}

// LatestRemote devuelve el nombre del backup más reciente en backupDir.
func (b *Backend) LatestRemote(ctx context.Context) (string, error) {
	entries, err := os.ReadDir(b.backupDir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("local: listar %s: %w", b.backupDir, err)
	}

	var baks []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(strings.ToLower(name), ".bak") && !strings.HasSuffix(strings.ToLower(name), ".tmp") {
			baks = append(baks, name)
		}
	}

	if len(baks) == 0 {
		return "", nil
	}

	// Orden lexicográfico cronológico (CONTABILIDAD_YYYYMMDD_HHMM.bak)
	sort.Strings(baks)
	return baks[len(baks)-1], nil
}
