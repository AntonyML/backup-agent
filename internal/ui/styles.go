package ui

import (
	"charm.land/lipgloss/v2"
)

// Semantic colors
var (
	ColorGreen    = lipgloss.Color("#2ecc71") // éxito
	ColorYellow   = lipgloss.Color("#f1c40f") // advertencia / pendiente
	ColorRed      = lipgloss.Color("#e74c3c") // error
	ColorBlue     = lipgloss.Color("#3498db") // información / primario
	ColorGray     = lipgloss.Color("#7f8c8d") // secundario / muted
	ColorDarkGray = lipgloss.Color("#34495e") // bordes secundarios
	ColorWhite    = lipgloss.Color("#ecf0f1") // texto principal
)


// Styles agrupa y centraliza los estilos de Lip Gloss para toda la interfaz TUI.
type Styles struct {
	AppTitle      lipgloss.Style
	Subtitle      lipgloss.Style
	Box           lipgloss.Style
	SectionHeader lipgloss.Style
	Label         lipgloss.Style
	Value         lipgloss.Style
	Success       lipgloss.Style
	Warning       lipgloss.Style
	Error         lipgloss.Style
	Info          lipgloss.Style
	Muted         lipgloss.Style
	Key           lipgloss.Style
	Desc          lipgloss.Style
	HelpBar       lipgloss.Style
	Spinner       lipgloss.Style
	InputPrompt   lipgloss.Style
	TableBorder   lipgloss.Style
}

// DefaultStyles inicializa la paleta de estilos estándar.
func DefaultStyles() Styles {
	return Styles{
		AppTitle: lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorWhite).
			Background(ColorBlue).
			Padding(0, 1),

		Subtitle: lipgloss.NewStyle().
			Foreground(ColorGray).
			Italic(true),

		Box: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorBlue).
			Padding(0, 1),

		SectionHeader: lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorBlue).
			MarginTop(1).
			MarginBottom(0),

		Label: lipgloss.NewStyle().
			Foreground(ColorWhite).
			Bold(true).
			Width(16),

		Value: lipgloss.NewStyle().
			Foreground(ColorWhite),

		Success: lipgloss.NewStyle().
			Foreground(ColorGreen).
			Bold(true),

		Warning: lipgloss.NewStyle().
			Foreground(ColorYellow).
			Bold(true),

		Error: lipgloss.NewStyle().
			Foreground(ColorRed).
			Bold(true),

		Info: lipgloss.NewStyle().
			Foreground(ColorBlue),

		Muted: lipgloss.NewStyle().
			Foreground(ColorGray),

		Key: lipgloss.NewStyle().
			Foreground(ColorBlue).
			Bold(true),

		Desc: lipgloss.NewStyle().
			Foreground(ColorWhite),

		HelpBar: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorDarkGray).
			Padding(0, 1).
			MarginTop(1),

		Spinner: lipgloss.NewStyle().
			Foreground(ColorBlue),

		InputPrompt: lipgloss.NewStyle().
			Foreground(ColorBlue).
			Bold(true),

		TableBorder: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorDarkGray),
	}
}
