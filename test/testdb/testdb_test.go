package testdb

import (
	"context"
	"path/filepath"
	"testing"

	"femucaribe-backup-agent/test/fixtures"
	"femucaribe-backup-agent/test/helpers"
)

func TestSQLiteMemory_SeedAndReset(t *testing.T) {
	ctx := context.Background()
	factory := NewFactory()

	db, err := factory.SQLiteMemory(ctx)
	if err != nil {
		t.Fatalf("factory.SQLiteMemory falló: %v", err)
	}
	defer db.Close()

	if err := db.Seed(ctx); err != nil {
		t.Fatalf("db.Seed falló: %v", err)
	}

	if err := fixtures.AssertSeedIntegrity(ctx, db.DB()); err != nil {
		t.Fatalf("AssertSeedIntegrity falló: %v", err)
	}

	// Verificar reset
	if err := db.Reset(ctx); err != nil {
		t.Fatalf("db.Reset falló: %v", err)
	}

	var count int
	if err := db.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM customers").Scan(&count); err != nil {
		t.Fatalf("contar customers tras reset: %v", err)
	}
	if count != 0 {
		t.Errorf("esperaba 0 customers tras reset, se encontraron %d", count)
	}
}

func TestSQLiteFile_PersistenceAndEquivalence(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbFile := filepath.Join(tmpDir, "test.sqlite")

	factory := NewFactory()

	// Crear y poblar
	dbOrig, err := factory.SQLiteFile(ctx, dbFile)
	if err != nil {
		t.Fatalf("factory.SQLiteFile falló: %v", err)
	}
	if err := dbOrig.Seed(ctx); err != nil {
		t.Fatalf("seed falló: %v", err)
	}

	// Abrir segunda conexión sobre el mismo archivo
	dbCopy, err := factory.SQLiteFile(ctx, dbFile)
	if err != nil {
		t.Fatalf("segunda conexión falló: %v", err)
	}
	defer dbCopy.Close()
	defer dbOrig.Close()

	// Comprobar equivalencia con snapshots
	helpers.AssertDatabaseEquivalent(t, dbOrig.DB(), dbCopy.DB())
}

func TestSnapshot_DetectsDataDifference(t *testing.T) {
	ctx := context.Background()
	factory := NewFactory()

	db1, err := factory.SQLiteMemory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer db1.Close()
	_ = db1.Seed(ctx)

	snap1, err := helpers.TakeSnapshot(ctx, db1.DB())
	if err != nil {
		t.Fatal(err)
	}

	// Modificar un dato en customers
	_, err = db1.DB().ExecContext(ctx, "UPDATE customers SET name = 'Nombre Modificado' WHERE id = 1")
	if err != nil {
		t.Fatal(err)
	}

	snap2, err := helpers.TakeSnapshot(ctx, db1.DB())
	if err != nil {
		t.Fatal(err)
	}

	if snap1.Tables["customers"].Hash == snap2.Tables["customers"].Hash {
		t.Errorf("el hash de snapshot debió cambiar tras modificar un registro")
	}
}
