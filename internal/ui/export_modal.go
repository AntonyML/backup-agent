package ui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type exportSuccessMsg struct {
	Path string
}

type exportFailedMsg struct {
	Err error
}

type exportModalModel struct {
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

func newExportModalModel(app AppConnector, styles Styles) exportModalModel {
	inputs := make([]textinput.Model, 3)

	// 0: Ruta de salida
	inputs[0] = textinput.New()
	inputs[0].Placeholder = "backup-agent-config.bacfg"
	inputs[0].SetValue("backup-agent-config.bacfg")
	inputs[0].Focus()
	inputs[0].CharLimit = 260
	inputs[0].SetWidth(46)

	// 1: Contraseña
	inputs[1] = textinput.New()
	inputs[1].Placeholder = "Contraseña temporal (mín. 4 caracteres)"
	inputs[1].EchoMode = textinput.EchoPassword
	inputs[1].CharLimit = 100
	inputs[1].SetWidth(46)

	// 2: Confirmación
	inputs[2] = textinput.New()
	inputs[2].Placeholder = "Confirmar contraseña"
	inputs[2].EchoMode = textinput.EchoPassword
	inputs[2].CharLimit = 100
	inputs[2].SetWidth(46)

	return exportModalModel{
		app:     app,
		styles:  styles,
		inputs:  inputs,
		focused: 0,
	}
}

func (m *exportModalModel) reset() {
	m.inputs[0].SetValue("backup-agent-config.bacfg")
	m.inputs[1].SetValue("")
	m.inputs[2].SetValue("")
	m.focused = 0
	m.loading = false
	m.err = nil
	m.success = ""
	m.updateFocus()
}

func (m *exportModalModel) setSize(w, h int) {
	m.width = w
	m.height = h
}

func (m *exportModalModel) updateFocus() tea.Cmd {
	cmds := make([]tea.Cmd, 3)
	for i := 0; i < 3; i++ {
		if i == m.focused {
			cmds[i] = m.inputs[i].Focus()
		} else {
			m.inputs[i].Blur()
		}
	}
	return tea.Batch(cmds...)
}

func (m exportModalModel) update(msg tea.Msg) (exportModalModel, tea.Cmd) {
	switch msg := msg.(type) {
	case exportSuccessMsg:
		m.loading = false
		m.success = fmt.Sprintf("Configuración exportada exitosamente en %s", msg.Path)
		m.err = nil
		return m, nil

	case exportFailedMsg:
		m.loading = false
		m.err = msg.Err
		return m, nil

	case tea.KeyPressMsg:
		if m.loading {
			return m, nil
		}

		// Si ya se exportó con éxito, cualquier tecla (Enter, Esc, etc.) vuelve
		if m.success != "" {
			switch msg.String() {
			case "enter", "esc", "q", " " :
				return m, func() tea.Msg { return backToDashboardMsg{} }
			}
		}

		switch msg.String() {
		case "tab", "down":
			m.focused = (m.focused + 1) % 5
			return m, m.updateFocus()
		case "shift+tab", "up":
			m.focused = (m.focused - 1 + 5) % 5
			return m, m.updateFocus()
		case "enter":
			if m.focused < 2 {
				m.focused++
				return m, m.updateFocus()
			}
			if m.focused == 4 { // Botón Cancelar
				return m, func() tea.Msg { return backToDashboardMsg{} }
			}
			return m.submit()
		case "esc":
			return m, func() tea.Msg { return backToDashboardMsg{} }
		}
	}

	if !m.loading && m.focused >= 0 && m.focused < 3 {
		var cmd tea.Cmd
		m.inputs[m.focused], cmd = m.inputs[m.focused].Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m exportModalModel) submit() (exportModalModel, tea.Cmd) {
	outPath := strings.TrimSpace(m.inputs[0].Value())
	if outPath == "" {
		outPath = "backup-agent-config.bacfg"
	}
	pass1 := m.inputs[1].Value()
	pass2 := m.inputs[2].Value()

	if pass1 == "" {
		m.err = fmt.Errorf("ingresá una contraseña temporal")
		m.focused = 1
		return m, m.updateFocus()
	}
	if len(pass1) < 4 {
		m.err = fmt.Errorf("la contraseña debe tener al menos 4 caracteres")
		m.focused = 1
		return m, m.updateFocus()
	}
	if pass1 != pass2 {
		m.err = fmt.Errorf("las contraseñas no coinciden")
		m.focused = 2
		return m, m.updateFocus()
	}

	m.loading = true
	m.err = nil
	m.success = ""

	return m, func() tea.Msg {
		err := m.app.ExportConfiguration(outPath, pass1)
		if err != nil {
			return exportFailedMsg{Err: err}
		}
		return exportSuccessMsg{Path: outPath}
	}
}

func (m exportModalModel) view() string {
	s := m.styles
	var b strings.Builder

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorWhite).
		Background(ColorBlue).
		Padding(0, 1)

	cardStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorBlue).
		Padding(1, 2).
		Width(60)

	btnNormal := lipgloss.NewStyle().
		Foreground(ColorWhite).
		Background(ColorDarkGray).
		Padding(0, 2).
		Bold(true)

	btnFocused := lipgloss.NewStyle().
		Foreground(ColorWhite).
		Background(ColorBlue).
		Padding(0, 2).
		Bold(true)

	btnSuccess := lipgloss.NewStyle().
		Foreground(ColorWhite).
		Background(ColorGreen).
		Padding(0, 2).
		Bold(true)

	labelStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorWhite)

	b.WriteString(titleStyle.Render("EXPORTAR CONFIGURACIÓN Y SECRETOS") + "\n\n")
	b.WriteString(s.Subtitle.Render("Empaqueta config.json y credenciales en un archivo cifrado con AES-256-GCM.") + "\n\n")

	if m.success != "" {
		b.WriteString(s.Success.Render("✔ "+m.success) + "\n\n")
		b.WriteString(s.Muted.Render("Podés copiar este archivo a otra máquina y usar la opción [I] Importar.") + "\n\n")
		b.WriteString(btnSuccess.Render("[ Continuar ]") + "\n\n")
		b.WriteString(s.HelpBar.Render("[Enter/Esc] Volver al Dashboard"))
		card := cardStyle.Render(b.String())
		if m.width > 0 && m.height > 0 {
			return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, card)
		}
		return card
	}

	b.WriteString(labelStyle.Render("Ruta de archivo destino:") + "\n")
	b.WriteString(m.inputs[0].View() + "\n\n")

	b.WriteString(labelStyle.Render("Contraseña temporal de cifrado:") + "\n")
	b.WriteString(m.inputs[1].View() + "\n\n")

	b.WriteString(labelStyle.Render("Confirmar contraseña:") + "\n")
	b.WriteString(m.inputs[2].View() + "\n\n")

	// Botones
	btnExport := btnNormal.Render("[ Exportar ]")
	if m.focused == 3 {
		btnExport = btnFocused.Render("[ Exportar ]")
	}
	btnCancel := btnNormal.Render("[ Cancelar ]")
	if m.focused == 4 {
		btnCancel = btnFocused.Render("[ Cancelar ]")
	}
	b.WriteString(fmt.Sprintf("%s   %s\n\n", btnExport, btnCancel))

	// Estado o error
	if m.loading {
		b.WriteString(s.Info.Render("● Cifrando y exportando configuración...") + "\n")
	} else if m.err != nil {
		b.WriteString(s.Error.Render("✗ "+m.err.Error()) + "\n")
	} else {
		b.WriteString(s.Muted.Render("La contraseña será necesaria al importar el archivo en la otra computadora.") + "\n")
	}

	b.WriteString("\n" + s.HelpBar.Render("[Tab/↑/↓] Moverse  ·  [Enter] Confirmar  ·  [Esc] Cancelar"))

	card := cardStyle.Render(b.String())
	if m.width > 0 && m.height > 0 {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, card)
	}
	return card
}
