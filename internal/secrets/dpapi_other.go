//go:build !windows

package secrets

import "errors"

var errNotSupported = errors.New("dpapi solo está disponible en Windows")

// Protect stub para plataformas no Windows.
func Protect(data []byte) ([]byte, error) {
	return nil, errNotSupported
}

// Unprotect stub para plataformas no Windows.
func Unprotect(data []byte) ([]byte, error) {
	return nil, errNotSupported
}
