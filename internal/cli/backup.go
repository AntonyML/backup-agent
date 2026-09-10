package cli

import (
	"femucaribe-backup-agent/internal/application"

	"github.com/spf13/cobra"
)

func newBackupCmd(appProvider func() (*application.App, error)) *cobra.Command {
	var (
		unattended bool
		force      bool
	)

	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Ejecuta el pipeline de backup (local + R2)",
		Long:  "Genera el backup de la base de datos SQL Server, valida integridad con RESTORE VERIFYONLY, calcula SHA-256, rota copias locales y sube a Cloudflare R2.",
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := appProvider()
			if err != nil {
				return err
			}
			return app.Backup(cmd.Context(), application.BackupOptions{
				Force: force,
			})
		},
	}

	cmd.Flags().BoolVar(&unattended, "unattended", false, "modo desatendido para Task Scheduler (sin interacción)")
	cmd.Flags().BoolVar(&force, "force", false, "forzar ejecución manual aunque ya exista backup del día")

	return cmd
}
