package testenv

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var (
	ErrProductionServerForbidden   = errors.New("seguridad de testing: prohibido conectar a la instancia de producción")
	ErrProductionDatabaseForbidden = errors.New("seguridad de testing: prohibido apuntar a la base de producción en modo test")
	ErrProductionBackupDirForbidden = errors.New("seguridad de testing: prohibido utilizar el directorio de backups de producción")
	ErrEmptyConfigField            = errors.New("seguridad de testing: campo obligatorio de configuración vacío")
	ErrConflictDatabase            = errors.New("seguridad de testing: TestDatabase no puede ser idéntica a ProductionDatabase")
	ErrConflictBackupPath          = errors.New("seguridad de testing: TestBackupPath no puede ser idéntico a ProductionBackupPath")
)

const (
	DefaultProductionSQLServer  = `Caproba01\vbadilla`
	DefaultProductionDatabase   = "CONTABILIDAD"
	DefaultProductionBackupPath = `C:\Backups\`

	DefaultTestSQLServer  = "localhost"
	DefaultTestDatabase   = "CONTABILIDAD_TEST"
	DefaultTestBackupPath = `C:\BackupsTest\`
)

// TestEnvironmentConfig centraliza y desacopla los parámetros de producción y testing.
type TestEnvironmentConfig struct {
	ProductionSQLServer  string
	ProductionDatabase   string
	ProductionBackupPath string

	TestSQLServer        string
	TestDatabase         string
	TestBackupPath       string

	// Aliases para compatibilidad hacia atrás
	ServerInstance string
	DatabaseName   string
	BackupRoot     string

	DatabaseDriver string
	TestMode       bool
}

// DefaultTestEnvironmentConfig devuelve la configuración con los defaults de FEMUCARIBE.
func DefaultTestEnvironmentConfig() TestEnvironmentConfig {
	return TestEnvironmentConfig{
		ProductionSQLServer:  DefaultProductionSQLServer,
		ProductionDatabase:   DefaultProductionDatabase,
		ProductionBackupPath: DefaultProductionBackupPath,
		TestSQLServer:        DefaultTestSQLServer,
		TestDatabase:         DefaultTestDatabase,
		TestBackupPath:       DefaultTestBackupPath,
		TestMode:             true,
	}
}

// LoadTestEnvironmentConfig carga la configuración desde variables de entorno con fallback a defaults.
func LoadTestEnvironmentConfig() TestEnvironmentConfig {
	cfg := DefaultTestEnvironmentConfig()

	if v := os.Getenv("TEST_PRODUCTION_SQLSERVER"); v != "" {
		cfg.ProductionSQLServer = v
	}
	if v := os.Getenv("TEST_PRODUCTION_DATABASE"); v != "" {
		cfg.ProductionDatabase = v
	}
	if v := os.Getenv("TEST_PRODUCTION_BACKUP_PATH"); v != "" {
		cfg.ProductionBackupPath = v
	}

	if v := os.Getenv("TEST_SQLSERVER"); v != "" {
		cfg.TestSQLServer = v
	} else if v := os.Getenv("TEST_SQLSERVER_HOST"); v != "" {
		cfg.TestSQLServer = v
	}

	if v := os.Getenv("TEST_DATABASE"); v != "" {
		cfg.TestDatabase = v
	} else if v := os.Getenv("TEST_SQLSERVER_DATABASE"); v != "" {
		cfg.TestDatabase = v
	}

	if v := os.Getenv("TEST_BACKUP_PATH"); v != "" {
		cfg.TestBackupPath = v
	} else if v := os.Getenv("TEST_BACKUP_ROOT"); v != "" {
		cfg.TestBackupPath = v
	}

	if v := os.Getenv("TEST_DATABASE_DRIVER"); v != "" {
		cfg.DatabaseDriver = v
	}

	return cfg
}

// ApplyDefaults aplica los defaults o aliases a cualquier campo que no haya sido fijado explícitamente.
func (c *TestEnvironmentConfig) ApplyDefaults() {
	if c.ProductionSQLServer == "" {
		if v := os.Getenv("TEST_PRODUCTION_SQLSERVER"); v != "" {
			c.ProductionSQLServer = v
		} else {
			c.ProductionSQLServer = DefaultProductionSQLServer
		}
	}
	if c.ProductionDatabase == "" {
		if v := os.Getenv("TEST_PRODUCTION_DATABASE"); v != "" {
			c.ProductionDatabase = v
		} else {
			c.ProductionDatabase = DefaultProductionDatabase
		}
	}
	if c.ProductionBackupPath == "" {
		if v := os.Getenv("TEST_PRODUCTION_BACKUP_PATH"); v != "" {
			c.ProductionBackupPath = v
		} else {
			c.ProductionBackupPath = DefaultProductionBackupPath
		}
	}

	// Mapeo de aliases legacy si Test* están vacíos
	if c.TestSQLServer == "" && c.ServerInstance != "" {
		c.TestSQLServer = c.ServerInstance
	}
	if c.TestDatabase == "" && c.DatabaseName != "" {
		c.TestDatabase = c.DatabaseName
	}
	if c.TestBackupPath == "" && c.BackupRoot != "" {
		c.TestBackupPath = c.BackupRoot
	}

	if c.TestSQLServer == "" {
		c.TestSQLServer = DefaultTestSQLServer
	}
	if c.TestDatabase == "" {
		c.TestDatabase = DefaultTestDatabase
	}
	if c.TestBackupPath == "" {
		c.TestBackupPath = DefaultTestBackupPath
	}
}

// Validate comprueba las reglas de aislamiento para asegurar que ningún test apunte a producción.
func (c TestEnvironmentConfig) Validate() error {
	cfg := c
	cfg.ApplyDefaults()

	if strings.TrimSpace(cfg.ProductionSQLServer) == "" {
		return fmt.Errorf("%w: ProductionSQLServer", ErrEmptyConfigField)
	}
	if strings.TrimSpace(cfg.ProductionDatabase) == "" {
		return fmt.Errorf("%w: ProductionDatabase", ErrEmptyConfigField)
	}
	if strings.TrimSpace(cfg.ProductionBackupPath) == "" {
		return fmt.Errorf("%w: ProductionBackupPath", ErrEmptyConfigField)
	}
	if strings.TrimSpace(cfg.TestSQLServer) == "" {
		return fmt.Errorf("%w: TestSQLServer", ErrEmptyConfigField)
	}
	if strings.TrimSpace(cfg.TestDatabase) == "" {
		return fmt.Errorf("%w: TestDatabase", ErrEmptyConfigField)
	}
	if strings.TrimSpace(cfg.TestBackupPath) == "" {
		return fmt.Errorf("%w: TestBackupPath", ErrEmptyConfigField)
	}

	// 1. Validar conflicto entre base de test y base de producción
	testDB := strings.ToLower(strings.TrimSpace(cfg.TestDatabase))
	prodDB := strings.ToLower(strings.TrimSpace(cfg.ProductionDatabase))
	if testDB == prodDB {
		return fmt.Errorf("%w: %s (detectado en test: %s)", ErrConflictDatabase, cfg.ProductionDatabase, cfg.TestDatabase)
	}

	// 2. Validar conflicto entre ruta de backup de test y de producción
	cleanProdPath := filepath.Clean(strings.ToLower(strings.TrimSpace(cfg.ProductionBackupPath)))
	cleanTestPath := filepath.Clean(strings.ToLower(strings.TrimSpace(cfg.TestBackupPath)))
	if cleanTestPath == cleanProdPath {
		return fmt.Errorf("%w: %s (detectado en test: %s)", ErrConflictBackupPath, cfg.ProductionBackupPath, cfg.TestBackupPath)
	}

	if !cfg.TestMode {
		return nil
	}

	// 3. Validar servidor en modo test
	testServer := strings.ToLower(strings.TrimSpace(cfg.TestSQLServer))
	prodServer := strings.ToLower(strings.TrimSpace(cfg.ProductionSQLServer))
	if testServer == prodServer || (prodServer != "" && strings.Contains(testServer, prodServer)) {
		return fmt.Errorf("%w: %s (detectado en test: %s)", ErrProductionServerForbidden, cfg.ProductionSQLServer, cfg.TestSQLServer)
	}

	// 4. Validar que la base de datos de test no sea la de producción
	if testDB == prodDB {
		return fmt.Errorf("%w: %s (detectado en test: %s)", ErrProductionDatabaseForbidden, cfg.ProductionDatabase, cfg.TestDatabase)
	}
	if cfg.DatabaseDriver != "sqlite" && testDB != "" && !strings.Contains(testDB, "test") {
		return fmt.Errorf("seguridad de testing: la base de datos %q debe incluir 'test' en su nombre para ejecución en modo prueba", cfg.TestDatabase)
	}

	// 5. Validar que el directorio de test no apunte a la ruta de producción
	if cleanTestPath == cleanProdPath {
		return fmt.Errorf("%w: %s (detectado en test: %s)", ErrProductionBackupDirForbidden, cfg.ProductionBackupPath, cfg.TestBackupPath)
	}

	return nil
}

// ValidateSafety mantiene compatibilidad hacia atrás delegando en Validate().
func ValidateSafety(cfg TestEnvironmentConfig) error {
	return cfg.Validate()
}
