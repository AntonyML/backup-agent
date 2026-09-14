//go:build !windows

package cli

// initConsoleEncoding es un no-op en entornos POSIX / no-Windows.
func initConsoleEncoding() func() {
	return func() {}
}
