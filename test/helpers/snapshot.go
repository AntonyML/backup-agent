package helpers

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// TableSnapshot guarda el conteo y hash determinístico de una tabla.
type TableSnapshot struct {
	RowCount int
	Hash     string
}

// DatabaseSnapshot almacena el estado integral de la base de datos para comparaciones antes/después.
type DatabaseSnapshot struct {
	Tables map[string]TableSnapshot
}

// TakeSnapshot genera un snapshot determinístico ordenado por clave primaria con hash SHA-256.
func TakeSnapshot(ctx context.Context, db *sql.DB) (*DatabaseSnapshot, error) {
	tables := []struct {
		name    string
		pk      string
		columns []string
	}{
		{
			name:    "customers",
			pk:      "id",
			columns: []string{"id", "name", "COALESCE(email, '')", "created_at"},
		},
		{
			name:    "accounts",
			pk:      "id",
			columns: []string{"id", "customer_id", "account_number", "balance", "status"},
		},
		{
			name:    "invoices",
			pk:      "id",
			columns: []string{"id", "customer_id", "invoice_number", "amount", "issued_date", "paid"},
		},
		{
			name:    "transactions",
			pk:      "id",
			columns: []string{"id", "account_id", "amount", "transaction_type", "COALESCE(description, '')", "transaction_date"},
		},
	}

	snap := &DatabaseSnapshot{
		Tables: make(map[string]TableSnapshot),
	}

	for _, t := range tables {
		query := fmt.Sprintf("SELECT %s FROM %s ORDER BY %s ASC", strings.Join(t.columns, ", "), t.name, t.pk)
		rows, err := db.QueryContext(ctx, query)
		if err != nil {
			return nil, fmt.Errorf("consultar tabla %s: %w", t.name, err)
		}

		h := sha256.New()
		count := 0
		colCount := len(t.columns)

		for rows.Next() {
			count++
			vals := make([]interface{}, colCount)
			valPtrs := make([]interface{}, colCount)
			for i := range vals {
				valPtrs[i] = &vals[i]
			}
			if err := rows.Scan(valPtrs...); err != nil {
				rows.Close()
				return nil, fmt.Errorf("escanear fila de %s: %w", t.name, err)
			}

			for _, v := range vals {
				h.Write([]byte(fmt.Sprintf("%v|", v)))
			}
			h.Write([]byte("\n"))
		}
		rows.Close()

		snap.Tables[t.name] = TableSnapshot{
			RowCount: count,
			Hash:     hex.EncodeToString(h.Sum(nil)),
		}
	}

	return snap, nil
}

// AssertDatabaseEquivalent compara rigurosamente los snapshots de dos bases de datos y falla el test si hay diferencias.
func AssertDatabaseEquivalent(t *testing.T, origDB, restoredDB *sql.DB) {
	t.Helper()
	AssertDatabaseEquivalentWithContext(context.Background(), t, origDB, restoredDB)
}

// AssertDatabaseEquivalentWithContext compara los snapshots de dos bases de datos bajo el contexto provisto.
func AssertDatabaseEquivalentWithContext(ctx context.Context, t *testing.T, origDB, restoredDB *sql.DB) {
	t.Helper()

	origSnap, err := TakeSnapshot(ctx, origDB)
	if err != nil {
		t.Fatalf("error generando snapshot de base original: %v", err)
	}

	restoredSnap, err := TakeSnapshot(ctx, restoredDB)
	if err != nil {
		t.Fatalf("error generando snapshot de base restaurada: %v", err)
	}

	for table, origT := range origSnap.Tables {
		restT, exists := restoredSnap.Tables[table]
		if !exists {
			t.Errorf("tabla %s no encontrada en base restaurada", table)
			continue
		}
		if origT.RowCount != restT.RowCount {
			t.Errorf("diferencia de filas en tabla %s: original=%d, restaurada=%d", table, origT.RowCount, restT.RowCount)
		}
		if origT.Hash != restT.Hash {
			t.Errorf("discrepancia de datos en tabla %s: hash original=%s, restaurado=%s", table, origT.Hash, restT.Hash)
		}
	}
}

