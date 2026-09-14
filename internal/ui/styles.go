package ui

import (
	"charm.land/lipgloss/v2"
)

// Semantic colors
var (
	ColorGreen         = lipgloss.Color("#4ade80") // éxito (emerald)
	ColorYellow        = lipgloss.Color("#fbbf24") // advertencia / pendiente (amber)
	ColorRed           = lipgloss.Color("#f87171") // error (coral)
	ColorBlue          = lipgloss.Color("#38bdf8") // información / primario (sky)
	ColorIndigo        = lipgloss.Color("#818cf8") // secundario de acento (indigo)
	ColorGray          = lipgloss.Color("#94a3b8") // secundario / muted (slate-400)
	ColorDarkGray      = lipgloss.Color("#334155") // bordes secundarios (slate-700)
	ColorWhite         = lipgloss.Color("#f8fafc") // texto principal (slate-50)
	ColorBgCard        = lipgloss.Color("#1e293b") // fondo panel (slate-800)
	ColorBadgeGreenBg  = lipgloss.Color("#14532d")
	ColorBadgeYellowBg = lipgloss.Color("#713f12")
	ColorBadgeRedBg    = lipgloss.Color("#7f1d1d")
	ColorBadgeBlueBg   = lipgloss.Color("#0c4a6e")
	ColorBadgeMutedBg  = lipgloss.Color("#1e293b")
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

	// Estilos modernos para layout modular tipo Lazygit / k9s / btop
	Panel        lipgloss.Style
	PanelActive  lipgloss.Style
	CardHeader   lipgloss.Style
	CardTitle    lipgloss.Style
	BadgeSuccess lipgloss.Style
	BadgeWarning lipgloss.Style
	BadgeError   lipgloss.Style
	BadgeMuted   lipgloss.Style
	BadgeInfo    lipgloss.Style
	StatusBar    lipgloss.Style
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

		Panel: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorDarkGray).
			Padding(0, 1),

		PanelActive: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorBlue).
			Padding(0, 1),

		CardHeader: lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorBlue),

		CardTitle: lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorWhite),

		BadgeSuccess: lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorGreen).
			Background(ColorBadgeGreenBg).
			Padding(0, 1),

		BadgeWarning: lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorYellow).
			Background(ColorBadgeYellowBg).
			Padding(0, 1),

		BadgeError: lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorRed).
			Background(ColorBadgeRedBg).
			Padding(0, 1),

		BadgeMuted: lipgloss.NewStyle().
			Foreground(ColorGray).
			Background(ColorBadgeMutedBg).
			Padding(0, 1),

		BadgeInfo: lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorBlue).
			Background(ColorBadgeBlueBg).
			Padding(0, 1),

		StatusBar: lipgloss.NewStyle().
			Foreground(ColorGray).
			Background(ColorBgCard).
			Padding(0, 1),
	}
}
