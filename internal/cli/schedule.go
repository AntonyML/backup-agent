package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"femucaribe-backup-agent/internal/config"
	"femucaribe-backup-agent/internal/scheduler"

	"github.com/spf13/cobra"
)

// newScheduleCmd agrupa la gestión de la tarea de Windows por perfil (D2/D7).
func newScheduleCmd(exeDir string, configPath, profileFlag *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "schedule",
		Short: "Gestiona la tarea del Programador de Windows por perfil",
		Long: "Instala, actualiza, elimina o consulta la tarea de Windows de un perfil.\n" +
			"Si falta permiso de administrador se muestra el comando exacto para copiar (nunca un error crudo).",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "install",
		Short: "Crea la tarea del perfil en el Programador de Windows",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runScheduleApply(cmd, exeDir, configPath, profileFlag, "instalada", func(m *scheduler.Manager, spec scheduler.Spec) error {
				return m.Install(cmd.Context(), spec)
			})
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "update",
		Short: "Reinstala la tarea con la configuración actual del perfil",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runScheduleApply(cmd, exeDir, configPath, profileFlag, "actualizada", func(m *scheduler.Manager, spec scheduler.Spec) error {
				return m.Update(cmd.Context(), spec)
			})
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "remove",
		Short: "Elimina la tarea del perfil del Programador de Windows",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig(exeDir, *configPath)
			if err != nil {
				return err
			}
			profile, err := resolveProfile(cfg, *profileFlag)
			if err != nil {
				return err
			}
			taskName := cfg.TaskNameForProfile(profile)
			if err := scheduler.New().Delete(cmd.Context(), taskName); err != nil {
				return handleSchedulerError(cmd, err, scheduler.CommandLineDelete(taskName))
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Tarea %q eliminada.\n", taskName)
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Muestra el estado de la tarea de Windows (todos los perfiles o uno)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runScheduleStatus(cmd, exeDir, configPath, profileFlag)
		},
	})

	return cmd
}

// runScheduleApply ejecuta install/update (idempotentes) y reporta el resultado.
func runScheduleApply(cmd *cobra.Command, exeDir string, configPath, profileFlag *string,
	doneVerb string, apply func(*scheduler.Manager, scheduler.Spec) error) error {
	spec, err := specForProfile(cmd, exeDir, configPath, profileFlag)
	if err != nil {
		return err
	}
	if !spec.Enabled {
		// D9: schedule deshabilitado -> se registra deshabilitada y se avisa.
		fmt.Fprintln(cmd.OutOrStdout(), "Aviso: el schedule del perfil está deshabilitado; la tarea quedará deshabilitada.")
	}
	if err := apply(scheduler.New(), spec); err != nil {
		return handleSchedulerError(cmd, err, scheduler.CommandLine(spec))
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Tarea %q %s (acción: %s).\n",
		spec.TaskName, doneVerb, scheduler.ActionArgs(spec))
	return nil
}

// specForProfile resuelve config, perfil y arma la Spec de la tarea.
func specForProfile(cmd *cobra.Command, exeDir string, configPath, profileFlag *string) (scheduler.Spec, error) {
	cfg, err := loadConfig(exeDir, *configPath)
	if err != nil {
		return scheduler.Spec{}, err
	}
	profile, err := resolveProfile(cfg, *profileFlag)
	if err != nil {
		return scheduler.Spec{}, err
	}
	exePath, err := os.Executable()
	if err != nil {
		return scheduler.Spec{}, fmt.Errorf("no se pudo resolver la ruta del ejecutable: %w", err)
	}
	spec, err := scheduler.SpecForProfile(cfg, profile, exePath)
	if err != nil {
		return scheduler.Spec{}, fmt.Errorf("%w: %v", ErrConfig, err)
	}
	return spec, nil
}

// runScheduleStatus consulta el estado de las tareas de todos los perfiles (o de uno).
func runScheduleStatus(cmd *cobra.Command, exeDir string, configPath, profileFlag *string) error {
	cfg, err := loadConfig(exeDir, *configPath)
	if err != nil {
		return err
	}
	profiles := cfg.Profiles
	if strings.TrimSpace(*profileFlag) != "" {
		name, err := resolveProfile(cfg, *profileFlag)
		if err != nil {
			return err
		}
		p, _ := cfg.ProfileByName(name)
		profiles = []config.Profile{p}
	}

	m := scheduler.New()
	out := cmd.OutOrStdout()
	fmt.Fprintln(out, "=== Estado de tareas de Windows ===")
	for _, p := range profiles {
		taskName := cfg.TaskNameForProfile(p.Name)
		st, err := m.Status(cmd.Context(), taskName)
		if err != nil {
			if handleSchedulerError(cmd, err, "") == nil {
				continue
			}
			return err
		}
		state := "NO INSTALADA"
		if st.Installed {
			state = st.StateText
			if !st.Enabled {
				state += " (deshabilitada)"
			}
		}
		active := " "
		if p.Name == cfg.ActiveProfile {
			active = "*"
		}
		fmt.Fprintf(out, "%s %-16s tarea=%-30s estado=%s\n", active, p.Name, taskName, state)
	}
	return nil
}

// handleSchedulerError aplica la degradación D7: falta de permisos o tarea
// inexistente se informan mostrando el comando exacto, sin error crudo.
// Devuelve nil cuando el caso quedó manejado.
func handleSchedulerError(cmd *cobra.Command, err error, fallbackCommand string) error {
	var perm *scheduler.PermissionError
	if errors.As(err, &perm) {
		command := strings.TrimSpace(perm.Command)
		if command == "" {
			command = fallbackCommand
		}
		out := cmd.OutOrStdout()
		fmt.Fprintln(out, "Se requiere permiso de administrador para gestionar la tarea.")
		fmt.Fprintln(out, "Ejecutá este comando en una consola elevada (copiar y pegar):")
		fmt.Fprintf(out, "\n    %s\n\n", command)
		fmt.Fprintln(out, "El agente sigue operativo: solo quedó sin registrar/actualizar la tarea.")
		return nil
	}
	if errors.Is(err, scheduler.ErrNotInstalled) {
		fmt.Fprintln(cmd.OutOrStdout(), "La tarea no existe en el Programador de Windows.")
		return nil
	}
	return err
}