package cli

import (
	"fmt"

	"femucaribe-backup-agent/internal/application"

	"github.com/spf13/cobra"
)

func newLogsCmd(appProvider func() (*application.App, error)) *cobra.Command {
	var lines int

	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Muestra las últimas líneas del registro del día",
		Long:  "Lee y despliega las últimas líneas del archivo de log diario activo.",
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := appProvider()
			if err != nil {
				return err
			}
			entries, err := app.TailLogs(cmd.Context(), lines)
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			for _, line := range entries {
				fmt.Fprintln(out, line)
			}
			return nil
		},
	}

	cmd.Flags().IntVarP(&lines, "lines", "n", 50, "número de líneas a mostrar")

	return cmd
}
