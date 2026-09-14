package storage

import (
	"context"
)

// RetryableError indica un error transitorio o recuperable (por ejemplo, caída de red o timeout),
// que la capa de aplicación puede capturar para decidir no bloquear el proceso y marcar pending_sync.
type RetryableError struct {
	Err error
}

func (e *RetryableError) Error() string {
	if e == nil || e.Err == nil {
		return "error recuperable"
	}
	return e.Err.Error()
}

func (e *RetryableError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// NewRetryableError envuelve un error como recuperable. Si err es nil, devuelve nil.
func NewRetryableError(err error) error {
	if err == nil {
		return nil
	}
	return &RetryableError{Err: err}
}

// Retryable marca este error como transitorio para la política de reintentos de
// internal/retry (satisface retry.Retryable sin acoplar ese paquete a storage).
func (e *RetryableError) Retryable() bool {
	return e != nil
}

// Backend define el contrato de almacenamiento desacoplado de estado y configuración global.
type Backend interface {
	Name() string
	Upload(ctx context.Context, localPath string) error
	Rotate(ctx context.Context, keep int) error
	LatestRemote(ctx context.Context) (string, error)
}
