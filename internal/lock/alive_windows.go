//go:build windows

package lock

// Stub Windows: la detección real va por tasklist en lock.go
// (windowsPidAlive). Este archivo existe para dejar explícito que
// en Windows no se usa señal 0.

func unixPidAlive(pid int) bool {
	// Nunca se llama en Windows; por seguridad asumir vivo.
	return true
}
