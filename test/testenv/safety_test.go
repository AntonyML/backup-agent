package testenv

import (
	"errors"
	"os"
	"testing"
)

// 1. Usar todos los defaults
func TestConfig_AllDefaults(t *testing.T) {
	cfg := DefaultTestEnvironmentConfig()

	if cfg.ProductionSQLServer != DefaultProductionSQLServer {
		t.Errorf("ProductionSQLServer: esperado=%s, obtenido=%s", DefaultProductionSQLServer, cfg.ProductionSQLServer)
	}
	if cfg.ProductionDatabase != DefaultProductionDatabase {
		t.Errorf("ProductionDatabase: esperado=%s, obtenido=%s", DefaultProductionDatabase, cfg.ProductionDatabase)
	}
	if cfg.ProductionBackupPath != DefaultProductionBackupPath {
		t.Errorf("ProductionBackupPath: esperado=%s, obtenido=%s", DefaultProductionBackupPath, cfg.ProductionBackupPath)
	}
	if cfg.TestSQLServer != DefaultTestSQLServer {
		t.Errorf("TestSQLServer: esperado=%s, obtenido=%s", DefaultTestSQLServer, cfg.TestSQLServer)
	}
	if cfg.TestDatabase != DefaultTestDatabase {
		t.Errorf("TestDatabase: esperado=%s, obtenido=%s", DefaultTestDatabase, cfg.TestDatabase)
	}
	if cfg.TestBackupPath != DefaultTestBackupPath {
		t.Errorf("TestBackupPath: esperado=%s, obtenido=%s", DefaultTestBackupPath, cfg.TestBackupPath)
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("configuración por defecto debería ser válida: %v", err)
	}
}

// 2. Sobrescribir solamente ProductionSQLServer
func TestConfig_OverrideProductionSQLServer(t *testing.T) {
	cfg := DefaultTestEnvironmentConfig()
	cfg.ProductionSQLServer = `CustomServer\InstanceProd`

	// Test targeting DefaultProductionSQLServer should now be allowed since production was overridden
	cfg.TestSQLServer = DefaultProductionSQLServer
	if err := cfg.Validate(); err != nil {
		t.Errorf("debería permitir test server %s cuando la producción configurada es otra: %v", DefaultProductionSQLServer, err)
	}

	// Test targeting the newly overridden production server should be rejected
	cfg.TestSQLServer = `CustomServer\InstanceProd`
	err := cfg.Validate()
	if err == nil || !errors.Is(err, ErrProductionServerForbidden) {
		t.Fatalf("debió rechazar servidor de producción personalizado: %v", err)
	}
}

// 3. Sobrescribir solamente ProductionDatabase
func TestConfig_OverrideProductionDatabase(t *testing.T) {
	cfg := DefaultTestEnvironmentConfig()
	cfg.ProductionDatabase = "ERP_PRODUCCION"

	// CONTABILIDAD (anterior prod) ahora es tratada como no conflictiva con ERP_PRODUCCION
	// (salvo regla de 'test' en nombre de base para SQL Server)
	cfg.DatabaseDriver = "sqlite"
	cfg.TestDatabase = "CONTABILIDAD"
	if err := cfg.Validate(); err != nil {
		t.Errorf("debería permitir base cuando la producción configurada cambió: %v", err)
	}

	// Apuntar a la nueva base de producción debe ser rechazado
	cfg.TestDatabase = "ERP_PRODUCCION"
	err := cfg.Validate()
	if err == nil || !errors.Is(err, ErrConflictDatabase) {
		t.Fatalf("debió rechazar base de producción personalizada: %v", err)
	}
}

// 4. Sobrescribir rutas de producción y test
func TestConfig_OverridePaths(t *testing.T) {
	cfg := DefaultTestEnvironmentConfig()
	cfg.ProductionBackupPath = `D:\Produccion\Backups\`
	cfg.TestBackupPath = `D:\Testing\Backups\`

	if err := cfg.Validate(); err != nil {
		t.Fatalf("rutas separadas deben ser válidas: %v", err)
	}

	// Si se intenta usar la ruta de producción configurada en test, debe rechazarse
	cfg.TestBackupPath = `D:\Produccion\Backups`
	err := cfg.Validate()
	if err == nil || !errors.Is(err, ErrConflictBackupPath) {
		t.Fatalf("debió rechazar ruta idéntica a la de producción personalizada: %v", err)
	}
}

// 5. Configurar una PC o laboratorio diferente
func TestConfig_DifferentPC(t *testing.T) {
	cfg := TestEnvironmentConfig{
		ProductionSQLServer:  `PC-CONTABILIDAD-01\SQL2019`,
		ProductionDatabase:   "FINANZAS_REAL",
		ProductionBackupPath: `E:\BackupsEmpresa\`,

		TestSQLServer:  "192.168.1.50:1433",
		TestDatabase:   "FINANZAS_TEST",
		TestBackupPath: `C:\LabTest\Backups\`,
		DatabaseDriver: "sqlserver",
		TestMode:       true,
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("configuración de PC alternativa debería ser válida: %v", err)
	}
}

// 6. Rechazar test apuntando a producción (todos los casos)
func TestConfig_RejectProductionWhenInTestMode(t *testing.T) {
	t.Run("servidor igual", func(t *testing.T) {
		cfg := DefaultTestEnvironmentConfig()
		cfg.TestSQLServer = cfg.ProductionSQLServer
		err := cfg.Validate()
		if err == nil || !errors.Is(err, ErrProductionServerForbidden) {
			t.Errorf("esperaba ErrProductionServerForbidden, dio: %v", err)
		}
	})

	t.Run("base igual", func(t *testing.T) {
		cfg := DefaultTestEnvironmentConfig()
		cfg.TestDatabase = cfg.ProductionDatabase
		err := cfg.Validate()
		if err == nil || !errors.Is(err, ErrConflictDatabase) {
			t.Errorf("esperaba ErrConflictDatabase, dio: %v", err)
		}
	})

	t.Run("ruta de backup igual", func(t *testing.T) {
		cfg := DefaultTestEnvironmentConfig()
		cfg.TestBackupPath = cfg.ProductionBackupPath
		err := cfg.Validate()
		if err == nil || !errors.Is(err, ErrConflictBackupPath) {
			t.Errorf("esperaba ErrConflictBackupPath, dio: %v", err)
		}
	})
}

// 7. Aceptar una configuración de laboratorio válida
func TestConfig_AcceptValidLab(t *testing.T) {
	cfg := TestEnvironmentConfig{
		ProductionSQLServer:  `ProdServer\Instance`,
		ProductionDatabase:   "MAIN_DB",
		ProductionBackupPath: `P:\Prod\Backups\`,

		TestSQLServer:  "localhost:14333",
		TestDatabase:   "MAIN_DB_TEST",
		TestBackupPath: `T:\Test\Backups\`,
		DatabaseDriver: "sqlserver",
		TestMode:       true,
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("configuración de laboratorio válida rechazada: %v", err)
	}
}

// 8. Verificar que C:\BackupsTest\ no sea confundido con C:\Backups\
func TestConfig_BackupsTestNotConfusedWithBackups(t *testing.T) {
	cfg := DefaultTestEnvironmentConfig()
	cfg.ProductionBackupPath = `C:\Backups\`
	cfg.TestBackupPath = `C:\BackupsTest\`

	if err := cfg.Validate(); err != nil {
		t.Fatalf("C:\\BackupsTest\\ no debe ser confundido con C:\\Backups\\: %v", err)
	}

	// Y probando con variaciones de casing y separadores
	cfg.ProductionBackupPath = `c:/backups/`
	cfg.TestBackupPath = `C:\backupstest`
	if err := cfg.Validate(); err != nil {
		t.Fatalf("rutas normalizadas no deben causar falso positivo: %v", err)
	}
}

// 9. Verificar que la configuración no dependa de valores hardcodeados fuera del sistema de defaults
func TestConfig_NoHardcodedValuesOutsideDefaults(t *testing.T) {
	// Limpiamos variables de entorno temporales si existieran
	_ = os.Unsetenv("TEST_PRODUCTION_SQLSERVER")
	_ = os.Unsetenv("TEST_PRODUCTION_DATABASE")
	_ = os.Unsetenv("TEST_PRODUCTION_BACKUP_PATH")

	customProdServer := "MyCompanyServer\\SQL"
	customProdDB := "COMPANY_DB"
	customProdPath := `X:\BackupsCompany\`

	cfg := TestEnvironmentConfig{
		ProductionSQLServer:  customProdServer,
		ProductionDatabase:   customProdDB,
		ProductionBackupPath: customProdPath,

		// En test usamos los defaults de FEMUCARIBE
		TestSQLServer:  DefaultProductionSQLServer,
		TestDatabase:   DefaultProductionDatabase + "_TEST",
		TestBackupPath: DefaultProductionBackupPath + "Test\\",
		TestMode:       true,
	}

	// Como la producción configurada es otra, los valores antiguos de FEMUCARIBE no deben provocar rechazo
	if err := cfg.Validate(); err != nil {
		t.Fatalf("la validación falló al usar valores personalizados de producción: %v", err)
	}
}

// 10. Validar rechazo ante campos obligatorios vacíos
func TestConfig_RejectsEmptyObligatoryFields(t *testing.T) {
	cfg := TestEnvironmentConfig{
		ProductionSQLServer:  " ",
		ProductionDatabase:   "PROD_DB",
		ProductionBackupPath: `C:\Backups\`,
		TestSQLServer:        "localhost",
		TestDatabase:         "TEST_DB",
		TestBackupPath:       `C:\BackupsTest\`,
		TestMode:             true,
	}

	err := cfg.Validate()
	if err == nil || !errors.Is(err, ErrEmptyConfigField) {
		t.Fatalf("se esperaba ErrEmptyConfigField para servidor de producción vacío, dio: %v", err)
	}
}
