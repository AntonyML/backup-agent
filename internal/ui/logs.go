package ui

import (
	"context"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
)

type logsModel struct {
	app      AppConnector
	styles   Styles
	viewport viewport.Model
	ready    bool
	err      error
}

func newLogsModel(app AppConnector, styles Styles) logsModel {
	vp := viewport.New()
	vp.SetWidth(80)
	vp.SetHeight(20)

	return logsModel{
		app:      app,
		styles:   styles,
		viewport: vp,
	}
}

func (m *logsModel) setSize(w, h int) {
	vpWidth := w - 6
	if vpWidth < 40 {
		vpWidth = 40
	}
	vpHeight := h - 8
	if vpHeight < 10 {
		vpHeight = 10
	}

	m.viewport.SetWidth(vpWidth)
	m.viewport.SetHeight(vpHeight)
	m.ready = true
}

func (m *logsModel) loadLogs() {
	if m.app == nil {
		m.viewport.SetContent("No hay instancia de aplicación conectada.")
		return
	}

	lines, err := m.app.TailLogs(context.Background(), 100)
	if err != nil {
		m.err = err
		m.viewport.SetContent(fmt.Sprintf("Error cargando logs: %v", err))
		return
	}

	if len(lines) == 0 {
		m.viewport.SetContent("No hay registros disponibles para el día de hoy.")
		return
	}

	m.viewport.SetContent(strings.Join(lines, "\n"))
	m.viewport.GotoBottom()
}

func (m logsModel) update(msg tea.Msg) (logsModel, tea.Cmd) {
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m logsModel) view() string {
	s := m.styles
	var b strings.Builder

	title := s.AppTitle.Render("REGISTROS DEL AGENTE")
	sub := s.Subtitle.Render("Últimas líneas del log diario (desplazate con las flechas o rueda del mouse)")
	b.WriteString(fmt.Sprintf("%s  %s\n\n", title, sub))

	b.WriteString(m.viewport.View())
	b.WriteString("\n\n")

	keys := []string{
		fmt.Sprintf("%s %s", s.Key.Render("[Esc/Q]"), s.Desc.Render("Volver al Dashboard")),
		fmt.Sprintf("%s %s", s.Key.Render("[R]"), s.Desc.Render("Refrescar")),
		fmt.Sprintf("%s %s", s.Key.Render("[↑/↓/PgUp/PgDn]"), s.Desc.Render("Scroll")),
	}
	b.WriteString(s.HelpBar.Render(strings.Join(keys, "  ")))

	return s.Box.Render(b.String())
}

