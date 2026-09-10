package cli

import (
	"fmt"

	"femucaribe-backup-agent/internal/application"

	"github.com/spf13/cobra"
)

func newStatusCmd(appProvider func() (*application.App, error)) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Muestra el estado operativo del agente y de las copias",
		Long:  "Consulta el estado del último backup, hash SHA-256, sincronizaciones remotas pendientes y estado del lock file.",
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := appProvider()
			if err != nil {
				return err
			}
			report, err := app.Status(cmd.Context())
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "=== Estado del Agente de Backup ===")
			fmt.Fprintf(out, "Base de datos:           %s\n", report.Database)
			fmt.Fprintf(out, "Servidor SQL:            %s\n", report.Server)
			fmt.Fprintf(out, "Directorio local:        %s\n", report.BackupDir)
			fmt.Fprintf(out, "Retención local:         %d copias\n", report.Retain)
			fmt.Fprintf(out, "Última fecha corrida:    %s\n", report.LastRunDate)
			fmt.Fprintf(out, "Último backup local:     %s\n", report.LastBackupFile)
			fmt.Fprintf(out, "SHA-256:                 %s\n", report.SHA256)
			fmt.Fprintf(out, "Sincronización R2 pend.: %v\n", report.PendingSyncR2)
			fmt.Fprintf(out, "Último sync en R2:       %s\n", report.R2LastSyncedFile)
			fmt.Fprintf(out, "Lock activo:             %v\n", report.LockActive)
			return nil
		},
	}
}
