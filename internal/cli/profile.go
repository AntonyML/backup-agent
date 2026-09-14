package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"femucaribe-backup-agent/internal/config"

	"github.com/spf13/cobra"
)

// newProfileCmd agrupa la gestión de perfiles: list, use, show.
func newProfileCmd(exeDir string, configPath, profileFlag *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "profile",
		Short: "Gestiona los perfiles de backup (listar, activar, ver)",
		Long: "Los perfiles definen qué plataformas recibe cada backup y su programación.\n" +
			"El destino local está siempre implícito; las plataformas remotas se eligen por perfil (D10).",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "Lista los perfiles configurados y marca el activo",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig(exeDir, *configPath)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "=== Perfiles de backup ===")
			for _, p := range cfg.Profiles {
				marker := "  "
				if p.Name == cfg.ActiveProfile {
					marker = "* "
				}
				platforms := "solo local"
				if len(p.Platforms) > 0 {
					platforms = strings.Join(p.Platforms, ", ")
				}
				sched := cfg.EffectiveSchedule(p)
				fmt.Fprintf(out, "%s%-16s kind=%-8s plataformas=%-32s schedule=%s\n",
					marker, p.Name, p.Kind, platforms, scheduleSummary(sched))
			}
			fmt.Fprintf(out, "\nPerfil activo: %s\n", cfg.ActiveProfile)
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "show [nombre]",
		Short: "Muestra el detalle de un perfil (default: --profile o el activo)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig(exeDir, *configPath)
			if err != nil {
				return err
			}
			name := cfg.ActiveProfile
			if strings.TrimSpace(*profileFlag) != "" {
				name = strings.TrimSpace(*profileFlag)
			}
			if len(args) == 1 {
				name = args[0]
			}
			p, ok := cfg.ProfileByName(name)
			if !ok {
				return fmt.Errorf("%w: el perfil %q no existe", ErrConfig, name)
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "=== Perfil %s ===\n", p.Name)
			fmt.Fprintf(out, "Tipo (kind):       %s\n", p.Kind)
			platforms := []string{config.PlatformLocal}
			platforms = append(platforms, p.Platforms...)
			fmt.Fprintf(out, "Plataformas:       %s\n", strings.Join(platforms, ", "))
			sched := cfg.EffectiveSchedule(p)
			fmt.Fprintf(out, "Schedule:          %s\n", scheduleSummary(sched))
			fmt.Fprintf(out, "Hereda del global: %v\n", p.Schedule == nil)
			fmt.Fprintf(out, "Tarea Windows:     %s\n", cfg.TaskNameForProfile(p.Name))
			if p.Overrides.Cloudflare != nil {
				fmt.Fprintf(out, "Override R2:       %s\n", overrideCloudflareSummary(p.Overrides.Cloudflare))
			}
			if p.Overrides.RemoteServer != nil {
				fmt.Fprintf(out, "Override UNC:      %s\n", overrideServerSummary(p.Overrides.RemoteServer))
			}
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "use <nombre>",
		Short: "Fija el perfil activo en config.json",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig(exeDir, *configPath)
			if err != nil {
				return err
			}
			name := args[0]
			if _, ok := cfg.ProfileByName(name); !ok {
				return fmt.Errorf("%w: el perfil %q no existe (disponibles: %s)",
					ErrConfig, name, strings.Join(profileNames(cfg), ", "))
			}
			cfg.ActiveProfile = name
			path := configPathFor(exeDir, *configPath)
			if err := config.Save(path, cfg); err != nil {
				return fmt.Errorf("%w: %v", ErrConfig, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Perfil activo cambiado a %q (guardado en %s).\n", name, path)
			fmt.Fprintln(cmd.OutOrStdout(), "Recordá que los backends y credenciales se rearman al reiniciar el agente.")
			return nil
		},
	})

	return cmd
}

// loadConfig carga y valida la config resolviendo la ruta efectiva.
func loadConfig(exeDir, configPath string) (config.Config, error) {
	cfg, err := config.Load(configPathFor(exeDir, configPath))
	if err != nil {
		return config.Config{}, fmt.Errorf("%w: %v", ErrConfig, err)
	}
	return cfg, nil
}

func configPathFor(exeDir, configPath string) string {
	if strings.TrimSpace(configPath) != "" {
		return configPath
	}
	return filepath.Join(exeDir, "config.json")
}

// scheduleSummary resume un schedule para impresión por consola.
func scheduleSummary(s config.ScheduleConfig) string {
	if !s.Enabled {
		return "deshabilitado"
	}
	switch s.Mode {
	case "interval":
		return fmt.Sprintf("cada %d min", s.IntervalMinutes)
	case "weekly":
		return fmt.Sprintf("weekly %s a las %s", strings.Join(s.Weekdays, ","), s.TimeOfDay)
	default:
		return "diario a las " + s.TimeOfDay
	}
}

func overrideCloudflareSummary(o *config.PlatformCloudflareOverride) string {
	parts := []string{}
	if o.Keep != nil {
		parts = append(parts, fmt.Sprintf("keep=%d", *o.Keep))
	}
	if o.TimeoutSec != nil {
		parts = append(parts, fmt.Sprintf("timeout_sec=%d", *o.TimeoutSec))
	}
	if o.UploadRetries != nil {
		parts = append(parts, fmt.Sprintf("upload_retries=%d", *o.UploadRetries))
	}
	return strings.Join(parts, " ")
}

func overrideServerSummary(o *config.PlatformServerOverride) string {
	parts := []string{}
	if o.Keep != nil {
		parts = append(parts, fmt.Sprintf("keep=%d", *o.Keep))
	}
	if o.TimeoutSec != nil {
		parts = append(parts, fmt.Sprintf("timeout_sec=%d", *o.TimeoutSec))
	}
	return strings.Join(parts, " ")
}