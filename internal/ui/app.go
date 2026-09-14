package ui

import (
	"context"

	tea "charm.land/bubbletea/v2"
	"femucaribe-backup-agent/internal/application"
)

type screen int

const (
	screenDashboard screen = iota
	screenBackupProgress
	screenSyncProgress
	screenStatus
	screenLogs
	screenConfigure
	screenHelp
)

type AppModel struct {
	app       *application.App
	exeDir    string
	styles    Styles
	screen    screen
	dashboard dashboardModel
	backup    backupProgressModel
	sync      syncProgressModel
	status    statusModel
	logs      logsModel
	config    configureModel
	help      helpModel
	width     int
	height    int
}

// NewApp crea el modelo raíz de Bubble Tea para la interfaz TUI.
func NewApp(app *application.App, exeDir string) AppModel {
	styles := DefaultStyles()

	return AppModel{
		app:       app,
		exeDir:    exeDir,
		styles:    styles,
		screen:    screenDashboard,
		dashboard: newDashboardModel(styles),
		backup:    newBackupProgressModel(styles),
		sync:      newSyncProgressModel(styles),
		status:    newStatusModel(app, styles),
		logs:      newLogsModel(app, styles),
		config:    newConfigureModel(app, styles),
		help:      newHelpModel(styles),
	}
}

func (m AppModel) Init() tea.Cmd {
	m.refreshDashboard()
	return nil
}

func (m *AppModel) refreshDashboard() {
	if m.app != nil {
		bStatus, backends, err := m.app.GetTUIStatus(context.Background())
		if err == nil {
			m.dashboard.setStatus(bStatus, backends)
		}
	}
}

func (m AppModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.logs.setSize(msg.Width, msg.Height)
		m.help.setSize(msg.Width, msg.Height)
		return m, nil

	case backupFinishedMsg:
		var cmd tea.Cmd
		m.backup, cmd = m.backup.update(msg)
		m.refreshDashboard()
		return m, cmd

	case syncFinishedMsg:
		var cmd tea.Cmd
		m.sync, cmd = m.sync.update(msg)
		m.refreshDashboard()
		return m, cmd

	case tea.KeyPressMsg:
		// Salir global con Ctrl+C
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}

		// En el Dashboard: atajos principales
		if m.screen == screenDashboard {
			switch msg.String() {
			case "b", "B":
				// Guarda anti-apilamiento: si ya hay un backup en curso no disparamos
				// otra goroutine (evita pipelines concurrentes pisándose el lock).
				if m.backup.running {
					return m, nil
				}
				m.screen = screenBackupProgress
				startCmd := m.backup.start()
				return m, tea.Batch(startCmd, runBackupCmd(m.app))
			case "y", "Y":
				// Idem para la sincronización remota.
				if m.sync.running {
					return m, nil
				}
				m.screen = screenSyncProgress
				startCmd := m.sync.start()
				return m, tea.Batch(startCmd, runSyncCmd(m.app))
			case "s", "S":
				m.screen = screenStatus
				m.status.loadStatus()
				return m, nil
			case "l", "L":
				m.screen = screenLogs
				m.logs.loadLogs()
				return m, nil
			case "c", "C":
				m.screen = screenConfigure
				m.config.loadExisting()
				return m, nil
			case "h", "H", "?":
				m.screen = screenHelp
				return m, nil
			case "q", "Q":
				return m, tea.Quit
			}
			return m, nil
		}

		// En pantallas secundarias: volver con Esc (o Q si no estamos editando texto)
		if msg.String() == "esc" || (msg.String() == "q" && m.screen != screenConfigure) {
			m.screen = screenDashboard
			m.refreshDashboard()
			return m, nil
		}

		// En pantallas de progreso finalizadas: Enter también vuelve al Dashboard
		if (m.screen == screenBackupProgress && m.backup.done) || (m.screen == screenSyncProgress && m.sync.done) {
			if msg.String() == "enter" {
				m.screen = screenDashboard
				m.refreshDashboard()
				return m, nil
			}
		}

		// En pantalla de Ayuda: teclas 1-4 cambian de tema
		if m.screen == screenHelp {
			switch msg.String() {
			case "1":
				m.help.loadTopic(topicAbout)
				return m, nil
			case "2":
				m.help.loadTopic(topicBackup)
				return m, nil
			case "3":
				m.help.loadTopic(topicConfig)
				return m, nil
			case "4":
				m.help.loadTopic(topicTroubleshooting)
				return m, nil
			}
		}
	}

	// Delegar actualización a la pantalla activa
	var cmd tea.Cmd
	switch m.screen {
	case screenBackupProgress:
		m.backup, cmd = m.backup.update(msg)
	case screenSyncProgress:
		m.sync, cmd = m.sync.update(msg)
	case screenLogs:
		m.logs, cmd = m.logs.update(msg)
	case screenHelp:
		m.help, cmd = m.help.update(msg)
	case screenConfigure:
		m.config, cmd = m.config.update(msg)
	}

	return m, cmd
}

func (m AppModel) View() tea.View {
	var content string
	switch m.screen {
	case screenDashboard:
		content = m.dashboard.view()
	case screenBackupProgress:
		content = m.backup.view()
	case screenSyncProgress:
		content = m.sync.view()
	case screenStatus:
		content = m.status.view()
	case screenLogs:
		content = m.logs.view()
	case screenConfigure:
		content = m.config.view()
	case screenHelp:
		content = m.help.view()
	default:
		content = m.dashboard.view()
	}

	v := tea.NewView(content)
	v.AltScreen = true
	return v
}
