package cli

import (
	"fmt"

	"femucaribe-backup-agent/internal/config"

	"github.com/spf13/cobra"
)

// newConfigCmd agrupa operaciones sobre config.json (solo lectura por ahora).
func newConfigCmd(exeDir string, configPath *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Operaciones sobre la configuración del agente",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "validate",
		Short: "Valida config.json sin ejecutar ningún backup (exit 2 si es inválida)",
		RunE: func(cmd *cobra.Command, args []string) error {
			path := configPathFor(exeDir, *configPath)
			cfg, err := config.Load(path)
			if err != nil {
				return fmt.Errorf("%w: %v", ErrConfig, err)
			}
			if err := cfg.Validate(); err != nil {
				return fmt.Errorf("%w: %v", ErrConfig, err)
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Configuración válida: %s\n", path)
			fmt.Fprintf(out, "Perfiles: %d (activo: %s)\n", len(cfg.Profiles), cfg.ActiveProfile)
			for _, p := range cfg.Profiles {
				platforms := "solo local"
				if len(p.Platforms) > 0 {
					platforms = fmt.Sprintf("%v", p.Platforms)
				}
				fmt.Fprintf(out, "  - %s (%s): %s · schedule %s · tarea %s\n",
					p.Name, p.Kind, platforms, scheduleSummary(cfg.EffectiveSchedule(p)), cfg.TaskNameForProfile(p.Name))
			}
			fmt.Fprintf(out, "Plataformas habilitadas globalmente: %v\n", cfg.EnabledPlatforms())
			return nil
		},
	})

	return cmd
}