package ui

import (
	"context"

	tea "charm.land/bubbletea/v2"
	"femucaribe-backup-agent/internal/auth"
)

type screen int

const (
	screenDashboard screen = iota
	screenBackupProgress
	screenSyncProgress
	screenStatus
	screenLogs
	screenSettings
	screenConfigure
	screenHelp
	screenLogin
	screenExport
	screenImport
)

type AppModel struct {
	app         AppConnector
	exeDir      string
	authMgr     *auth.Manager
	styles      Styles
	screen      screen
	dashboard   dashboardModel
	backup      backupProgressModel
	sync        syncProgressModel
	status      statusModel
	logs        logsModel
	settings    settingsModel
	config      configureModel
	help        helpModel
	login       loginModel
	export      exportModalModel
	importModal importModalModel
	width       int
	height      int
}

// NewApp crea el modelo raíz de Bubble Tea para la interfaz TUI.
func NewApp(app AppConnector, exeDir string, authMgr ...*auth.Manager) AppModel {
	styles := DefaultStyles()

	var mgr *auth.Manager
	if len(authMgr) > 0 {
		mgr = authMgr[0]
	}

	initialScreen := screenDashboard
	if mgr != nil && !mgr.IsAuthenticated(context.Background()) {
		initialScreen = screenLogin
	}

	return AppModel{
		app:       app,
		exeDir:    exeDir,
		authMgr:   mgr,
		styles:    styles,
		screen:    initialScreen,
		dashboard: newDashboardModel(styles),
		backup:    newBackupProgressModel(styles),
		sync:      newSyncProgressModel(styles),
		status:    newStatusModel(app, styles),
		logs:      newLogsModel(app, styles),
		settings:    newSettingsModel(app, styles),
		config:      newConfigureModel(app, styles),
		help:        newHelpModel(styles),
		login:       newLoginModel(mgr, styles),
		export:      newExportModalModel(app, styles),
		importModal: newImportModalModel(app, styles),
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
		m.dashboard.setProfile(m.app.ActiveProfileDetail())
	}
	if m.authMgr != nil {
		m.dashboard.setCurrentUser(m.authMgr.CurrentUser(context.Background()))
	}
}

func (m AppModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.dashboard.setSize(msg.Width, msg.Height)
		m.settings.setSize(msg.Width, msg.Height)
		m.status.setSize(msg.Width, msg.Height)
		m.logs.setSize(msg.Width, msg.Height)
		m.help.setSize(msg.Width, msg.Height)
		m.login.setSize(msg.Width, msg.Height)
		m.export.setSize(msg.Width, msg.Height)
		m.importModal.setSize(msg.Width, msg.Height)
		return m, nil

	case loginSuccessMsg:
		m.screen = screenDashboard
		m.refreshDashboard()
		return m, nil

	case logoutMsg:
		if m.authMgr != nil {
			_ = m.authMgr.Logout()
		}
		m.screen = screenLogin
		return m, nil

	case openCredentialsMsg:
		m.screen = screenConfigure
		m.config.loadExisting()
		return m, nil

	case backToDashboardMsg:
		m.screen = screenDashboard
		m.settings.load()
		m.refreshDashboard()
		return m, nil

	case exportSuccessMsg, exportFailedMsg:
		m.export, _ = m.export.update(msg)
		return m, nil

	case importSuccessMsg, importFailedMsg:
		m.importModal, _ = m.importModal.update(msg)
		if _, ok := msg.(importSuccessMsg); ok {
			m.settings.load()
			m.refreshDashboard()
		}
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

		// En pantalla de Login: delegar exclusivamente al modelo de autenticación
		if m.screen == screenLogin {
			var cmd tea.Cmd
			m.login, cmd = m.login.update(msg)
			return m, cmd
		}

		// En pantalla de Exportar: delegar al modal
		if m.screen == screenExport {
			var cmd tea.Cmd
			m.export, cmd = m.export.update(msg)
			return m, cmd
		}

		// En pantalla de Importar: delegar al modal
		if m.screen == screenImport {
			var cmd tea.Cmd
			m.importModal, cmd = m.importModal.update(msg)
			return m, cmd
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
				m.screen = screenSettings
				m.settings.load()
				return m, nil
			case "e", "E":
				m.screen = screenExport
				m.export.reset()
				return m, nil
			case "i", "I":
				m.screen = screenImport
				m.importModal.reset()
				return m, nil
			case "h", "H", "?":
				m.screen = screenHelp
				return m, nil
			case "x", "X":
				if m.authMgr != nil {
					_ = m.authMgr.Logout()
				}
				m.screen = screenLogin
				return m, nil
			case "q", "Q":
				return m, tea.Quit
			}
			return m, nil
		}

		// En pantallas secundarias: volver con Esc (o Q si no estamos editando texto/settings)
		if m.screen != screenSettings && m.screen != screenConfigure && m.screen != screenExport && m.screen != screenImport {
			if msg.String() == "esc" || msg.String() == "q" {
				m.screen = screenDashboard
				m.refreshDashboard()
				return m, nil
			}
		}

		// En pantalla de configuración de credenciales (screenConfigure) volver con Esc
		if m.screen == screenConfigure {
			if msg.String() == "esc" {
				m.screen = screenSettings
				m.settings.load()
				return m, nil
			}
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
	case screenLogin:
		m.login, cmd = m.login.update(msg)
	case screenExport:
		m.export, cmd = m.export.update(msg)
	case screenImport:
		m.importModal, cmd = m.importModal.update(msg)
	case screenBackupProgress:
		m.backup, cmd = m.backup.update(msg)
	case screenSyncProgress:
		m.sync, cmd = m.sync.update(msg)
	case screenStatus:
		var st *statusModel
		st, cmd = m.status.update(msg)
		if st != nil {
			m.status = *st
		}
	case screenLogs:
		m.logs, cmd = m.logs.update(msg)
	case screenHelp:
		m.help, cmd = m.help.update(msg)
	case screenSettings:
		var sm *settingsModel
		sm, cmd = m.settings.update(msg)
		if sm != nil {
			m.settings = *sm
		}
	case screenConfigure:
		m.config, cmd = m.config.update(msg)
	}

	return m, cmd
}

func (m AppModel) View() tea.View {
	var content string
	switch m.screen {
	case screenLogin:
		content = m.login.view()
	case screenExport:
		content = m.export.view()
	case screenImport:
		content = m.importModal.view()
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
	case screenSettings:
		content = m.settings.view()
	case screenConfigure:
		content = m.config.view()
	case screenHelp:
		content = m.help.view()
	default:
		content = m.dashboard.view()
	}

	v := tea.NewView(content)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

