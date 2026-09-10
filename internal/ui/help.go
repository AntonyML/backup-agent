package ui

import (
	"embed"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2"
)

//go:embed content/*.md
var contentFS embed.FS

type helpTopic int

const (
	topicAbout helpTopic = iota
	topicBackup
	topicConfig
	topicTroubleshooting
)

type helpModel struct {
	styles       Styles
	viewport     viewport.Model
	currentTopic helpTopic
	width        int
	height       int
}

func newHelpModel(styles Styles) helpModel {
	vp := viewport.New()
	vp.SetWidth(80)
	vp.SetHeight(20)

	m := helpModel{
		styles:       styles,
		viewport:     vp,
		currentTopic: topicAbout,
	}
	m.loadTopic(topicAbout)
	return m
}

func (m *helpModel) setSize(w, h int) {
	m.width = w
	m.height = h

	vpWidth := w - 6
	if vpWidth < 40 {
		vpWidth = 40
	}
	vpHeight := h - 10
	if vpHeight < 10 {
		vpHeight = 10
	}

	m.viewport.SetWidth(vpWidth)
	m.viewport.SetHeight(vpHeight)
	m.loadTopic(m.currentTopic)
}

func (m *helpModel) loadTopic(topic helpTopic) {
	m.currentTopic = topic
	var filename string
	switch topic {
	case topicAbout:
		filename = "content/about.md"
	case topicBackup:
		filename = "content/backup-help.md"
	case topicConfig:
		filename = "content/configuration-help.md"
	case topicTroubleshooting:
		filename = "content/troubleshooting.md"
	default:
		filename = "content/about.md"
	}

	raw, err := contentFS.ReadFile(filename)
	if err != nil {
		m.viewport.SetContent(fmt.Sprintf("Error leyendo archivo de ayuda %s: %v", filename, err))
		return
	}

	rendered, err := glamour.Render(string(raw), "dark")
	if err != nil {
		// Fallback a texto plano si falla el renderer
		m.viewport.SetContent(string(raw))
		return
	}

	m.viewport.SetContent(rendered)
	m.viewport.GotoTop()
}

func (m helpModel) update(msg tea.Msg) (helpModel, tea.Cmd) {
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m helpModel) view() string {
	s := m.styles
	var b strings.Builder

	title := s.AppTitle.Render("MANUAL Y AYUDA CONTEXTUAL")
	b.WriteString(fmt.Sprintf("%s\n\n", title))

	// Topic tabs
	tabAbout := "[1] Acerca de"
	tabBackup := "[2] Backup"
	tabConfig := "[3] Configuración"
	tabTrouble := "[4] Resolución de Problemas"

	switch m.currentTopic {
	case topicAbout:
		tabAbout = s.Key.Render("▶ ") + s.AppTitle.Render(tabAbout)
	case topicBackup:
		tabBackup = s.Key.Render("▶ ") + s.AppTitle.Render(tabBackup)
	case topicConfig:
		tabConfig = s.Key.Render("▶ ") + s.AppTitle.Render(tabConfig)
	case topicTroubleshooting:
		tabTrouble = s.Key.Render("▶ ") + s.AppTitle.Render(tabTrouble)
	}

	tabsLine := fmt.Sprintf("%s   %s   %s   %s\n\n", tabAbout, tabBackup, tabConfig, tabTrouble)
	b.WriteString(tabsLine)

	b.WriteString(m.viewport.View() + "\n\n")

	keys := []string{
		fmt.Sprintf("%s %s", s.Key.Render("[Esc/Q]"), s.Desc.Render("Volver al Dashboard")),
		fmt.Sprintf("%s %s", s.Key.Render("[1-4]"), s.Desc.Render("Cambiar Tema")),
		fmt.Sprintf("%s %s", s.Key.Render("[↑/↓]"), s.Desc.Render("Scroll")),
	}
	b.WriteString(s.HelpBar.Render(strings.Join(keys, "  ")))

	return s.Box.Render(b.String())
}
