package testenv

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

var (
	ErrProductionServerForbidden   = errors.New("seguridad de testing: prohibido conectar a la instancia de producción 'Caproba01\\vbadilla'")
	ErrProductionDatabaseForbidden = errors.New("seguridad de testing: prohibido apuntar a la base de producción 'CONTABILIDAD' en modo test")
	ErrProductionBackupDirForbidden = errors.New("seguridad de testing: prohibido utilizar el directorio de backups de producción 'C:\\Backups'")
)

const (
	ForbiddenProductionServer   = "caproba01\\vbadilla"
	ForbiddenProductionDatabase = "contabilidad"
	ForbiddenProductionBackupDir = "c:\\backups"
)

// TestEnvironmentConfig parametriza el entorno de pruebas para validar su aislamiento.
type TestEnvironmentConfig struct {
	DatabaseDriver string
	DatabaseName   string
	ServerInstance string
	BackupRoot     string
	TestMode       bool
}

// ValidateSafety rechaza categóricamente cualquier intento de conectar a recursos de producción en modo test.
func ValidateSafety(cfg TestEnvironmentConfig) error {
	if !cfg.TestMode {
		return nil
	}

	// 1. Validar servidor
	serverLower := strings.ToLower(strings.TrimSpace(cfg.ServerInstance))
	if serverLower == ForbiddenProductionServer || strings.Contains(serverLower, "caproba01") {
		return fmt.Errorf("%w (detectado: %s)", ErrProductionServerForbidden, cfg.ServerInstance)
	}

	// 2. Validar base de datos
	dbLower := strings.ToLower(strings.TrimSpace(cfg.DatabaseName))
	if dbLower == ForbiddenProductionDatabase {
		return fmt.Errorf("%w (detectado: %s)", ErrProductionDatabaseForbidden, cfg.DatabaseName)
	}
	if cfg.DatabaseDriver != "sqlite" && dbLower != "" && !strings.Contains(dbLower, "test") {
		return fmt.Errorf("seguridad de testing: la base de datos %q debe incluir 'test' en su nombre para ejecución en modo prueba", cfg.DatabaseName)
	}

	// 3. Validar directorio de backup
	if cfg.BackupRoot != "" {
		cleaned := filepath.Clean(strings.ToLower(strings.TrimSpace(cfg.BackupRoot)))
		if cleaned == ForbiddenProductionBackupDir || strings.EqualFold(cleaned, `c:\backups`) || strings.EqualFold(cleaned, `c:/backups`) {
			return fmt.Errorf("%w (detectado: %s)", ErrProductionBackupDirForbidden, cfg.BackupRoot)
		}
	}

	return nil
}
