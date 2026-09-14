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

type syncFinishedMsg struct {
	err error
}

type syncProgressModel struct {
	styles  Styles
	spinner spinner.Model
	running bool
	done    bool
	err     error
}

func newSyncProgressModel(styles Styles) syncProgressModel {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = styles.Spinner

	return syncProgressModel{
		styles:  styles,
		spinner: sp,
	}
}

func (m *syncProgressModel) start() tea.Cmd {
	m.running = true
	m.done = false
	m.err = nil
	return m.spinner.Tick
}

func (m syncProgressModel) update(msg tea.Msg) (syncProgressModel, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		if m.running {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
	case syncFinishedMsg:
		m.running = false
		m.done = true
		m.err = msg.err
		return m, nil
	}
	return m, nil
}

func (m syncProgressModel) view() string {
	s := m.styles
	var b strings.Builder

	title := s.AppTitle.Render("SINCRONIZACIÓN REMOTA A R2")
	b.WriteString(fmt.Sprintf("%s\n\n", title))

	if m.running {
		spin := m.spinner.View()
		b.WriteString(fmt.Sprintf("%s %s\n\n", spin, s.Info.Render("Subiendo backup pendiente a Cloudflare R2...")))
		b.WriteString(s.Muted.Render("Verificando tamaño y rotación remota..."))
		b.WriteString("\n\n")
	} else if m.done {
		b.WriteString(s.SectionHeader.Render("RESULTADO DE LA SINCRONIZACIÓN"))
		b.WriteString("\n\n")
		if m.err != nil {
			b.WriteString(fmt.Sprintf("  %s %s\n\n", s.Error.Render("✖ ERROR:"), s.Value.Render(m.err.Error())))
		} else {
			b.WriteString(s.Success.Render("✔ Sincronización a Cloudflare R2 completada exitosamente."))
			b.WriteString("\n\n")
		}

		keys := []string{
			fmt.Sprintf("%s %s", s.Key.Render("[Esc/Enter]"), s.Desc.Render("Volver al Dashboard")),
		}
		b.WriteString(s.HelpBar.Render(strings.Join(keys, "  ")))
	}

	return s.Box.Render(b.String())
}

func runSyncCmd(app *application.App) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		err := app.Sync(ctx, application.SyncOptions{Force: true})
		return syncFinishedMsg{err: err}
	}
}
