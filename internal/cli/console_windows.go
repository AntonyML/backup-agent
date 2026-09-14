//go:build windows

package cli

import (
	"golang.org/x/sys/windows"
)

// initConsoleEncoding configura la consola de Windows en UTF-8 (Code Page 65001)
// para garantizar que los caracteres Unicode y Box Drawing se dibujen correctamente.
// Devuelve una función de restauración para recuperar los code pages previos al salir.
func initConsoleEncoding() func() {
	inCP, errIn := windows.GetConsoleCP()
	outCP, errOut := windows.GetConsoleOutputCP()

	_ = windows.SetConsoleCP(65001)
	_ = windows.SetConsoleOutputCP(65001)

	return func() {
		if errIn == nil && inCP != 0 {
			_ = windows.SetConsoleCP(inCP)
		}
		if errOut == nil && outCP != 0 {
			_ = windows.SetConsoleOutputCP(outCP)
		}
	}
}
