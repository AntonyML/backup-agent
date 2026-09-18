package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"femucaribe-backup-agent/internal/config"
	"femucaribe-backup-agent/internal/portability"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// newConfigCmd agrupa operaciones sobre la configuración del agente.
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

	var exportPass string
	exportCmd := &cobra.Command{
		Use:   "export [ruta-destino]",
		Short: "Exporta la configuración y credenciales a un archivo cifrado (.bacfg)",
		Long:  "Empaqueta config.json y las credenciales de R2 en un archivo cifrado con AES-256-GCM para transporte seguro a otra máquina.",
		RunE: func(cmd *cobra.Command, args []string) error {
			outPath := "backup-agent-config.bacfg"
			if len(args) > 0 && strings.TrimSpace(args[0]) != "" {
				outPath = strings.TrimSpace(args[0])
			}
			cfgPath := configPathFor(exeDir, *configPath)
			datPath := filepath.Join(exeDir, "config.dat")

			pass := exportPass
			if pass == "" {
				p, err := promptPassword(cmd.InOrStdin(), cmd.OutOrStdout(), "Ingrese contraseña temporal para cifrar el archivo: ")
				if err != nil {
					return err
				}
				pass = p
			}

			if err := portability.Export(cfgPath, datPath, outPath, pass); err != nil {
				return fmt.Errorf("exportar configuración: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Configuración exportada exitosamente a: %s\n", outPath)
			return nil
		},
	}
	exportCmd.Flags().StringVarP(&exportPass, "password", "p", "", "Contraseña para cifrar el archivo")
	cmd.AddCommand(exportCmd)

	var importPass string
	importCmd := &cobra.Command{
		Use:   "import <ruta-archivo>",
		Short: "Importa una configuración cifrada y re-cifra credenciales con DPAPI local",
		Long:  "Descifra un archivo de configuración .bacfg y lo aplica localmente, re-cifrando automáticamente los secretos con el DPAPI de esta máquina.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			inPath := strings.TrimSpace(args[0])
			cfgPath := configPathFor(exeDir, *configPath)
			datPath := filepath.Join(exeDir, "config.dat")

			pass := importPass
			if pass == "" {
				p, err := promptPassword(cmd.InOrStdin(), cmd.OutOrStdout(), "Ingrese contraseña de descifrado: ")
				if err != nil {
					return err
				}
				pass = p
			}

			importedCfg, err := portability.Import(inPath, pass, cfgPath, datPath)
			if err != nil {
				return fmt.Errorf("importar configuración: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Configuración importada exitosamente desde %s (Servidor: %s, Base de datos: %s, Perfil activo: %s)\n",
				inPath, importedCfg.Server, importedCfg.Database, importedCfg.ActiveProfile)
			return nil
		},
	}
	importCmd.Flags().StringVarP(&importPass, "password", "p", "", "Contraseña para descifrar el archivo")
	cmd.AddCommand(importCmd)

	return cmd
}

func promptPassword(in io.Reader, out io.Writer, label string) (string, error) {
	fmt.Fprint(out, label)
	if f, ok := in.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		bytes, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(out)
		if err != nil {
			return "", err
		}
		return string(bytes), nil
	}
	reader := bufio.NewReader(in)
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimSpace(line), nil
}