package testenv

import (
	"errors"
	"testing"
)

func TestValidateSafety_RejectsProductionServer(t *testing.T) {
	cfg := TestEnvironmentConfig{
		TestMode:       true,
		ServerInstance: "Caproba01\\vbadilla",
		DatabaseName:   "CONTABILIDAD_TEST",
		BackupRoot:     `C:\BackupsTest\`,
	}

	err := ValidateSafety(cfg)
	if err == nil {
		t.Fatal("ValidateSafety debió rechazar el servidor de producción Caproba01\\vbadilla")
	}
	if !errors.Is(err, ErrProductionServerForbidden) {
		t.Errorf("esperaba ErrProductionServerForbidden, dio: %v", err)
	}
}

func TestValidateSafety_RejectsProductionDatabase(t *testing.T) {
	cfg := TestEnvironmentConfig{
		TestMode:       true,
		ServerInstance: "localhost,14333",
		DatabaseName:   "CONTABILIDAD",
		BackupRoot:     `C:\BackupsTest\`,
	}

	err := ValidateSafety(cfg)
	if err == nil {
		t.Fatal("ValidateSafety debió rechazar la base de datos de producción CONTABILIDAD")
	}
	if !errors.Is(err, ErrProductionDatabaseForbidden) {
		t.Errorf("esperaba ErrProductionDatabaseForbidden, dio: %v", err)
	}
}

func TestValidateSafety_RejectsProductionBackupDir(t *testing.T) {
	cases := []string{
		`C:\Backups\`,
		`C:\Backups`,
		`c:\backups\`,
		`c:/backups`,
	}

	for _, p := range cases {
		cfg := TestEnvironmentConfig{
			TestMode:       true,
			ServerInstance: "localhost,14333",
			DatabaseName:   "CONTABILIDAD_TEST",
			BackupRoot:     p,
		}

		err := ValidateSafety(cfg)
		if err == nil {
			t.Fatalf("ValidateSafety debió rechazar el directorio de producción %q", p)
		}
		if !errors.Is(err, ErrProductionBackupDirForbidden) {
			t.Errorf("para %q esperaba ErrProductionBackupDirForbidden, dio: %v", p, err)
		}
	}
}

func TestValidateSafety_AcceptsIsolatedTestEnvironment(t *testing.T) {
	// 1. SQL Server aislado
	cfgSQL := TestEnvironmentConfig{
		TestMode:       true,
		DatabaseDriver: "sqlserver",
		ServerInstance: "localhost,14333",
		DatabaseName:   "CONTABILIDAD_TEST",
		BackupRoot:     `C:\BackupsTest\`,
	}
	if err := ValidateSafety(cfgSQL); err != nil {
		t.Errorf("ValidateSafety debió aceptar entorno SQL Server aislado, dio: %v", err)
	}

	// 2. SQLite aislado
	cfgSQLite := TestEnvironmentConfig{
		TestMode:       true,
		DatabaseDriver: "sqlite",
		DatabaseName:   ":memory:",
		BackupRoot:     `C:\BackupsTest\sqlite\`,
	}
	if err := ValidateSafety(cfgSQLite); err != nil {
		t.Errorf("ValidateSafety debió aceptar entorno SQLite aislado, dio: %v", err)
	}
}
