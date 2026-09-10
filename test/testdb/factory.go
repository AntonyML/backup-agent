package testdb

import (
	"context"
	"database/sql"
)

// TestDatabase define el contrato de operaciones sobre una base de datos aislada para testing.
type TestDatabase interface {
	Driver() string
	DSN() string
	DB() *sql.DB
	Seed(ctx context.Context) error
	Reset(ctx context.Context) error
	Close() error
}

// DatabaseFactory permite instanciar bases de prueba sin exponer detalles de conexión en los tests.
type DatabaseFactory struct{}

func NewFactory() *DatabaseFactory {
	return &DatabaseFactory{}
}

func (f *DatabaseFactory) SQLiteMemory(ctx context.Context) (TestDatabase, error) {
	return NewSQLiteMemory(ctx)
}

func (f *DatabaseFactory) SQLiteFile(ctx context.Context, filePath string) (TestDatabase, error) {
	return NewSQLiteFile(ctx, filePath)
}
