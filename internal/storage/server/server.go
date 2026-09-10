package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"femucaribe-backup-agent/internal/hasher"
	"femucaribe-backup-agent/internal/rotation"
	"femucaribe-backup-agent/internal/storage"
)

// DefaultKeep es la cantidad predeterminada de copias a retener en el servidor remoto.
const DefaultKeep = 10

// DefaultTimeoutSec es el timeout predeterminado en segundos para transferencias al servidor.
const DefaultTimeoutSec = 300

// Config define los parámetros para el almacenamiento en servidor remoto / recurso compartido.
type Config struct {
	Enabled    bool   `json:"enabled"`
	RemotePath string `json:"remote_path"`
	Keep       int    `json:"keep"`
	TimeoutSec int    `json:"timeout_sec"`
	Database   string `json:"database,omitempty"`
}

var backupPatternRe = regexp.MustCompile(`^([A-Za-z0-9_]+)_(\d{8}_\d{4})\.bak$`)

// DefaultConfig devuelve la configuración por defecto para ServerBackend.
func DefaultConfig() Config {
	return Config{
		Enabled:    false,
		RemotePath: ``,
		Keep:       DefaultKeep,
		TimeoutSec: DefaultTimeoutSec,
		Database:   "",
	}
}

// Validate valida que la configuración sea consistente antes de realizar operaciones de I/O.
func (c Config) Validate() error {
	if !c.Enabled {
		return nil
	}
	if strings.TrimSpace(c.RemotePath) == "" {
		return fmt.Errorf("server: remote_path es obligatorio cuando enabled es true")
	}
	if c.Keep < 1 {
		return fmt.Errorf("server: keep debe ser >= 1, recibido %d", c.Keep)
	}
	if c.TimeoutSec < 0 {
		return fmt.Errorf("server: timeout_sec no puede ser negativo")
	}
	return nil
}

// Backend implementa storage.Backend para copias seguras en servidores remotos o rutas UNC.
type Backend struct {
	cfg    Config
	logger *slog.Logger
}

// New crea un nuevo ServerBackend con dependencias explícitas.
func New(cfg Config, logger *slog.Logger) *Backend {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.Keep < 1 {
		cfg.Keep = DefaultKeep
	}
	if cfg.TimeoutSec <= 0 {
		cfg.TimeoutSec = DefaultTimeoutSec
	}
	return &Backend{
		cfg:    cfg,
		logger: logger,
	}
}

func (b *Backend) Name() string {
	return "server"
}

// Config devuelve una copia de la configuración actual del backend.
func (b *Backend) Config() Config {
	return b.cfg
}

// Upload copia el archivo local de forma atómica y segura al servidor remoto:
// 1. Escribe en <remotePath>/<filename>.tmp
// 2. Valida tamaño y SHA-256 contra el original local
// 3. Renombra atómicamente a <filename>.bak
func (b *Backend) Upload(ctx context.Context, localPath string) error {
	if !b.cfg.Enabled {
		return nil
	}

	if err := b.cfg.Validate(); err != nil {
		return fmt.Errorf("server: configuración inválida: %w", err)
	}

	localFi, err := os.Stat(localPath)
	if err != nil {
		return fmt.Errorf("server: inspeccionar origen local %s: %w", localPath, err)
	}
	if localFi.IsDir() {
		return fmt.Errorf("server: origen local %s es un directorio", localPath)
	}

	var opCtx context.Context
	var cancel context.CancelFunc
	if b.cfg.TimeoutSec > 0 {
		opCtx, cancel = context.WithTimeout(ctx, time.Duration(b.cfg.TimeoutSec)*time.Second)
	} else {
		opCtx, cancel = context.WithCancel(ctx)
	}
	defer cancel()

	// Crear directorio destino si no existe (soporta rutas UNC o locales)
	if err := os.MkdirAll(b.cfg.RemotePath, 0o755); err != nil {
		return storage.NewRetryableError(fmt.Errorf("server: conectar o crear directorio destino %s: %w", b.cfg.RemotePath, err))
	}

	filename := filepath.Base(localPath)
	finalTarget := filepath.Join(b.cfg.RemotePath, filename)
	tmpTarget := finalTarget + ".tmp"

	// Limpieza preventiva de temporales huérfanos previos
	_, _ = b.CleanOrphanTmp(opCtx)

	b.logger.Info("iniciando copia a servidor remoto",
		"backend", b.Name(),
		"origen", localPath,
		"destino_tmp", tmpTarget,
		"tamaño", localFi.Size())

	// Copiar respetando timeout o cancelación
	if err := copyFileWithContext(opCtx, localPath, tmpTarget); err != nil {
		_ = os.Remove(tmpTarget)
		return storage.NewRetryableError(fmt.Errorf("server: transferir a %s: %w", tmpTarget, err))
	}

	// 1. Verificación de tamaño
	remoteFi, err := os.Stat(tmpTarget)
	if err != nil {
		_ = os.Remove(tmpTarget)
		return storage.NewRetryableError(fmt.Errorf("server: verificar archivo temporal en servidor: %w", err))
	}
	if remoteFi.Size() != localFi.Size() {
		_ = os.Remove(tmpTarget)
		return fmt.Errorf("server: discrepancia de tamaño: local=%d bytes, remoto=%d bytes", localFi.Size(), remoteFi.Size())
	}

	// 2. Verificación de hash SHA-256
	localHash, err := hasher.File(localPath)
	if err != nil {
		_ = os.Remove(tmpTarget)
		return fmt.Errorf("server: calcular hash local: %w", err)
	}
	remoteHash, err := hasher.File(tmpTarget)
	if err != nil {
		_ = os.Remove(tmpTarget)
		return storage.NewRetryableError(fmt.Errorf("server: calcular hash remoto: %w", err))
	}
	if localHash != remoteHash {
		_ = os.Remove(tmpTarget)
		return fmt.Errorf("server: integridad fallida: SHA-256 local (%s) no coincide con remoto (%s)", localHash, remoteHash)
	}

	// 3. Renombrado a destino final
	_ = os.Remove(finalTarget)
	if err := os.Rename(tmpTarget, finalTarget); err != nil {
		_ = os.Remove(tmpTarget)
		return storage.NewRetryableError(fmt.Errorf("server: renombrar temporal a destino final: %w", err))
	}

	b.logger.Info("copia a servidor completada y verificada exitosamente",
		"backend", b.Name(),
		"archivo", filename,
		"sha256", remoteHash,
		"tamaño", remoteFi.Size())

	return nil
}

// CleanOrphanTmp elimina archivos temporales (.tmp) huérfanos en el directorio del servidor.
func (b *Backend) CleanOrphanTmp(ctx context.Context) ([]string, error) {
	if !b.cfg.Enabled {
		return nil, nil
	}
	entries, err := os.ReadDir(b.cfg.RemotePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, storage.NewRetryableError(fmt.Errorf("server: leer directorio remoto para limpieza: %w", err))
	}

	var cleaned []string
	var errs []error
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(strings.ToLower(name), ".tmp") {
			fullPath := filepath.Join(b.cfg.RemotePath, name)
			if err := os.Remove(fullPath); err != nil {
				errs = append(errs, fmt.Errorf("limpiar %s: %w", name, err))
			} else {
				cleaned = append(cleaned, name)
			}
		}
	}

	if len(cleaned) > 0 {
		b.logger.Info("archivos temporales huérfanos limpiados en servidor",
			"backend", b.Name(),
			"cantidad", len(cleaned),
			"archivos", cleaned)
	}

	if len(errs) > 0 {
		return cleaned, errors.Join(errs...)
	}
	return cleaned, nil
}

func (b *Backend) isValidBackupFileName(name string) bool {
	lower := strings.ToLower(name)
	if strings.HasSuffix(lower, ".tmp") {
		return false
	}
	match := backupPatternRe.FindStringSubmatch(name)
	if match == nil {
		return false
	}
	dbPart := match[1]
	datePart := match[2]

	if b.cfg.Database != "" && dbPart != b.cfg.Database {
		return false
	}

	if _, err := time.Parse("20060102_1504", datePart); err != nil {
		return false
	}

	return true
}

func (b *Backend) listValidBackups() ([]string, error) {
	entries, err := os.ReadDir(b.cfg.RemotePath)
	if err != nil {
		return nil, err
	}

	var valid []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !b.isValidBackupFileName(name) {
			continue
		}
		info, err := e.Info()
		if err != nil || info.Size() <= 0 {
			continue
		}
		valid = append(valid, name)
	}
	return valid, nil
}

// Rotate ejecuta la rotación de archivos conservando como máximo keep copias válidas.
func (b *Backend) Rotate(ctx context.Context, keep int) error {
	if !b.cfg.Enabled {
		return nil
	}
	if keep < 1 {
		keep = b.cfg.Keep
	}
	if keep < 1 {
		keep = DefaultKeep
	}

	b.logger.Info("iniciando rotación en servidor",
		"backend", b.Name(),
		"keep", keep,
		"directorio", b.cfg.RemotePath)

	validFiles, err := b.listValidBackups()
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		b.logger.Warn("rotación en servidor fallida al listar",
			"backend", b.Name(),
			"error", err)
		return storage.NewRetryableError(fmt.Errorf("server: listar para rotación: %w", err))
	}

	toDelete, err := rotation.Plan(validFiles, keep)
	if err != nil {
		return fmt.Errorf("server: plan de rotación: %w", err)
	}

	var deleted []string
	var delErrs []error
	for _, name := range toDelete {
		full := filepath.Join(b.cfg.RemotePath, name)
		if err := os.Remove(full); err != nil {
			delErrs = append(delErrs, fmt.Errorf("borrar %s: %w", name, err))
		} else {
			deleted = append(deleted, name)
		}
	}

	if len(deleted) > 0 {
		b.logger.Info("rotación en servidor completada",
			"backend", b.Name(),
			"eliminados", len(deleted),
			"archivos", deleted)
	}

	if len(delErrs) > 0 {
		b.logger.Warn("rotación en servidor parcialmente fallida",
			"backend", b.Name(),
			"errores", errors.Join(delErrs...))
		return fmt.Errorf("server: rotación: %w", errors.Join(delErrs...))
	}

	return nil
}

// LatestRemote devuelve el nombre del backup válido más reciente en el servidor.
func (b *Backend) LatestRemote(ctx context.Context) (string, error) {
	if !b.cfg.Enabled {
		return "", nil
	}
	validFiles, err := b.listValidBackups()
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", storage.NewRetryableError(fmt.Errorf("server: listar %s: %w", b.cfg.RemotePath, err))
	}

	if len(validFiles) == 0 {
		return "", nil
	}

	sort.Strings(validFiles)
	return validFiles[len(validFiles)-1], nil
}

func copyFileWithContext(ctx context.Context, srcPath, dstPath string) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := os.OpenFile(dstPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer dst.Close()

	buf := make([]byte, 64*1024)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		nr, rerr := src.Read(buf)
		if nr > 0 {
			nw, werr := dst.Write(buf[:nr])
			if werr != nil {
				return werr
			}
			if nr != nw {
				return io.ErrShortWrite
			}
		}
		if rerr != nil {
			if rerr == io.EOF {
				break
			}
			return rerr
		}
	}

	return dst.Sync()
}
