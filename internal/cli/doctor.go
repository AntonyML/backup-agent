package cli

import (
	"context"
	"fmt"

	"femucaribe-backup-agent/internal/application"
	"femucaribe-backup-agent/internal/scheduler"

	"github.com/spf13/cobra"
)

// newDoctorCmd diagnostica el entorno del perfil: SQL, carpeta local,
// plataformas remotas y estado de la tarea de Windows.
// Usa los flags persistentes --config/--profile del comando raíz.
func newDoctorCmd(exeDir string, appProvider func(cfgPath, profile string) (*application.App, error)) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Diagnostica conectividad y estado del perfil (SQL, plataformas, tarea Windows)",
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := appProvider(configFlagOf(cmd), profileFlagOf(cmd))
			if err != nil {
				return err
			}
			ctx := context.Background()
			out := cmd.OutOrStdout()

			fmt.Fprintf(out, "=== Doctor · perfil %s ===\n\n", app.ProfileName())
			hasFailure := false
			for _, check := range app.CheckPlatforms(ctx) {
				icon := "✔"
				if !check.OK {
					icon = "○"
					hasFailure = true
				}
				fmt.Fprintf(out, "  %s %-16s %s\n", icon, check.Name, check.Detail)
			}

			// Estado de la tarea de Windows de este perfil
			cfg, err := loadConfig(exeDir, configFlagOf(cmd))
			if err != nil {
				return err
			}
			taskName := cfg.TaskNameForProfile(app.ProfileName())
			st, err := scheduler.New().Status(ctx, taskName)
			if err != nil {
				if handled := handleSchedulerError(cmd, err, ""); handled != nil {
					return handled
				}
			} else {
				state := "no instalada"
				if st.Installed {
					state = st.StateText
					if !st.Enabled {
						state += " (deshabilitada)"
					}
				}
				fmt.Fprintf(out, "  ✔ %-16s %s (%s)\n", "Tarea Windows", taskName, state)
			}

			fmt.Fprintln(out)
			if hasFailure {
				fmt.Fprintln(out, "Diagnóstico con observaciones: revisá los ítems marcados con ○.")
				return nil
			}
			fmt.Fprintln(out, "Todo en orden.")
			return nil
		},
	}
}

// configFlagOf y profileFlagOf leen los flags persistentes heredados.
func configFlagOf(cmd *cobra.Command) string {
	v, err := cmd.Flags().GetString("config")
	if err != nil {
		return ""
	}
	return v
}

func profileFlagOf(cmd *cobra.Command) string {
	v, err := cmd.Flags().GetString("profile")
	if err != nil {
		return ""
	}
	return v
}