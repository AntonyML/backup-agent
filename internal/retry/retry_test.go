package retry

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"
)

// retryableErr implementa Retryable sin depender de internal/storage.
type retryableErr struct{ err error }

func (e retryableErr) Error() string   { return e.err.Error() }
func (e retryableErr) Unwrap() error   { return e.err }
func (e retryableErr) Retryable() bool { return true }

var (
	errTransient = retryableErr{err: errors.New("timeout de red simulado")}
	errFatal     = errors.New("credenciales inválidas (401)")
)

// fakeClock registra los delays solicitados sin esperar tiempo real.
type fakeClock struct {
	sleeps []time.Duration
	now    time.Time
}

func (c *fakeClock) sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.sleeps = append(c.sleeps, d)
	c.now = c.now.Add(d)
	return nil
}

func noJitter(time.Duration) time.Duration { return 0 }

func halfJitter(d time.Duration) time.Duration { return d / 2 }

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

func countingFn(failures int) (func(context.Context) error, *int) {
	calls := 0
	return func(context.Context) error {
		calls++
		if calls <= failures {
			return errTransient
		}
		return nil
	}, &calls
}

func TestDo_RetriesUntilSuccess(t *testing.T) {
	clock := &fakeClock{}
	fn, calls := countingFn(2)
	policy := Policy{MaxAttempts: 5, BaseDelay: 10 * time.Millisecond, MaxDelay: time.Second}

	err := DoWithDeps(context.Background(), policy, Deps{Sleep: clock.sleep, Jitter: noJitter, Logger: quietLogger()}, fn)

	if err != nil {
		t.Fatalf("esperaba éxito tras reintentos, recibí %v", err)
	}
	if *calls != 3 {
		t.Fatalf("esperaba 3 llamadas (2 fallos + 1 éxito), recibí %d", *calls)
	}
	if len(clock.sleeps) != 2 {
		t.Fatalf("esperaba 2 esperas de backoff, recibí %d", len(clock.sleeps))
	}
	if clock.sleeps[0] != 10*time.Millisecond || clock.sleeps[1] != 20*time.Millisecond {
		t.Fatalf("delays inesperados: %v", clock.sleeps)
	}
}

func TestDo_ExhaustsAttemptsAndReturnsLastError(t *testing.T) {
	clock := &fakeClock{}
	fn, calls := countingFn(100)
	policy := Policy{MaxAttempts: 3, BaseDelay: time.Millisecond, MaxDelay: time.Second}

	err := DoWithDeps(context.Background(), policy, Deps{Sleep: clock.sleep, Jitter: noJitter, Logger: quietLogger()}, fn)

	if !errors.Is(err, errTransient) {
		t.Fatalf("esperaba el último error transitorio, recibí %v", err)
	}
	if *calls != 3 {
		t.Fatalf("esperaba detenerse en MaxAttempts=3, recibí %d llamadas", *calls)
	}
	if len(clock.sleeps) != 2 {
		t.Fatalf("esperaba 2 esperas (no se espera después del último intento), recibí %d", len(clock.sleeps))
	}
}

func TestDo_FatalErrorStopsAtFirstAttempt(t *testing.T) {
	clock := &fakeClock{}
	calls := 0
	fn := func(context.Context) error {
		calls++
		return errFatal
	}
	policy := Policy{MaxAttempts: 4, BaseDelay: time.Millisecond, MaxDelay: time.Second}

	err := DoWithDeps(context.Background(), policy, Deps{Sleep: clock.sleep, Jitter: noJitter, Logger: quietLogger()}, fn)

	if !errors.Is(err, errFatal) {
		t.Fatalf("esperaba el error fatal, recibí %v", err)
	}
	if calls != 1 {
		t.Fatalf("un error fatal no debe reintentarse, llamadas=%d", calls)
	}
	if len(clock.sleeps) != 0 {
		t.Fatalf("un error fatal no debe esperar backoff, sleeps=%v", clock.sleeps)
	}
}

func TestDo_CustomClassifierOverridesDefault(t *testing.T) {
	clock := &fakeClock{}
	calls := 0
	fn := func(context.Context) error {
		calls++
		if calls < 2 {
			return errFatal // plano: por defecto sería fatal
		}
		return nil
	}
	policy := Policy{
		MaxAttempts: 3,
		BaseDelay:   time.Millisecond,
		MaxDelay:    time.Second,
		IsRetryable: func(error) bool { return true },
	}

	if err := DoWithDeps(context.Background(), policy, Deps{Sleep: clock.sleep, Jitter: noJitter, Logger: quietLogger()}, fn); err != nil {
		t.Fatalf("el clasificador inyectado debía habilitar el reintento, recibí %v", err)
	}
	if calls != 2 {
		t.Fatalf("esperaba 2 llamadas, recibí %d", calls)
	}
}

func TestDo_MaxAttemptsOneDoesNotSleep(t *testing.T) {
	clock := &fakeClock{}
	fn, calls := countingFn(100)

	err := DoWithDeps(context.Background(), Policy{}, Deps{Sleep: clock.sleep, Jitter: noJitter, Logger: quietLogger()}, fn)

	if err == nil || *calls != 1 || len(clock.sleeps) != 0 {
		t.Fatalf("MaxAttempts cero/uno no debe dormir: err=%v llamadas=%d sleeps=%v", err, *calls, clock.sleeps)
	}
}

func TestDo_AlreadyCanceledContextDoesNotCallFn(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	fn := func(context.Context) error { calls++; return nil }

	err := DoWithDeps(ctx, Policy{MaxAttempts: 3}, Deps{Sleep: (&fakeClock{}).sleep, Jitter: noJitter, Logger: quietLogger()}, fn)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("esperaba context.Canceled, recibí %v", err)
	}
	if calls != 0 {
		t.Fatalf("con ctx cancelado no debe ejecutarse fn, llamadas=%d", calls)
	}
}

func TestDo_CanceledDuringBackoffAbortsImmediately(t *testing.T) {
	clock := &fakeClock{}
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	fn := func(context.Context) error {
		calls++
		cancel() // simula presupuesto de tiempo agotado durante el intento
		return errTransient
	}
	policy := Policy{MaxAttempts: 5, BaseDelay: time.Second, MaxDelay: time.Minute}

	err := DoWithDeps(ctx, policy, Deps{Sleep: clock.sleep, Jitter: noJitter, Logger: quietLogger()}, fn)

	if !errors.Is(err, errTransient) {
		t.Fatalf("esperaba el último error transitorio, recibí %v", err)
	}
	if calls != 1 {
		t.Fatalf("debía abortar sin más intentos, llamadas=%d", calls)
	}
	if len(clock.sleeps) != 0 {
		t.Fatalf("no debía esperar el backoff pendiente, sleeps=%v", clock.sleeps)
	}
}

func TestDo_NilFnReturnsError(t *testing.T) {
	if err := Do(context.Background(), Policy{MaxAttempts: 2}, nil); err == nil {
		t.Fatal("esperaba error con fn nil")
	}
}

func TestDefaultIsRetryable(t *testing.T) {
	if !DefaultIsRetryable(errTransient) {
		t.Fatal("un error que implementa Retryable debe reintentarse")
	}
	if !DefaultIsRetryable(retryableErr{err: context.DeadlineExceeded}) {
		t.Fatal("un error envuelto que implementa Retryable debe reintentarse")
	}
	if DefaultIsRetryable(errFatal) {
		t.Fatal("un error plano debe ser fatal")
	}
	if DefaultIsRetryable(nil) {
		t.Fatal("nil no es reintentable")
	}
}

func TestBackoffDelay_GrowsExponentiallyAndRespectsMaxDelay(t *testing.T) {
	policy := Policy{BaseDelay: 100 * time.Millisecond, MaxDelay: time.Second}
	want := []time.Duration{
		100 * time.Millisecond,
		200 * time.Millisecond,
		400 * time.Millisecond,
		800 * time.Millisecond,
		time.Second, // 1600ms recortado al techo
		time.Second,
	}
	for attempt, expected := range want {
		got := BackoffDelay(policy, attempt+1, noJitter)
		if got != expected {
			t.Fatalf("attempt=%d: esperaba %v, recibí %v", attempt+1, expected, got)
		}
	}
}

func TestBackoffDelay_WithoutMaxDelayKeepsDoubling(t *testing.T) {
	policy := Policy{BaseDelay: time.Second}
	if got := BackoffDelay(policy, 4, noJitter); got != 8*time.Second {
		t.Fatalf("sin MaxDelay esperaba 8s, recibí %v", got)
	}
}

func TestBackoffDelay_JitterNeverExceedsMaxDelay(t *testing.T) {
	policy := Policy{BaseDelay: 100 * time.Millisecond, MaxDelay: time.Second}
	var previous time.Duration
	for attempt := 1; attempt <= 6; attempt++ {
		got := BackoffDelay(policy, attempt, halfJitter)
		if got <= 0 {
			t.Fatalf("attempt=%d: delay inválido %v", attempt, got)
		}
		if got > policy.MaxDelay {
			t.Fatalf("attempt=%d: jitter excedió MaxDelay (%v > %v)", attempt, got, policy.MaxDelay)
		}
		if attempt > 1 && got < previous {
			t.Fatalf("attempt=%d: el backoff debe ser no decreciente (%v < %v)", attempt, got, previous)
		}
		previous = got
	}
}

func TestBackoffDelay_ZeroBaseDelayIsZero(t *testing.T) {
	if got := BackoffDelay(Policy{}, 3, noJitter); got != 0 {
		t.Fatalf("sin BaseDelay esperaba 0, recibí %v", got)
	}
}

func TestBackoffDelay_RealJitterStaysWithinHalfOfDelay(t *testing.T) {
	policy := Policy{BaseDelay: time.Second, MaxDelay: time.Minute}
	for attempt := 1; attempt <= 5; attempt++ {
		got := BackoffDelay(policy, attempt, nil)
		floor := time.Duration(1<<(attempt-1)) * time.Second
		if got < floor || got > floor+floor/2 {
			t.Fatalf("attempt=%d: %v fuera del rango [%v, %v]", attempt, got, floor, floor+floor/2)
		}
	}
}
