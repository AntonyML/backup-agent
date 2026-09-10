//go:build sqlserver

package testdb

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"femucaribe-backup-agent/test/fixtures"
	"femucaribe-backup-agent/test/helpers"
)

func getSQLServerTestConfig() SQLServerConfig {
	host := os.Getenv("TEST_SQLSERVER_HOST")
	if host == "" {
		host = "localhost"
	}
	portStr := os.Getenv("TEST_SQLSERVER_PORT")
	port := 14333
	if p, err := strconv.Atoi(portStr); err == nil {
		port = p
	}
	db := os.Getenv("TEST_SQLSERVER_DATABASE")
	if db == "" {
		db = "CONTABILIDAD_TEST"
	}
	user := os.Getenv("TEST_SQLSERVER_USER")
	if user == "" {
		user = "sa"
	}
	pass := os.Getenv("TEST_SQLSERVER_PASSWORD")
	if pass == "" {
		pass = "TestPassw0rd!123"
	}
	backupRoot := os.Getenv("TEST_BACKUP_ROOT")
	if backupRoot == "" {
		backupRoot = `C:\BackupsTest\`
	}

	return SQLServerConfig{
		Host:       host,
		Port:       port,
		Database:   db,
		User:       user,
		Password:   pass,
		BackupRoot: backupRoot,
	}
}

func TestSQLServer_SeedAndSnapshot(t *testing.T) {
	cfg := getSQLServerTestConfig()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db, err := NewSQLServer(ctx, cfg)
	if err != nil {
		t.Skipf("SQL Server aislado no disponible en %s:%d (%v); saltando test de integración", cfg.Host, cfg.Port, err)
	}
	defer db.Close()

	if err := db.Seed(ctx); err != nil {
		t.Fatalf("seed en SQL Server falló: %v", err)
	}

	if err := fixtures.AssertSeedIntegrity(ctx, db.DB()); err != nil {
		t.Fatalf("integridad de seed en SQL Server falló: %v", err)
	}

	snap, err := helpers.TakeSnapshot(ctx, db.DB())
	if err != nil {
		t.Fatalf("TakeSnapshot en SQL Server falló: %v", err)
	}

	if snap.Tables["customers"].RowCount != 10 {
		t.Errorf("esperaba 10 customers en snapshot, dio: %d", snap.Tables["customers"].RowCount)
	}
}
