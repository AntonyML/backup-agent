package ui

import (
	"fmt"
	"strings"

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

func (m *dashboardModel) setStatus(bStatus application.BackupStatus, backends []application.BackendStatus) {
	m.backupStatus = bStatus
	m.backends = backends
}

func (m *dashboardModel) setProfile(detail application.ProfileDetail) {
	m.profile = detail
}

func (m dashboardModel) view() string {
	s := m.styles
	var b strings.Builder

	// Header
	title := s.AppTitle.Render("FEMUCARIBE BACKUP AGENT")
	ver := s.Subtitle.Render("v" + version.Current + " (Charm TUI)")
	headerLine := fmt.Sprintf("%s  %s", title, ver)
	b.WriteString(headerLine)
	b.WriteString("\n\n")

	// Section 0: Perfil Activo
	if m.profile.Name != "" {
		b.WriteString(s.SectionHeader.Render("PERFIL ACTIVO"))
		b.WriteString("\n")
		platStr := strings.Join(m.profile.Platforms, ", ")
		b.WriteString(fmt.Sprintf("  %s %s (%s) · %s: %s\n",
			s.Label.Render("Perfil:"),
			s.Value.Render(m.profile.Name),
			m.profile.Kind,
			s.Label.Render("Destinos"),
			platStr))
		if m.profile.NextRun != "" {
			b.WriteString(fmt.Sprintf("  %s %s\n", s.Label.Render("Próxima corrida:"), s.Value.Render(m.profile.NextRun)))
		}
		if m.profile.TaskName != "" {
			b.WriteString(fmt.Sprintf("  %s %s\n", s.Label.Render("Tarea Windows:  "), s.Value.Render(m.profile.TaskName)))
		}
		b.WriteString("\n")
	}

	// Section 1: Backup Status
	b.WriteString(s.SectionHeader.Render("ESTADO DEL BACKUP"))
	b.WriteString("\n")

	lastRunText := "Nunca ejecutado"
	if !m.backupStatus.LastRun.IsZero() {
		lastRunText = m.backupStatus.LastRun.Format("2006-01-02 15:04:05")
	}

	resultText := s.Muted.Render("○ NUNCA EJECUTADO")
	switch m.backupStatus.Result {
	case "success":
		resultText = s.Success.Render("● EXITOSO")
	case "pending_sync":
		resultText = s.Warning.Render("● PENDIENTE DE SYNC")
	case "error":
		resultText = s.Error.Render("● ERROR")
	}

	b.WriteString(fmt.Sprintf("  %s %s\n", s.Label.Render("Última copia:"), s.Value.Render(lastRunText)))
	if m.backupStatus.Filename != "" {
		b.WriteString(fmt.Sprintf("  %s %s\n", s.Label.Render("Archivo:"), s.Value.Render(m.backupStatus.Filename)))
	}
	if m.backupStatus.SHA256 != "" {
		shaShort := m.backupStatus.SHA256
		if len(shaShort) > 20 {
			shaShort = shaShort[:20] + "..."
		}
		b.WriteString(fmt.Sprintf("  %s %s\n", s.Label.Render("SHA-256:"), s.Value.Render(shaShort)))
	}
	b.WriteString(fmt.Sprintf("  %s %s\n\n", s.Label.Render("Resultado:"), resultText))

	// Section 2: Backends
	b.WriteString(s.SectionHeader.Render("DESTINOS DE ALMACENAMIENTO"))
	b.WriteString("\n")
	if len(m.backends) == 0 {
		b.WriteString(s.Muted.Render("  No hay backends disponibles"))
		b.WriteString("\n")
	} else {
		for _, backend := range m.backends {
			var icon, statusStr string
			if !backend.Configured {
				icon = s.Muted.Render("○")
				statusStr = s.Muted.Render("Not configured")
			} else if backend.PendingSync {
				icon = s.Warning.Render("●")
				statusStr = s.Warning.Render("PENDING")
			} else if backend.LastSyncOK {
				icon = s.Success.Render("●")
				statusStr = s.Success.Render("OK")
			} else {
				icon = s.Error.Render("●")
				statusStr = s.Error.Render("ERROR")
			}

			nameStyled := s.Value.Render(fmt.Sprintf("%-12s", backend.Name))
			b.WriteString(fmt.Sprintf("  %s %s %s\n", icon, nameStyled, statusStr))
		}
	}
	b.WriteString("\n")

	// Section 3: Navigation / Keybindings
	keys := []string{
		fmt.Sprintf("%s %s", s.Key.Render("[B]"), s.Desc.Render("Backup")),
		fmt.Sprintf("%s %s", s.Key.Render("[S]"), s.Desc.Render("Estado")),
		fmt.Sprintf("%s %s", s.Key.Render("[L]"), s.Desc.Render("Logs")),
		fmt.Sprintf("%s %s", s.Key.Render("[C]"), s.Desc.Render("Config")),
		fmt.Sprintf("%s %s", s.Key.Render("[Y]"), s.Desc.Render("Sync")),
		fmt.Sprintf("%s %s", s.Key.Render("[H]"), s.Desc.Render("Ayuda")),
		fmt.Sprintf("%s %s", s.Key.Render("[Q]"), s.Desc.Render("Salir")),
	}
	helpBar := s.HelpBar.Render(strings.Join(keys, "  "))
	b.WriteString(helpBar)

	return s.Box.Render(b.String())
}
