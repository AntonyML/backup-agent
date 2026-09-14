package ui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

type configureModel struct {
	app     AppConnector
	styles  Styles
	inputs  []textinput.Model
	focused int
	err     error
	success bool
}

func newConfigureModel(app AppConnector, styles Styles) configureModel {
	inputs := make([]textinput.Model, 4)

	// 0: Endpoint
	inputs[0] = textinput.New()
	inputs[0].Placeholder = "https://<account_id>.r2.cloudflarestorage.com"
	inputs[0].Focus()
	inputs[0].CharLimit = 120
	inputs[0].SetWidth(50)

	// 1: Bucket
	inputs[1] = textinput.New()
	inputs[1].Placeholder = "femucaribe-backups"
	inputs[1].CharLimit = 64
	inputs[1].SetWidth(50)

	// 2: Access Key ID
	inputs[2] = textinput.New()
	inputs[2].Placeholder = "R2 Access Key ID"
	inputs[2].EchoMode = textinput.EchoPassword
	inputs[2].CharLimit = 64
	inputs[2].SetWidth(50)

	// 3: Secret Access Key
	inputs[3] = textinput.New()
	inputs[3].Placeholder = "R2 Secret Access Key"
	inputs[3].EchoMode = textinput.EchoPassword
	inputs[3].CharLimit = 100
	inputs[3].SetWidth(50)


	return configureModel{
		app:     app,
		styles:  styles,
		inputs:  inputs,
		focused: 0,
	}
}

func (m *configureModel) loadExisting() {
	m.err = nil
	m.success = false
	if m.app != nil {
		creds, err := m.app.GetR2Credentials()
		if err == nil && creds != nil {
			m.inputs[0].SetValue(creds.Endpoint)
			m.inputs[1].SetValue(creds.Bucket)
			m.inputs[2].SetValue(creds.AccessKeyID)
			m.inputs[3].SetValue(creds.SecretAccessKey)
		}
	}
}

func (m configureModel) update(msg tea.Msg) (configureModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "tab", "down":
			m.focused = (m.focused + 1) % 5
			return m, m.updateFocus()
		case "shift+tab", "up":
			m.focused = (m.focused - 1 + 5) % 5
			return m, m.updateFocus()
		case "enter":
			if m.focused < 3 {
				m.focused++
				return m, m.updateFocus()
			}
			// En el último campo o botón guardar: guardar
			m.save()
			return m, nil
		}
	}

	// Update the focused textinput
	if m.focused >= 0 && m.focused < 4 {
		var cmd tea.Cmd
		m.inputs[m.focused], cmd = m.inputs[m.focused].Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m *configureModel) updateFocus() tea.Cmd {
	cmds := make([]tea.Cmd, 4)
	for i := 0; i < 4; i++ {
		if i == m.focused {
			cmds[i] = m.inputs[i].Focus()
		} else {
			m.inputs[i].Blur()
		}
	}
	return tea.Batch(cmds...)
}

func (m *configureModel) save() {
	if m.app == nil {
		m.err = fmt.Errorf("no hay instancia de aplicación conectada")
		return
	}

	endpoint := strings.TrimSpace(m.inputs[0].Value())
	bucket := strings.TrimSpace(m.inputs[1].Value())
	accessKey := strings.TrimSpace(m.inputs[2].Value())
	secretKey := strings.TrimSpace(m.inputs[3].Value())

	err := m.app.SaveR2Credentials(endpoint, bucket, accessKey, secretKey)
	if err != nil {
		m.err = err
		m.success = false
	} else {
		m.err = nil
		m.success = true
	}
}

func (m configureModel) view() string {
	s := m.styles
	var b strings.Builder

	title := s.AppTitle.Render("CONFIGURACIÓN DE CREDENCIALES (R2)")
	sub := s.Subtitle.Render("Los secretos se cifran con Windows DPAPI en config.dat")
	b.WriteString(fmt.Sprintf("%s  %s\n\n", title, sub))

	labels := []string{
		"Endpoint R2:",
		"Bucket:",
		"Access Key ID:",
		"Secret Key:",
	}

	for i := 0; i < 4; i++ {
		labelStr := labels[i]
		if i == m.focused {
			labelStr = s.InputPrompt.Render("▶ " + labelStr)
		} else {
			labelStr = s.Label.Render("  " + labelStr)
		}

		b.WriteString(fmt.Sprintf("%s\n  %s\n\n", labelStr, m.inputs[i].View()))
	}

	// Botón guardar
	saveBtn := "[ Guardar Credenciales ]"
	if m.focused == 4 {
		saveBtn = s.AppTitle.Render("▶ " + saveBtn)
	} else {
		saveBtn = s.Desc.Render("  " + saveBtn)
	}
	b.WriteString(saveBtn)
	b.WriteString("\n\n")

	if m.err != nil {
		b.WriteString(s.Error.Render(fmt.Sprintf("✖ Error: %v", m.err)))
		b.WriteString("\n\n")
	} else if m.success {
		b.WriteString(s.Success.Render("✔ Credenciales cifradas con Windows DPAPI y guardadas en config.dat con éxito."))
		b.WriteString("\n\n")
	}

	keys := []string{
		fmt.Sprintf("%s %s", s.Key.Render("[Esc]"), s.Desc.Render("Volver al Dashboard")),
		fmt.Sprintf("%s %s", s.Key.Render("[Tab/Shift+Tab]"), s.Desc.Render("Cambiar campo")),
		fmt.Sprintf("%s %s", s.Key.Render("[Enter]"), s.Desc.Render("Guardar")),
	}
	b.WriteString(s.HelpBar.Render(strings.Join(keys, "  ")))

	return s.Box.Render(b.String())
}

