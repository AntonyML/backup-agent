package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"femucaribe-backup-agent/internal/application"
	"femucaribe-backup-agent/internal/secrets"

	"github.com/spf13/cobra"
)

func newInteractiveCmd(exeDir string, appProvider func() (*application.App, error)) *cobra.Command {
	return &cobra.Command{
		Use:   "interactive",
		Short: "Inicia el menú interactivo por consola",
		Long:  "Despliega un menú interactivo para configurar credenciales, disparar backups, consultar estado o logs.",
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := appProvider()
			if err != nil {
				return err
			}
			return runInteractive(cmd.Context(), app, exeDir, cmd.InOrStdin(), cmd.OutOrStdout())
		},
	}
}

func runInteractive(ctx context.Context, app *application.App, exeDir string, in io.Reader, out io.Writer) error {
	reader := bufio.NewReader(in)

	for {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "==========================================")
		fmt.Fprintln(out, "    FEMUCARIBE Backup Agent — Menú        ")
		fmt.Fprintln(out, "==========================================")
		fmt.Fprintln(out, "1. Configurar credenciales")
		fmt.Fprintln(out, "2. Ejecutar backup ahora")
		fmt.Fprintln(out, "3. Ver estado")
		fmt.Fprintln(out, "4. Ver logs recientes")
		fmt.Fprintln(out, "5. Forzar sincronización pendiente")
		fmt.Fprintln(out, "6. Salir")
		fmt.Fprintln(out)
		fmt.Fprint(out, "Seleccione una opción (1-6): ")

		choice, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		choice = strings.TrimSpace(choice)

		switch choice {
		case "1":
			datPath := filepath.Join(exeDir, "config.dat")
			if err := secrets.Configure(datPath, in, out); err != nil {
				fmt.Fprintf(out, "Error en configuración: %v\n", err)
			}
		case "2":
			fmt.Fprintln(out, "Iniciando backup manual...")
			if err := app.Backup(ctx, application.BackupOptions{Force: true}); err != nil {
				if errors.Is(err, application.ErrPendingSync) {
					fmt.Fprintln(out, "Backup local completado con éxito. Sincronización a R2 pendiente.")
				} else {
					fmt.Fprintf(out, "Error ejecutando backup: %v\n", err)
				}
			} else {
				fmt.Fprintln(out, "Backup completado exitosamente.")
			}
		case "3":
			report, err := app.Status(ctx)
			if err != nil {
				fmt.Fprintf(out, "Error consultando estado: %v\n", err)
			} else {
				fmt.Fprintln(out)
				fmt.Fprintln(out, "--- Estado Actual ---")
				fmt.Fprintf(out, "Base de datos:           %s\n", report.Database)
				fmt.Fprintf(out, "Servidor:                %s\n", report.Server)
				fmt.Fprintf(out, "Directorio:              %s\n", report.BackupDir)
				fmt.Fprintf(out, "Última corrida:          %s\n", report.LastRunDate)
				fmt.Fprintf(out, "Último backup:           %s\n", report.LastBackupFile)
				fmt.Fprintf(out, "SHA-256:                 %s\n", report.SHA256)
				fmt.Fprintf(out, "Pendiente R2:            %v\n", report.PendingSyncR2)
				fmt.Fprintf(out, "Último sync R2:          %s\n", report.R2LastSyncedFile)
				fmt.Fprintf(out, "Lock activo:             %v\n", report.LockActive)
			}
		case "4":
			lines, err := app.TailLogs(ctx, 20)
			if err != nil {
				fmt.Fprintf(out, "Error leyendo logs: %v\n", err)
			} else {
				fmt.Fprintln(out)
				fmt.Fprintln(out, "--- Registros Recientes ---")
				for _, line := range lines {
					fmt.Fprintln(out, line)
				}
			}
		case "5":
			fmt.Fprintln(out, "Iniciando sincronización pendiente...")
			if err := app.Sync(ctx, application.SyncOptions{Force: true}); err != nil {
				if errors.Is(err, application.ErrNoPendingBackup) {
					fmt.Fprintln(out, "No hay ningún backup pendiente de sincronizar.")
				} else {
					fmt.Fprintf(out, "Error sincronizando: %v\n", err)
				}
			} else {
				fmt.Fprintln(out, "Sincronización completada exitosamente.")
			}
		case "6", "q", "exit":
			fmt.Fprintln(out, "Saliendo...")
			return nil
		default:
			if choice == "" && errors.Is(err, io.EOF) {
				return nil
			}
			fmt.Fprintf(out, "Opción inválida: %q. Ingrese un número de 1 a 6.\n", choice)
		}
	}
}
