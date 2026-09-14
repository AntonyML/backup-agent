package ui

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"femucaribe-backup-agent/internal/application"
)

type statusModel struct {
	app    AppConnector
	styles Styles
	report *application.StatusReport
	checks []application.PlatformCheck
	err    error
}

func newStatusModel(app AppConnector, styles Styles) statusModel {
	return statusModel{
		app:    app,
		styles: styles,
	}
}

func (m *statusModel) loadStatus() {
	if m.app == nil {
		m.err = fmt.Errorf("no hay instancia de aplicación conectada")
		return
	}

	report, err := m.app.Status(context.Background())
	if err != nil {
		m.err = err
		return
	}
	m.report = report
	m.checks = m.app.CheckPlatforms(context.Background())
	m.err = nil
}

func (m *statusModel) update(msg tea.Msg) (*statusModel, tea.Cmd) {
	if key, ok := msg.(tea.KeyPressMsg); ok {
		if key.String() == "r" || key.String() == "R" {
			m.loadStatus()
			return m, nil
		}
	}
	return m, nil
}

func (m statusModel) view() string {
	s := m.styles
	var b strings.Builder

	title := s.AppTitle.Render("ESTADO DETALLADO DEL AGENTE")
	b.WriteString(fmt.Sprintf("%s\n\n", title))

	if m.err != nil {
		b.WriteString(s.Error.Render(fmt.Sprintf("Error obteniendo estado: %v\n\n", m.err)))
	} else if m.report != nil {
		r := m.report

		// Perfil y Base de datos
		b.WriteString(s.SectionHeader.Render("CONFIGURACIÓN DE BASE DE DATOS Y PERFIL"))
		b.WriteString("\n")
		b.WriteString(fmt.Sprintf("  %s %s\n", s.Label.Render("Perfil activo:"), s.Value.Render(r.Profile)))
		b.WriteString(fmt.Sprintf("  %s %s\n", s.Label.Render("Base de datos:"), s.Value.Render(r.Database)))
		b.WriteString(fmt.Sprintf("  %s %s\n", s.Label.Render("Servidor SQL: "), s.Value.Render(r.Server)))
		b.WriteString(fmt.Sprintf("  %s %s\n", s.Label.Render("Directorio:   "), s.Value.Render(r.BackupDir)))
		b.WriteString(fmt.Sprintf("  %s %s\n\n", s.Label.Render("Retención:    "), s.Value.Render(fmt.Sprintf("%d copias", r.Retain))))

		// Proceso y Lock
		b.WriteString(s.SectionHeader.Render("PROCESO Y CONCURRENCIA"))
		b.WriteString("\n")
		lockText := s.Success.Render("Libre")
		if r.LockActive {
			lockText = s.Warning.Render("Activo (en ejecución)")
		}
		b.WriteString(fmt.Sprintf("  %s %s\n\n", s.Label.Render("Lock file:"), lockText))

		// Copia local
		b.WriteString(s.SectionHeader.Render("ÚLTIMA COPIA LOCAL"))
		b.WriteString("\n")
		lastDate := r.LastRunDate
		if lastDate == "" {
			lastDate = "Nunca"
		}
		b.WriteString(fmt.Sprintf("  %s %s\n", s.Label.Render("Fecha de corrida:"), s.Value.Render(lastDate)))
		if r.LastBackupFile != "" {
			b.WriteString(fmt.Sprintf("  %s %s\n", s.Label.Render("Archivo:         "), s.Value.Render(r.LastBackupFile)))
		}
		if r.SHA256 != "" {
			b.WriteString(fmt.Sprintf("  %s %s\n\n", s.Label.Render("SHA-256:         "), s.Value.Render(r.SHA256)))
		} else {
			b.WriteString("\n")
		}

		// Remoto R2 y UNC
		b.WriteString(s.SectionHeader.Render("ESTADO DE DESTINOS REMOTOS"))
		b.WriteString("\n")
		pendingR2 := s.Success.Render("Al día (sin pendientes)")
		if r.PendingSyncR2 {
			pendingR2 = s.Warning.Render("PENDIENTE DE SUBIDA")
		}
		b.WriteString(fmt.Sprintf("  %s %s\n", s.Label.Render("Sincronización R2: "), pendingR2))
		if r.R2LastSyncedFile != "" {
			b.WriteString(fmt.Sprintf("  %s %s\n", s.Label.Render("Último en R2:      "), s.Value.Render(r.R2LastSyncedFile)))
		}

		pendingUNC := s.Success.Render("Al día (sin pendientes)")
		if r.PendingSyncServer {
			pendingUNC = s.Warning.Render("PENDIENTE DE COPIA")
		}
		b.WriteString(fmt.Sprintf("  %s %s\n", s.Label.Render("Sincronización UNC:"), pendingUNC))
		if r.ServerLastSyncedFile != "" {
			b.WriteString(fmt.Sprintf("  %s %s\n", s.Label.Render("Último en UNC:     "), s.Value.Render(r.ServerLastSyncedFile)))
		}
		b.WriteString("\n")

		// Diagnóstico de Salud (Doctor / CheckPlatforms)
		b.WriteString(s.SectionHeader.Render("DIAGNÓSTICO DE PLATAFORMAS (DOCTOR)"))
		b.WriteString("\n")
		if len(m.checks) == 0 {
			b.WriteString(s.Muted.Render("  Sin información de diagnóstico"))
			b.WriteString("\n")
		} else {
			for _, chk := range m.checks {
				icon := s.Success.Render("✔")
				if !chk.OK {
					icon = s.Error.Render("✖")
				}
				nameStyled := s.Value.Render(fmt.Sprintf("%-16s", chk.Name))
				detailStyled := s.Desc.Render(chk.Detail)
				if !chk.OK {
					detailStyled = s.Error.Render(chk.Detail)
				}
				b.WriteString(fmt.Sprintf("  %s %s %s\n", icon, nameStyled, detailStyled))
			}
		}
		b.WriteString("\n")
	} else {
		b.WriteString(s.Muted.Render("Cargando información..."))
		b.WriteString("\n\n")
	}

	keys := []string{
		fmt.Sprintf("%s %s", s.Key.Render("[Esc/Q]"), s.Desc.Render("Volver al Dashboard")),
		fmt.Sprintf("%s %s", s.Key.Render("[R]"), s.Desc.Render("Recargar")),
	}
	b.WriteString(s.HelpBar.Render(strings.Join(keys, "  ")))

	return s.Box.Render(b.String())
}
