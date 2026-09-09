//go:build integration

// Tests de integración: requieren SQL Server real (local o contenedor).
// NUNCA contra CONTABILIDAD de producción.
//
// Uso (PowerShell):
//
//	$env:FEMU_TEST_SQLSERVER = "localhost\SQLEXPRESS"  # o "localhost" / contenedor
//	go test -tags integration ./internal/sqlbackup/ -v
//
// El test crea y borra la base `femucaribe_baktest`. Si el servidor no
// responde, el test falla (es la señal de que el entorno no está listo).
package sqlbackup

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testDB = "femucaribe_baktest"

func testServer(t *testing.T) string {
	t.Helper()
	if strings.EqualFold(testDB, "CONTABILIDAD") {
		t.Fatal("guard: prohibido usar CONTABILIDAD en tests")
	}
	srv := os.Getenv("FEMU_TEST_SQLSERVER")
	if srv == "" {
		srv = "localhost"
	}
	return srv
}

// TestBackupAndVerify_FullCycle prueba BACKUP + VERIFY contra una base chica.
func TestBackupAndVerify_FullCycle(t *testing.T) {
	srv := testServer(t)
	db, err := Open(srv, 10)
	if err != nil {
		t.Fatalf("Open(%s): %v — ¿SQL Server corriendo y con Windows Auth?", srv, err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if _, err := db.ExecContext(ctx, "IF DB_ID("+QuoteStringLiteral(testDB)+") IS NOT NULL DROP DATABASE "+QuoteIdent(testDB)); err != nil {
		t.Fatalf("limpieza previa: %v", err)
	}
	if _, err := db.ExecContext(ctx, "CREATE DATABASE "+QuoteIdent(testDB)); err != nil {
		t.Fatalf("CREATE DATABASE: %v", err)
	}
	defer db.ExecContext(context.Background(), "ALTER DATABASE "+QuoteIdent(testDB)+" SET SINGLE_USER WITH ROLLBACK IMMEDIATE; DROP DATABASE "+QuoteIdent(testDB))

	if _, err := db.ExecContext(ctx, "CREATE TABLE "+QuoteIdent(testDB)+".dbo.t (id INT PRIMARY KEY, v NVARCHAR(100)); INSERT INTO "+QuoteIdent(testDB)+".dbo.t VALUES (1, 'hola')"); err != nil {
		t.Fatalf("datos de prueba: %v", err)
	}

	dest := filepath.Join(t.TempDir(), testDB+".bak")
	if err := BackupDatabase(ctx, db, testDB, dest); err != nil {
		t.Fatalf("BackupDatabase: %v", err)
	}
	fi, err := os.Stat(dest)
	if err != nil || fi.Size() == 0 {
		t.Fatalf("el .bak debería existir y no estar vacío: %v %v", fi, err)
	}
	if err := VerifyBackup(ctx, db, dest); err != nil {
		t.Fatalf("VerifyBackup: %v", err)
	}
}
