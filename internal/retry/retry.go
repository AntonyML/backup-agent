// Package retry implementa la política única de reintentos con backoff
// exponencial y jitter para operaciones de red transitorias (R2, servidor
// remoto, Supabase).
//
// El paquete no conoce ningún backend: recibe una función y decide cuántas
// veces repetirla según la clasificación del error. Los backends ejecutan la
// operación una sola vez y reportan; la capa de aplicación decide si se
// reintenta (regla de arquitectura de Fase 2.1).
package retry

import (
	"context"
	"errors"
	"log/slog"
	"math/rand/v2"
	"time"
)

// ErrNilFunc se devuelve cuando Do recibe una función nula.
var ErrNilFunc = errors.New("retry: fn no puede ser nil")

// Retryable marca un error como transitorio y por lo tanto reintentable.
// storage.RetryableError implementa esta interfaz; la capa de aplicación puede
// aportar su propio clasificador vía Policy.IsRetryable.
type Retryable interface {
	Retryable() bool
}

// DefaultIsRetryable clasifica como reintentable únicamente los errores que
// implementan Retryable. Un error plano es fatal: se corta en el primer intento.
func DefaultIsRetryable(err error) bool {
	if err == nil {
		return false
	}
	var r Retryable
	if errors.As(err, &r) {
		return r.Retryable()
	}
	return false
}

// Policy describe cuántas veces intentar y cuánto esperar entre intentos.
type Policy struct {
	// MaxAttempts es la cantidad total de intentos, incluido el primero.
	// Valores <= 0 se normalizan a 1 (una sola pasada, sin reintentos).
	MaxAttempts int
	// BaseDelay es la espera antes del segundo intento. Se duplica en cada
	// reintento posterior hasta alcanzar MaxDelay.
	BaseDelay time.Duration
	// MaxDelay es el techo absoluto de espera entre intentos. 0 = sin techo.
	MaxDelay time.Duration
	// IsRetryable clasifica el error de cada intento. nil usa DefaultIsRetryable.
	IsRetryable func(error) bool
}

// SleepFunc espera d respetando la cancelación de ctx. Es inyectable para que
// los tests usen un reloj simulado sin esperar tiempos reales.
type SleepFunc func(ctx context.Context, d time.Duration) error

// JitterFunc devuelve un agregado aleatorio en [0, d] que se suma al backoff.
type JitterFunc func(d time.Duration) time.Duration

// Deps agrupa dependencias inyectables. El valor cero usa los valores productivos.
type Deps struct {
	Sleep  SleepFunc
	Jitter JitterFunc
	Logger *slog.Logger
}

// Do ejecuta fn aplicando la política de reintentos.
//
// Reglas:
//   - Reintenta solo errores clasificados como reintentables.
//   - Un error no reintentable corta en el primer intento (fatal).
//   - Si ctx se cancela, aborta de inmediato y no espera el backoff pendiente:
//     devuelve el último error de fn para que el llamador pueda seguir
//     clasificándolo (por ejemplo, marcar pending_sync).
//   - Devuelve nil si algún intento tuvo éxito, o el último error si se agotaron
//     los intentos.
func Do(ctx context.Context, policy Policy, fn func(ctx context.Context) error) error {
	return DoWithDeps(ctx, policy, Deps{}, fn)
}

// DoWithDeps es Do con dependencias inyectables (tests con reloj y jitter simulados).
func DoWithDeps(ctx context.Context, policy Policy, deps Deps, fn func(ctx context.Context) error) error {
	if fn == nil {
		return ErrNilFunc
	}

	maxAttempts := policy.MaxAttempts
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	isRetryable := policy.IsRetryable
	if isRetryable == nil {
		isRetryable = DefaultIsRetryable
	}
	sleep := deps.Sleep
	if sleep == nil {
		sleep = sleepWithContext
	}
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if ctxErr := ctx.Err(); ctxErr != nil {
			if lastErr != nil {
				logger.Warn("reintentos abortados por cancelación de contexto",
					"intento", attempt, "max_attempts", maxAttempts, "error", lastErr)
				return lastErr
			}
			return ctxErr
		}

		lastErr = fn(ctx)
		if lastErr == nil {
			return nil
		}

		if !isRetryable(lastErr) {
			logger.Error("error fatal, sin reintento",
				"intento", attempt, "max_attempts", maxAttempts, "error", lastErr)
			return lastErr
		}

		if attempt == maxAttempts {
			break
		}

		delay := BackoffDelay(policy, attempt, deps.Jitter)
		logger.Warn("intento fallido, reintentando",
			"intento", attempt,
			"max_attempts", maxAttempts,
			"espera", delay.String(),
			"error", lastErr)

		if err := sleep(ctx, delay); err != nil {
			logger.Warn("reintentos abortados durante la espera de backoff",
				"intento", attempt, "error", lastErr)
			return lastErr
		}
	}

	logger.Error("reintentos agotados", "intentos", maxAttempts, "error", lastErr)
	return lastErr
}

// BackoffDelay calcula la espera previa al intento siguiente a attempt
// (attempt = 1 → después del primer fallo). Es BaseDelay * 2^(attempt-1)
// recortado por MaxDelay, más un jitter acotado que nunca excede MaxDelay.
func BackoffDelay(policy Policy, attempt int, jitter JitterFunc) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	base := policy.BaseDelay
	if base < 0 {
		base = 0
	}
	maxDelay := policy.MaxDelay
	if maxDelay > 0 && base > maxDelay {
		base = maxDelay
	}

	delay := base
	for i := 1; i < attempt; i++ {
		if delay <= 0 || (maxDelay > 0 && delay >= maxDelay) {
			break
		}
		delay *= 2
		if maxDelay > 0 && delay > maxDelay {
			delay = maxDelay
		}
	}

	if jitter == nil {
		jitter = randomJitter
	}
	delay += jitter(delay)
	if maxDelay > 0 && delay > maxDelay {
		delay = maxDelay
	}
	if delay < 0 {
		delay = 0
	}
	return delay
}

// randomJitter devuelve un valor aleatorio en [0, d/2]. Evita que varios
// destinos que fallan al mismo tiempo reintenten sincronizados.
func randomJitter(d time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}
	return time.Duration(rand.Int64N(int64(d/2) + 1))
}

// sleepWithContext espera d o hasta que ctx se cancele, lo que ocurra primero.
func sleepWithContext(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
