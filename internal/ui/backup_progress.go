package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"femucaribe-backup-agent/internal/application"
)

type backupFinishedMsg struct {
	err error
}

type backupProgressModel struct {
	styles  Styles
	spinner spinner.Model
	running bool
	done    bool
	err     error
}

func newBackupProgressModel(styles Styles) backupProgressModel {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = styles.Spinner

	return backupProgressModel{
		styles:  styles,
		spinner: sp,
	}
}

func (m *backupProgressModel) start() tea.Cmd {
	m.running = true
	m.done = false
	m.err = nil
	return m.spinner.Tick
}

func (m backupProgressModel) update(msg tea.Msg) (backupProgressModel, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		if m.running {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
	case backupFinishedMsg:
		m.running = false
		m.done = true
		m.err = msg.err
		return m, nil
	}
	return m, nil
}

func (m backupProgressModel) view() string {
	s := m.styles
	var b strings.Builder

	title := s.AppTitle.Render("EJECUCIÃ“N DE BACKUP")
	b.WriteString(fmt.Sprintf("%s\n\n", title))

	if m.running {
		spin := m.spinner.View()
		b.WriteString(fmt.Sprintf("%s %s\n\n", spin, s.Info.Render("Ejecutando pipeline de backup en segundo plano...")))

		b.WriteString(s.SectionHeader.Render("ETAPAS DEL PROCESO"))
		b.WriteString("\n")
		b.WriteString(fmt.Sprintf("  %s %s\n", s.Label.Render("SQL Server:"), s.Value.Render("BACKUP DATABASE y RESTORE VERIFYONLY")))
		b.WriteString(fmt.Sprintf("  %s %s\n", s.Label.Render("Copia Local:"), s.Value.Render("Hash SHA-256 y rotaciÃ³n (3 copias)")))
		b.WriteString(fmt.Sprintf("  %s %s\n\n", s.Label.Render("Cloudflare R2:"), s.Value.Render("Subida y verificaciÃ³n de integridad")))

		b.WriteString(s.Muted.Render("Por favor esperÃ¡, este proceso puede tardar unos minutos segÃºn el tamaÃ±o de la base..."))
		b.WriteString("\n\n")
	} else if m.done {
		b.WriteString(s.SectionHeader.Render("RESULTADO DE LA OPERACIÃ“N"))
		b.WriteString("\n\n")
		if m.err != nil {
			b.WriteString(fmt.Sprintf("  %s %s\n\n", s.Error.Render("âœ– ERROR:"), s.Value.Render(m.err.Error())))
			b.WriteString(s.Muted.Render("RevisÃ¡ los logs con [L] o verificÃ¡ la conectividad a SQL / R2."))
			b.WriteString("\n\n")
		} else {
			b.WriteString(fmt.Sprintf("  %s %s\n", s.Success.Render("âœ” SQL Server:"), s.Value.Render("Backup verificado con Ã©xito")))
			b.WriteString(fmt.Sprintf("  %s %s\n", s.Success.Render("âœ” Copia Local:"), s.Value.Render("SHA-256 generado y rotaciÃ³n completada")))
			b.WriteString(fmt.Sprintf("  %s %s\n\n", s.Success.Render("âœ” Cloudflare R2:"), s.Value.Render("SincronizaciÃ³n remota confirmada")))
			b.WriteString(s.Success.Render("El proceso de backup finalizÃ³ exitosamente."))
			b.WriteString("\n\n")
		}

		keys := []string{
			fmt.Sprintf("%s %s", s.Key.Render("[Esc/Enter]"), s.Desc.Render("Volver al Dashboard")),
		}
		b.WriteString(s.HelpBar.Render(strings.Join(keys, "  ")))
	}

	return s.Box.Render(b.String())
}

func runBackupCmd(app AppConnector) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
		err := app.Backup(ctx, application.BackupOptions{Force: true})
		return backupFinishedMsg{err: err}
	}
}

