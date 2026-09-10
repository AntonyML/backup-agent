package cli

import (
	"path/filepath"

	"femucaribe-backup-agent/internal/secrets"

	"github.com/spf13/cobra"
)

func newConfigureCmd(exeDir string) *cobra.Command {
	return &cobra.Command{
		Use:   "configure",
		Short: "Configura interactivamente las credenciales de Cloudflare R2 con DPAPI",
		Long:  "Solicita Endpoint, Bucket, Access Key y Secret Key de R2, cifrándolos con Windows DPAPI (CURRENT_USER) y guardándolos en config.dat.",
		RunE: func(cmd *cobra.Command, args []string) error {
			datPath := filepath.Join(exeDir, "config.dat")
			return secrets.Configure(datPath, cmd.InOrStdin(), cmd.OutOrStdout())
		},
	}
}
