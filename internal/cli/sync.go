package cli

import (
	"femucaribe-backup-agent/internal/application"

	"github.com/spf13/cobra"
)

func newSyncCmd(appProvider func() (*application.App, error)) *cobra.Command {
	var (
		unattended bool
		force      bool
	)

	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Fuerza la sincronización remota de backups pendientes",
		Long:  "Intenta subir a Cloudflare R2 cualquier backup local que haya quedado marcado como pendiente por fallas previas de red o timeout.",
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := appProvider()
			if err != nil {
				return err
			}
			return app.Sync(cmd.Context(), application.SyncOptions{
				Force: force,
			})
		},
	}

	cmd.Flags().BoolVar(&unattended, "unattended", false, "modo desatendido (sin prompts ni menús)")
	cmd.Flags().BoolVar(&force, "force", false, "forzar sincronización aunque no esté marcado como pendiente")

	return cmd
}
