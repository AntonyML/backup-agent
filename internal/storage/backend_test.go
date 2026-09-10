package storage

import (
	"errors"
	"testing"
)

func TestRetryableError(t *testing.T) {
	baseErr := errors.New("timeout de red")
	retErr := NewRetryableError(baseErr)

	if retErr == nil {
		t.Fatal("retErr no debería ser nil")
	}
	if retErr.Error() != "timeout de red" {
		t.Errorf("Error() inesperado: %s", retErr.Error())
	}

	var target *RetryableError
	if !errors.As(retErr, &target) {
		t.Fatal("errors.As debería encontrar RetryableError")
	}
	if !errors.Is(retErr, baseErr) {
		t.Fatal("errors.Is debería coincidir con baseErr vía Unwrap")
	}

	if NewRetryableError(nil) != nil {
		t.Error("NewRetryableError(nil) debe devolver nil")
	}
}
