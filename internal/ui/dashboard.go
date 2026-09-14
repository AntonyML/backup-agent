package ui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"femucaribe-backup-agent/internal/application"
	"femucaribe-backup-agent/internal/version"
)

type dashboardModel struct {
	styles       Styles
	backupStatus application.BackupStatus
	backends     []application.BackendStatus
	profile      application.ProfileDetail
	width        int
	height       int
}

func newDashboardModel(styles Styles) dashboardModel {
	return dashboardModel{
		styles: styles,
	}
}

func (m *dashboardModel) setSize(w, h int) {
	m.width = w
	m.height = h
}

func (m *dashboardModel) setStatus(bStatus application.BackupStatus, backends []application.BackendStatus) {
	m.backupStatus = bStatus
	m.backends = backends
}

func (m *dashboardModel) setProfile(detail application.ProfileDetail) {
	m.profile = detail
}

func (m dashboardModel) renderHeader() string {
	s := m.styles
	title := s.AppTitle.Render("FEMUCARIBE BACKUP AGENT")
	ver := s.Subtitle.Render("v" + version.Current + " (Charm TUI)")
	if m.profile.Name != "" {
		badge := s.BadgeInfo.Render("PERFIL: " + strings.ToUpper(m.profile.Name))
		return fmt.Sprintf("%s  %s  %s\n\n", title, badge, ver)
	}
	return fmt.Sprintf("%s  %s\n\n", title, ver)
}

func (m dashboardModel) renderBackupPanel(targetWidth int) string {
	s := m.styles
	var b strings.Builder

	b.WriteString(s.CardHeader.Render("ÚLTIMO BACKUP"))
	b.WriteString("\n\n")

	var badge string
	switch m.backupStatus.Result {
	case "success":
		badge = s.BadgeSuccess.Render("● EXITOSO")
	case "pending_sync":
		badge = s.BadgeWarning.Render("● PENDIENTE DE SYNC")
	case "error":
		badge = s.BadgeError.Render("● ERROR")
	default:
		badge = s.BadgeMuted.Render("○ NUNCA EJECUTADO")
	}

	b.WriteString(fmt.Sprintf("%s %s\n", s.Label.Render("Resultado:"), badge))

	lastRunText := "Nunca ejecutado"
	if !m.backupStatus.LastRun.IsZero() {
		lastRunText = m.backupStatus.LastRun.Format("2006-01-02 15:04:05")
	}
	b.WriteString(fmt.Sprintf("%s %s\n", s.Label.Render("Última copia:"), s.Value.Render(lastRunText)))

	if m.backupStatus.Filename != "" {
		fn := m.backupStatus.Filename
		maxFn := targetWidth - 22
		if maxFn > 10 && len(fn) > maxFn {
			fn = "..." + fn[len(fn)-maxFn+3:]
		}
		b.WriteString(fmt.Sprintf("%s %s\n", s.Label.Render("Archivo:"), s.Value.Render(fn)))
	}

	if m.backupStatus.SHA256 != "" {
		shaShort := m.backupStatus.SHA256
		if len(shaShort) > 18 {
			shaShort = shaShort[:18] + "..."
		}
		b.WriteString(fmt.Sprintf("%s %s\n", s.Label.Render("SHA-256:"), s.Muted.Render(shaShort)))
	}

	innerWidth := targetWidth - 4
	if innerWidth < 30 {
		innerWidth = 30
	}
	return s.Panel.Width(innerWidth).Render(b.String())
}

func (m dashboardModel) renderStoragePanel(targetWidth int) string {
	s := m.styles
	var b strings.Builder

	b.WriteString(s.CardHeader.Render("DESTINOS DE ALMACENAMIENTO"))
	b.WriteString("\n\n")

	if len(m.backends) == 0 {
		b.WriteString(s.BadgeMuted.Render("Sin destinos configurados"))
		b.WriteString("\n")
	} else {
		for _, backend := range m.backends {
			var badge string
			if !backend.Configured {
				badge = s.BadgeMuted.Render("○ Not configured")
			} else if backend.PendingSync {
				badge = s.BadgeWarning.Render("● PENDING")
			} else if backend.LastSyncOK {
				badge = s.BadgeSuccess.Render("● OK")
			} else {
				badge = s.BadgeError.Render("● ERROR")
			}

			nameStyled := s.Value.Render(fmt.Sprintf("%-12s", backend.Name))
			b.WriteString(fmt.Sprintf("%s  %s\n", nameStyled, badge))
		}
	}

	innerWidth := targetWidth - 4
	if innerWidth < 30 {
		innerWidth = 30
	}
	return s.Panel.Width(innerWidth).Render(b.String())
}

func (m dashboardModel) renderProfilePanel(targetWidth int) string {
	s := m.styles
	var b strings.Builder

	b.WriteString(s.CardHeader.Render("PERFIL ACTIVO & PROGRAMACIÓN"))
	b.WriteString("\n\n")

	if m.profile.Name != "" {
		platStr := strings.Join(m.profile.Platforms, ", ")
		if platStr == "" {
			platStr = "ninguno"
		}
		b.WriteString(fmt.Sprintf("%s %s   %s %s   %s %s\n",
			s.Label.Render("Perfil:"), s.Value.Render(m.profile.Name),
			s.Label.Render("Tipo:"), s.Value.Render(m.profile.Kind),
			s.Label.Render("Destinos:"), s.Muted.Render(platStr),
		))
		if m.profile.NextRun != "" {
			b.WriteString(fmt.Sprintf("%s %s\n", s.Label.Render("Próxima corrida:"), s.Value.Render(m.profile.NextRun)))
		}
		if m.profile.TaskName != "" {
			b.WriteString(fmt.Sprintf("%s %s\n", s.Label.Render("Tarea Windows:"), s.Value.Render(m.profile.TaskName)))
		}
	} else {
		b.WriteString(s.Muted.Render("Perfil no configurado"))
		b.WriteString("\n")
	}

	innerWidth := targetWidth - 4
	if innerWidth < 30 {
		innerWidth = 30
	}
	return s.Panel.Width(innerWidth).Render(b.String())
}

func (m dashboardModel) renderFooter(targetWidth int) string {
	s := m.styles
	keys := []string{
		fmt.Sprintf("%s %s", s.Key.Render("[B]"), s.Desc.Render("Backup")),
		fmt.Sprintf("%s %s", s.Key.Render("[S]"), s.Desc.Render("Estado")),
		fmt.Sprintf("%s %s", s.Key.Render("[L]"), s.Desc.Render("Logs")),
		fmt.Sprintf("%s %s", s.Key.Render("[C]"), s.Desc.Render("Config")),
		fmt.Sprintf("%s %s", s.Key.Render("[Y]"), s.Desc.Render("Sync")),
		fmt.Sprintf("%s %s", s.Key.Render("[H]"), s.Desc.Render("Ayuda")),
		fmt.Sprintf("%s %s", s.Key.Render("[Q]"), s.Desc.Render("Salir")),
	}
	innerWidth := targetWidth - 4
	if innerWidth < 30 {
		innerWidth = 30
	}
	return s.HelpBar.Width(innerWidth).Render(strings.Join(keys, "  "))
}

func (m dashboardModel) view() string {
	w := m.width
	if w <= 0 {
		w = 88
	}

	header := m.renderHeader()

	if w >= 90 {
		avail := w - 4
		halfLeft := avail / 2
		halfRight := avail - halfLeft

		leftPanel := m.renderBackupPanel(halfLeft)
		rightPanel := m.renderStoragePanel(halfRight)
		topRow := lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, rightPanel)

		profilePanel := m.renderProfilePanel(avail)
		footer := m.renderFooter(avail)

		return lipgloss.JoinVertical(lipgloss.Left, header, topRow, profilePanel, footer)
	}

	panelWidth := w - 4
	if panelWidth < 40 {
		panelWidth = 40
	}
	backupPanel := m.renderBackupPanel(panelWidth)
	storagePanel := m.renderStoragePanel(panelWidth)
	profilePanel := m.renderProfilePanel(panelWidth)
	footer := m.renderFooter(panelWidth)

	return lipgloss.JoinVertical(lipgloss.Left, header, backupPanel, storagePanel, profilePanel, footer)
}

