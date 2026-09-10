package ui

import (
	"context"
	"fmt"
	"strings"

	"femucaribe-backup-agent/internal/application"
)

type statusModel struct {
	app    *application.App
	styles Styles
	report *application.StatusReport
	err    error
}

func newStatusModel(app *application.App, styles Styles) statusModel {
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
	m.err = nil
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

		// Parámetros SQL
		b.WriteString(s.SectionHeader.Render("CONFIGURACIÓN DE BASE DE DATOS") + "\n")
		b.WriteString(fmt.Sprintf("  %s %s\n", s.Label.Render("Base de datos:"), s.Value.Render(r.Database)))
		b.WriteString(fmt.Sprintf("  %s %s\n", s.Label.Render("Servidor SQL:"), s.Value.Render(r.Server)))
		b.WriteString(fmt.Sprintf("  %s %s\n", s.Label.Render("Directorio:"), s.Value.Render(r.BackupDir)))
		b.WriteString(fmt.Sprintf("  %s %s\n\n", s.Label.Render("Retención:"), s.Value.Render(fmt.Sprintf("%d copias", r.Retain))))

		// Proceso y Lock
		b.WriteString(s.SectionHeader.Render("PROCESO Y CONCURRENCIA") + "\n")
		lockText := s.Success.Render("Libre")
		if r.LockActive {
			lockText = s.Warning.Render("Activo (en ejecución)")
		}
		b.WriteString(fmt.Sprintf("  %s %s\n\n", s.Label.Render("Lock file:"), lockText))

		// Copia local
		b.WriteString(s.SectionHeader.Render("ÚLTIMA COPIA LOCAL") + "\n")
		lastDate := r.LastRunDate
		if lastDate == "" {
			lastDate = "Nunca"
		}
		b.WriteString(fmt.Sprintf("  %s %s\n", s.Label.Render("Fecha de corrida:"), s.Value.Render(lastDate)))
		if r.LastBackupFile != "" {
			b.WriteString(fmt.Sprintf("  %s %s\n", s.Label.Render("Archivo:"), s.Value.Render(r.LastBackupFile)))
		}
		if r.SHA256 != "" {
			b.WriteString(fmt.Sprintf("  %s %s\n\n", s.Label.Render("SHA-256:"), s.Value.Render(r.SHA256)))
		} else {
			b.WriteString("\n")
		}

		// Remoto R2
		b.WriteString(s.SectionHeader.Render("ESTADO REMOTO CLOUDFLARE R2") + "\n")
		pendingText := s.Success.Render("Al día (sin pendientes)")
		if r.PendingSyncR2 {
			pendingText = s.Warning.Render("PENDIENTE DE SUBIDA")
		}
		b.WriteString(fmt.Sprintf("  %s %s\n", s.Label.Render("Sincronización:"), pendingText))
		if r.R2LastSyncedFile != "" {
			b.WriteString(fmt.Sprintf("  %s %s\n\n", s.Label.Render("Último en R2:"), s.Value.Render(r.R2LastSyncedFile)))
		} else {
			b.WriteString("\n")
		}
	} else {
		b.WriteString(s.Muted.Render("Cargando información...") + "\n\n")
	}

	keys := []string{
		fmt.Sprintf("%s %s", s.Key.Render("[Esc/Q]"), s.Desc.Render("Volver al Dashboard")),
		fmt.Sprintf("%s %s", s.Key.Render("[R]"), s.Desc.Render("Recargar")),
	}
	b.WriteString(s.HelpBar.Render(strings.Join(keys, "  ")))

	return s.Box.Render(b.String())
}
