package fixtures

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"strings"
)

//go:embed seed/*.sql
var seedFS embed.FS

// SeedTestData ejecuta el schema, inserta los fixtures base y genera las 200 transacciones determinísticas.
func SeedTestData(ctx context.Context, db *sql.DB) error {
	// 1. Ejecutar schema
	schemaSQL, err := seedFS.ReadFile("seed/schema.sql")
	if err != nil {
		return fmt.Errorf("leer schema.sql: %w", err)
	}
	if err := executeScript(ctx, db, string(schemaSQL)); err != nil {
		return fmt.Errorf("ejecutar schema.sql: %w", err)
	}

	// 2. Limpiar tablas existentes para asegurar idempotencia
	if err := ResetTestData(ctx, db); err != nil {
		return fmt.Errorf("limpiar datos previos: %w", err)
	}

	// 3. Insertar customers, accounts, invoices
	seedSQL, err := seedFS.ReadFile("seed/seed.sql")
	if err != nil {
		return fmt.Errorf("leer seed.sql: %w", err)
	}
	if err := executeScript(ctx, db, string(seedSQL)); err != nil {
		return fmt.Errorf("ejecutar seed.sql: %w", err)
	}

	// 4. Insertar 200 transacciones determinísticas (10 por cada una de las 20 cuentas)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("iniciar transacción para seed de transacciones: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, "INSERT INTO transactions (id, account_id, amount, transaction_type, description, transaction_date) VALUES (?, ?, ?, ?, ?, ?)")
	if err != nil {
		return fmt.Errorf("preparar statement de transacciones: %w", err)
	}
	defer stmt.Close()

	txID := 1
	for accountID := 1; accountID <= 20; accountID++ {
		for i := 1; i <= 10; i++ {
			txType := "DEPOSIT"
			amount := float64(i*50) + 0.25
			if i%2 == 0 {
				txType = "WITHDRAWAL"
				amount = float64(i*20) + 0.75
			}
			desc := fmt.Sprintf("Transacción %d para cuenta %d", txID, accountID)
			dateStr := fmt.Sprintf("2026-01-%02d 10:%02d:00", (i%28)+1, (accountID*2)%60)

			if _, err := stmt.ExecContext(ctx, txID, accountID, amount, txType, desc, dateStr); err != nil {
				return fmt.Errorf("insertar transacción %d: %w", txID, err)
			}
			txID++
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit seed transacciones: %w", err)
	}

	return nil
}

// ResetTestData borra todos los datos de las tablas de prueba sin destruir el schema.
func ResetTestData(ctx context.Context, db *sql.DB) error {
	tables := []string{"transactions", "invoices", "accounts", "customers"}
	for _, table := range tables {
		_, err := db.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s", table))
		if err != nil {
			// Si la tabla no existe aún, ignorar
			if !strings.Contains(strings.ToLower(err.Error()), "no such table") &&
				!strings.Contains(strings.ToLower(err.Error()), "invalid object name") {
				return fmt.Errorf("limpiar tabla %s: %w", table, err)
			}
		}
	}
	return nil
}

// AssertSeedIntegrity comprueba que el dataset completo de prueba esté íntegro.
func AssertSeedIntegrity(ctx context.Context, db *sql.DB) error {
	checks := []struct {
		table    string
		expected int
	}{
		{"customers", 10},
		{"accounts", 20},
		{"invoices", 50},
		{"transactions", 200},
	}

	for _, c := range checks {
		var count int
		row := db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", c.table))
		if err := row.Scan(&count); err != nil {
			return fmt.Errorf("contar registros en %s: %w", c.table, err)
		}
		if count != c.expected {
			return fmt.Errorf("integridad fallida en %s: esperaba %d registros, se encontraron %d", c.table, c.expected, count)
		}
	}

	// Validar un registro crítico
	var custName string
	err := db.QueryRowContext(ctx, "SELECT name FROM customers WHERE id = 1").Scan(&custName)
	if err != nil {
		return fmt.Errorf("consultar customer id 1: %w", err)
	}
	if custName != "Cooperativa Central & Cía" {
		return fmt.Errorf("registro crítico corrupto: customer id 1 tiene nombre %q", custName)
	}

	return nil
}

func executeScript(ctx context.Context, db *sql.DB, script string) error {
	statements := strings.Split(script, ";")
	for _, raw := range statements {
		stmt := strings.TrimSpace(raw)
		if stmt == "" {
			continue
		}
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("ejecutando statement %q: %w", stmt, err)
		}
	}
	return nil
}
