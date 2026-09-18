package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type importSuccessMsg struct {
	Path string
}

type importFailedMsg struct {
	Err error
}

type importModalModel struct {
	app     AppConnector
	styles  Styles
	inputs  []textinput.Model
	focused int
	loading bool
	err     error
	success string
	width   int
	height  int
}

func newImportModalModel(app AppConnector, styles Styles) importModalModel {
	inputs := make([]textinput.Model, 2)

	// 0: Ruta del archivo a importar
	inputs[0] = textinput.New()
	inputs[0].Placeholder = filepath.Join("exports", "backup-agent-config.bacfg")
	inputs[0].SetValue(filepath.Join("exports", "backup-agent-config.bacfg"))
	inputs[0].Focus()
	inputs[0].CharLimit = 260
	inputs[0].SetWidth(46)

	// 1: Contraseña
	inputs[1] = textinput.New()
	inputs[1].Placeholder = "Contraseña de descifrado"
	inputs[1].EchoMode = textinput.EchoPassword
	inputs[1].CharLimit = 100
	inputs[1].SetWidth(46)

	return importModalModel{
		app:     app,
		styles:  styles,
		inputs:  inputs,
		focused: 0,
	}
}

func (m *importModalModel) reset() {
	m.inputs[0].SetValue(filepath.Join("exports", "backup-agent-config.bacfg"))
	m.inputs[1].SetValue("")
	m.focused = 0
	m.loading = false
	m.err = nil
	m.success = ""
	m.updateFocus()
}

func (m *importModalModel) setSize(w, h int) {
	m.width = w
	m.height = h
}

func (m *importModalModel) updateFocus() tea.Cmd {
	cmds := make([]tea.Cmd, 2)
	for i := 0; i < 2; i++ {
		if i == m.focused {
			cmds[i] = m.inputs[i].Focus()
		} else {
			m.inputs[i].Blur()
		}
	}
	return tea.Batch(cmds...)
}

func (m importModalModel) update(msg tea.Msg) (importModalModel, tea.Cmd) {
	switch msg := msg.(type) {
	case importSuccessMsg:
		m.loading = false
		m.success = fmt.Sprintf("Configuración y secretos importados correctamente desde %s", msg.Path)
		m.err = nil
		return m, nil

	case importFailedMsg:
		m.loading = false
		m.err = msg.Err
		return m, nil

	case tea.KeyPressMsg:
		if m.loading {
			return m, nil
		}

		if m.success != "" {
			switch msg.String() {
			case "enter", "esc", "q", " ":
				return m, func() tea.Msg { return backToDashboardMsg{} }
			}
		}

		switch msg.String() {
		case "tab", "down":
			m.focused = (m.focused + 1) % 4
			return m, m.updateFocus()
		case "shift+tab", "up":
			m.focused = (m.focused - 1 + 4) % 4
			return m, m.updateFocus()
		case "enter":
			if m.focused == 0 {
				m.focused = 1
				return m, m.updateFocus()
			}
			if m.focused == 3 { // Botón Cancelar
				return m, func() tea.Msg { return backToDashboardMsg{} }
			}
			return m.submit()
		case "esc":
			return m, func() tea.Msg { return backToDashboardMsg{} }
		}
	}

	if !m.loading && m.focused >= 0 && m.focused < 2 {
		var cmd tea.Cmd
		m.inputs[m.focused], cmd = m.inputs[m.focused].Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m importModalModel) submit() (importModalModel, tea.Cmd) {
	inPath := strings.TrimSpace(m.inputs[0].Value())
	if inPath == "" {
		m.err = fmt.Errorf("ingresá la ruta del archivo a importar")
		m.focused = 0
		return m, m.updateFocus()
	}
	pass := m.inputs[1].Value()
	if pass == "" {
		m.err = fmt.Errorf("ingresá la contraseña de descifrado")
		m.focused = 1
		return m, m.updateFocus()
	}

	m.loading = true
	m.err = nil
	m.success = ""

	return m, func() tea.Msg {
		err := m.app.ImportConfiguration(inPath, pass)
		if err != nil {
			return importFailedMsg{Err: err}
		}
		return importSuccessMsg{Path: inPath}
	}
}

func (m importModalModel) view() string {
	s := m.styles
	var b strings.Builder

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorWhite).
		Background(ColorGreen).
		Padding(0, 1)

	cardStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorGreen).
		Padding(1, 2).
		Width(60)

	btnNormal := lipgloss.NewStyle().
		Foreground(ColorWhite).
		Background(ColorDarkGray).
		Padding(0, 2).
		Bold(true)

	btnFocused := lipgloss.NewStyle().
		Foreground(ColorWhite).
		Background(ColorGreen).
		Padding(0, 2).
		Bold(true)

	labelStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorWhite)

	b.WriteString(titleStyle.Render("IMPORTAR CONFIGURACIÓN") + "\n\n")
	b.WriteString(s.Subtitle.Render("Descifra el paquete, aplica config.json y re-cifra credenciales con DPAPI local.") + "\n\n")

	if m.success != "" {
		b.WriteString(s.Success.Render("✔ "+m.success) + "\n\n")
		b.WriteString(s.Muted.Render("El Dashboard y las tareas programadas se actualizaron automáticamente.") + "\n\n")
		b.WriteString(btnFocused.Render("[ Continuar al Dashboard ]") + "\n\n")
		b.WriteString(s.HelpBar.Render("[Enter/Esc] Volver al Dashboard"))
		card := cardStyle.Render(b.String())
		if m.width > 0 && m.height > 0 {
			return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, card)
		}
		return card
	}

	b.WriteString(labelStyle.Render("Ruta del archivo a importar:") + "\n")
	b.WriteString(m.inputs[0].View() + "\n\n")

	b.WriteString(labelStyle.Render("Contraseña de descifrado:") + "\n")
	b.WriteString(m.inputs[1].View() + "\n\n")

	// Botones
	btnImport := btnNormal.Render("[ Importar ]")
	if m.focused == 2 {
		btnImport = btnFocused.Render("[ Importar ]")
	}
	btnCancel := btnNormal.Render("[ Cancelar ]")
	if m.focused == 3 {
		btnCancel = btnFocused.Render("[ Cancelar ]")
	}
	b.WriteString(fmt.Sprintf("%s   %s\n\n", btnImport, btnCancel))

	// Estado o error
	if m.loading {
		b.WriteString(s.Info.Render("● Descifrando y aplicando configuración...") + "\n")
	} else if m.err != nil {
		b.WriteString(s.Error.Render("✗ "+m.err.Error()) + "\n")
	} else {
		b.WriteString(s.Muted.Render("Asegurate de haber copiado el archivo exportado a esta máquina.") + "\n")
	}

	b.WriteString("\n" + s.HelpBar.Render("[Tab/↑/↓] Moverse  ·  [Enter] Confirmar  ·  [Esc] Cancelar"))

	card := cardStyle.Render(b.String())
	if m.width > 0 && m.height > 0 {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, card)
	}
	return card
}
