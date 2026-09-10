package testenv

import (
	"os"
	"strconv"
)

// Config centraliza los parámetros de configuración para las pruebas de integración.
type Config struct {
	ProductionSQLServer  string
	ProductionDatabase   string
	ProductionBackupPath string

	DatabaseDriver string
	DatabasePath   string
	BackupRoot     string

	SQLServerHost     string
	SQLServerPort     int
	SQLServerDatabase string
	SQLServerUser     string
	SQLServerPassword string

	KeepArtifacts bool
}

// LoadConfig carga la configuración de prueba desde variables de entorno con valores seguros por defecto.
func LoadConfig() Config {
	envCfg := LoadTestEnvironmentConfig()

	port := 14333
	if p, err := strconv.Atoi(os.Getenv("TEST_SQLSERVER_PORT")); err == nil && p > 0 {
		port = p
	}
	user := os.Getenv("TEST_SQLSERVER_USER")
	if user == "" {
		user = "sa"
	}
	password := os.Getenv("TEST_SQLSERVER_PASSWORD")
	if password == "" {
		password = "TestPassw0rd!123"
	}
	keepArtifacts := os.Getenv("KEEP_TEST_ARTIFACTS") == "true"

	return Config{
		ProductionSQLServer:  envCfg.ProductionSQLServer,
		ProductionDatabase:   envCfg.ProductionDatabase,
		ProductionBackupPath: envCfg.ProductionBackupPath,

		DatabaseDriver:    envCfg.DatabaseDriver,
		DatabasePath:      os.Getenv("TEST_DATABASE_PATH"),
		BackupRoot:        envCfg.TestBackupPath,
		SQLServerHost:     envCfg.TestSQLServer,
		SQLServerPort:     port,
		SQLServerDatabase: envCfg.TestDatabase,
		SQLServerUser:     user,
		SQLServerPassword: password,
		KeepArtifacts:     keepArtifacts,
	}
}

// ToSafetyConfig convierte la configuración de test al formato requerido por ValidateSafety.
func (c Config) ToSafetyConfig() TestEnvironmentConfig {
	server := c.SQLServerHost
	if c.SQLServerPort > 0 {
		server = server + ":" + strconv.Itoa(c.SQLServerPort)
	}
	dbName := c.SQLServerDatabase
	if c.DatabaseDriver == "sqlite" {
		dbName = "test_sqlite"
	}
	return TestEnvironmentConfig{
		ProductionSQLServer:  c.ProductionSQLServer,
		ProductionDatabase:   c.ProductionDatabase,
		ProductionBackupPath: c.ProductionBackupPath,
		DatabaseDriver:       c.DatabaseDriver,
		TestDatabase:         dbName,
		TestSQLServer:        server,
		TestBackupPath:       c.BackupRoot,
		TestMode:             true,
	}
}
