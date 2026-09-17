package ui

import (
	"context"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"femucaribe-backup-agent/internal/auth"
)

type loginSuccessMsg struct {
	Session *auth.Session
}

type loginFailedMsg struct {
	Err error
}

type logoutMsg struct{}

type loginModel struct {
	authMgr *auth.Manager
	styles  Styles
	inputs  []textinput.Model
	focused int
	loading bool
	err     error
	width   int
	height  int
}

func newLoginModel(authMgr *auth.Manager, styles Styles) loginModel {
	inputs := make([]textinput.Model, 2)

	// 0: Correo electrónico
	inputs[0] = textinput.New()
	inputs[0].Placeholder = "correo@empresa.com"
	inputs[0].Focus()
	inputs[0].CharLimit = 120
	inputs[0].SetWidth(42)

	// 1: Contraseña
	inputs[1] = textinput.New()
	inputs[1].Placeholder = "Contraseña"
	inputs[1].EchoMode = textinput.EchoPassword
	inputs[1].CharLimit = 100
	inputs[1].SetWidth(42)

	return loginModel{
		authMgr: authMgr,
		styles:  styles,
		inputs:  inputs,
		focused: 0,
	}
}

func (m *loginModel) setSize(w, h int) {
	m.width = w
	m.height = h
}

func (m loginModel) update(msg tea.Msg) (loginModel, tea.Cmd) {
	switch msg := msg.(type) {
	case loginFailedMsg:
		m.loading = false
		m.err = msg.Err
		return m, nil

	case tea.KeyPressMsg:
		if m.loading {
			return m, nil
		}

		switch msg.String() {
		case "tab", "down":
			m.focused = (m.focused + 1) % 3
			return m, m.updateFocus()
		case "shift+tab", "up":
			m.focused = (m.focused - 1 + 3) % 3
			return m, m.updateFocus()
		case "enter":
			if m.focused == 0 {
				m.focused = 1
				return m, m.updateFocus()
			}
			// Submit si está en Contraseña o en el botón
			return m.submit()
		case "esc":
			return m, tea.Quit
		}
	}

	if !m.loading && m.focused >= 0 && m.focused < 2 {
		var cmd tea.Cmd
		m.inputs[m.focused], cmd = m.inputs[m.focused].Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m *loginModel) updateFocus() tea.Cmd {
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

func (m loginModel) submit() (loginModel, tea.Cmd) {
	email := strings.TrimSpace(m.inputs[0].Value())
	password := m.inputs[1].Value()

	if email == "" {
		m.err = fmt.Errorf("ingresá tu correo electrónico")
		m.focused = 0
		return m, m.updateFocus()
	}
	if password == "" {
		m.err = fmt.Errorf("ingresá tu contraseña")
		m.focused = 1
		return m, m.updateFocus()
	}

	m.loading = true
	m.err = nil

	return m, func() tea.Msg {
		if m.authMgr == nil {
			return loginFailedMsg{Err: fmt.Errorf("módulo de autenticación no disponible")}
		}
		sess, err := m.authMgr.Login(context.Background(), email, password)
		if err != nil {
			return loginFailedMsg{Err: err}
		}
		return loginSuccessMsg{Session: sess}
	}
}

func (m loginModel) view() string {
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
		Width(56)

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

	labelStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorWhite)

	b.WriteString(titleStyle.Render("BACKUP AGENT ENTERPRISE") + "\n\n")
	b.WriteString(s.Subtitle.Render("Autenticación obligatoria de operador") + "\n\n")

	b.WriteString(labelStyle.Render("Correo electrónico:") + "\n")
	b.WriteString(m.inputs[0].View() + "\n\n")

	b.WriteString(labelStyle.Render("Contraseña:") + "\n")
	b.WriteString(m.inputs[1].View() + "\n\n")

	// Botón de Iniciar Sesión
	if m.focused == 2 {
		b.WriteString(btnFocused.Render("[ Iniciar Sesión ]"))
	} else {
		b.WriteString(btnNormal.Render("[ Iniciar Sesión ]"))
	}
	b.WriteString("\n\n")

	// Estado o error
	if m.loading {
		b.WriteString(s.Info.Render("● Conectando con Supabase Auth...") + "\n")
	} else if m.err != nil {
		b.WriteString(s.Error.Render("✗ "+m.err.Error()) + "\n")
	} else {
		b.WriteString(s.Muted.Render("Ingresá las credenciales autorizadas por tu administrador.") + "\n")
	}

	b.WriteString("\n" + s.HelpBar.Render("[Tab/↑/↓] Moverse  ·  [Enter] Confirmar  ·  [Esc] Salir"))

	card := cardStyle.Render(b.String())

	// Centrar en pantalla
	if m.width > 0 && m.height > 0 {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, card)
	}
	return card
}
