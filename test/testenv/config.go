package testenv

import (
	"os"
	"strconv"
)

// Config centraliza los parámetros de configuración para las pruebas de integración.
type Config struct {
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
	driver := os.Getenv("TEST_DATABASE_DRIVER")
	if driver == "" {
		driver = "sqlite"
	}
	dbPath := os.Getenv("TEST_DATABASE_PATH")
	backupRoot := os.Getenv("TEST_BACKUP_ROOT")
	if backupRoot == "" {
		backupRoot = `C:\BackupsTest\`
	}

	host := os.Getenv("TEST_SQLSERVER_HOST")
	if host == "" {
		host = "localhost"
	}
	port := 14333
	if p, err := strconv.Atoi(os.Getenv("TEST_SQLSERVER_PORT")); err == nil && p > 0 {
		port = p
	}
	database := os.Getenv("TEST_SQLSERVER_DATABASE")
	if database == "" {
		database = "CONTABILIDAD_TEST"
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
		DatabaseDriver:    driver,
		DatabasePath:      dbPath,
		BackupRoot:        backupRoot,
		SQLServerHost:     host,
		SQLServerPort:     port,
		SQLServerDatabase: database,
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
		DatabaseDriver: c.DatabaseDriver,
		DatabaseName:   dbName,
		ServerInstance: server,
		BackupRoot:     c.BackupRoot,
		TestMode:       true,
	}
}
