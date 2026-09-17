package cli

import (
	"context"
	"io"

	tea "charm.land/bubbletea/v2"
	"femucaribe-backup-agent/internal/application"
	"femucaribe-backup-agent/internal/auth"
	"femucaribe-backup-agent/internal/ui"
	"github.com/spf13/cobra"
)

func newInteractiveCmd(exeDir string, appProvider func() (*application.App, error)) *cobra.Command {
	return &cobra.Command{
		Use:     "interactive",
		Aliases: []string{"tui"},
		Short:   "Inicia la interfaz gráfica de terminal (TUI)",
		Long:    "Despliega el dashboard interactivo de terminal para monitorear backups, ejecutar copias y configurar credenciales.",
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
	if app != nil {
		restore := app.MuteConsole()
		defer restore()
	}

	var opts []tea.ProgramOption
	if in != nil {
		opts = append(opts, tea.WithInput(in))
	}
	if out != nil {
		opts = append(opts, tea.WithOutput(out))
	}
	if ctx != nil {
		opts = append(opts, tea.WithContext(ctx))
	}

	var authMgr *auth.Manager
	if app != nil {
		s := app.GetSettings()
		if s.Supabase.Enabled {
			authMgr = auth.NewManager(s.Supabase.URL, s.Supabase.APIKey, "", nil)
		}
	}

	p := tea.NewProgram(ui.NewApp(app, exeDir, authMgr), opts...)
	_, err := p.Run()
	return err
}
