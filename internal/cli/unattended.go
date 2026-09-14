package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
)

// errUnattended es el error que devuelve cualquier intento de leer stdin en
// modo desatendido (D8): la garantía es que la lectura falla de inmediato en
// lugar de bloquear esperando input que nunca llegará.
var errUnattended = errors.New("modo unattended: no se permite leer stdin")

// unattendedStdin es un io.Reader que falla siempre. Se instala como entrada
// del comando cuando --unattended está presente: cualquier intento de lectura
// (prompts, menús, TUI) recibe este error de inmediato.
type unattendedStdin struct{}

func (unattendedStdin) Read(p []byte) (int, error) {
	return 0, errUnattended
}

// installUnattendedStdinGuard activa la garantía de no-stdin para el comando.
func installUnattendedStdinGuard(cmd *cobra.Command) {
	cmd.SetIn(unattendedStdin{})
	fmt.Fprintln(cmd.ErrOrStderr(), "modo unattended: ejecución sin interacción garantizada")
}
