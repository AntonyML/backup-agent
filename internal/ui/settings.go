package ui

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"femucaribe-backup-agent/internal/application"
	"femucaribe-backup-agent/internal/scheduler"
)

// settingsLevel indica el nivel de profundidad de navegación en Ajustes.
type settingsLevel int

const (
	settingsLevelGroups settingsLevel = iota // Nivel 0: grupos principales
	settingsLevelFields                      // Nivel 1: campos / listado del grupo
	settingsLevelSub                         // Nivel 2: subpantalla (ej. plataforma específica, tarea Windows)
)

// fieldKind define cómo se edita un campo.
type fieldKind int

const (
	kindText fieldKind = iota
	kindInt
	kindBool
	kindChoice
	kindList
	kindMulti
	kindPassword
)

// settingsField es una fila editable de un grupo de ajustes.
type settingsField struct {
	Label     string
	Help      string
	Kind      fieldKind
	Options   []string
	MultiType string // "weekdays" o "platforms"
	Get       func(application.Settings) string
	Display   func(application.Settings) string
	Set       func(*application.Settings, string) error
}

func boolText(v bool) string {
	if v {
		return "Activado"
	}
	return "Desactivado"
}

func boolOptions() []string { return []string{"true", "false"} }

func parseBool(v string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "sí", "si", "s", "y", "yes", "1", "activado", "on":
		return true, nil
	case "false", "no", "n", "0", "desactivado", "off":
		return false, nil
	}
	return false, fmt.Errorf("valor booleano inválido: %q", v)
}

func parseInt(v string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return 0, fmt.Errorf("valor numérico inválido: %q", v)
	}
	return n, nil
}

func joinWeekdays(days []string) string { return strings.Join(days, ",") }

func splitList(v string) []string {
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.ToLower(strings.TrimSpace(p))
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// openCredentialsMsg indica al AppModel raíz que debe abrir la pantalla de credenciales DPAPI.
type openCredentialsMsg struct{}

// backToDashboardMsg indica volver al dashboard principal.
type backToDashboardMsg struct{}

type promptKind int

const (
	promptNone promptKind = iota
	promptNewProfile
	promptRenameProfile
)

type confirmKind int

const (
	confirmNone confirmKind = iota
	confirmDeleteProfile
	confirmDeleteWindowsTask
	confirmInstallWindowsTask
)

// settingsModel coordina toda la navegación y edición de Ajustes.
type settingsModel struct {
	app      AppConnector
	styles   Styles
	level    settingsLevel
	cfg      application.Settings
	groupIdx int
	fieldIdx int
	subIdx   int

	editing     bool
	input       textinput.Model
	multi       *multiselect
	confirm     *confirmModel
	confirmType confirmKind

	// Modo prompt para ingresar texto libre (crear / renombrar perfil)
	prompting   bool
	promptTitle string
	promptType  promptKind

	dirty  bool
	saved  bool
	err    error
	notice string

	// Estado cacheado de la tarea de Windows
	taskStatusText string

	// Viewport para scroll universal
	viewport viewport.Model
	width    int
	height   int
	ready    bool
}

func newSettingsModel(app AppConnector, styles Styles) settingsModel {
	in := textinput.New()
	in.CharLimit = 1024
	in.SetWidth(48)

	vp := viewport.New()
	vp.SetWidth(80)
	vp.SetHeight(16)

	return settingsModel{
		app:      app,
		styles:   styles,
		input:    in,
		viewport: vp,
	}
}

func (m *settingsModel) setSize(w, h int) {
	m.width = w
	m.height = h
	vpWidth := w - 6
	if vpWidth < 40 {
		vpWidth = 40
	}
	vpHeight := h - 10
	if vpHeight < 8 {
		vpHeight = 8
	}
	m.viewport.SetWidth(vpWidth)
	m.viewport.SetHeight(vpHeight)
	m.ready = true
	m.updateViewportContent()
}

func (m *settingsModel) ensureActiveVisible(content string) {
	lines := strings.Split(content, "\n")
	targetLine := -1
	for i, l := range lines {
		if strings.Contains(l, "▶") {
			targetLine = i
			break
		}
	}
	if targetLine >= 0 {
		m.viewport.EnsureVisible(targetLine, 0, 0)
	}
}

func (m *settingsModel) buildBodyContent() string {
	var body strings.Builder
	switch m.level {
	case settingsLevelGroups:
		m.viewGroups(&body)
	case settingsLevelFields:
		switch m.groupIdx {
		case groupProfiles:
			m.viewProfiles(&body)
		case groupPlatforms:
			m.viewPlatformsMenu(&body)
		default:
			m.viewFields(&body)
		}
	case settingsLevelSub:
		switch m.groupIdx {
		case groupSchedule:
			m.viewWindowsTaskSub(&body)
		default:
			m.viewFields(&body)
		}
	}

	s := m.styles
	if m.notice != "" {
		body.WriteString("\n")
		body.WriteString(s.Success.Render(m.notice))
		body.WriteString("\n")
	}
	if m.err != nil {
		body.WriteString("\n")
		body.WriteString(s.Error.Render(fmt.Sprintf("✖ %v", m.err)))
		body.WriteString("\n")
	}

	if strings.EqualFold(m.cfg.Schedule.Mode, "weekly") && len(m.cfg.Schedule.Weekdays) == 0 {
		body.WriteString("\n")
		body.WriteString(s.Warning.Render("⚠ Modo weekly activo sin días seleccionados."))
	}
	if m.cfg.Cloudflare.Enabled && m.app != nil {
		creds, _ := m.app.GetR2Credentials()
		if creds == nil || creds.Endpoint == "" || creds.Bucket == "" || creds.AccessKeyID == "" || creds.SecretAccessKey == "" {
			body.WriteString("\n")
			body.WriteString(s.Warning.Render("⚠ R2 habilitado pero faltan credenciales (cargalas en Credenciales R2)."))
		}
	}
	if m.cfg.Schedule.SyncAfterBackup {
		p, ok := m.cfg.ProfileByName(m.cfg.ActiveProfile)
		if ok && len(p.Platforms) == 0 {
			body.WriteString("\n")
			body.WriteString(s.Warning.Render("⚠ sync_after_backup activo pero el perfil no tiene destinos remotos."))
		}
	}

	return body.String()
}

func (m *settingsModel) updateViewportContent() {
	bodyStr := m.buildBodyContent()
	m.viewport.SetContent(bodyStr)
	m.ensureActiveVisible(bodyStr)
}

func (m *settingsModel) load() {
	m.level = settingsLevelGroups
	m.groupIdx = 0
	m.fieldIdx = 0
	m.subIdx = 0
	m.editing = false
	m.prompting = false
	m.promptType = promptNone
	m.confirm = nil
	m.confirmType = confirmNone
	m.multi = nil
	m.dirty = false
	m.saved = false
	m.err = nil
	m.notice = ""
	m.input.Blur()
	if m.app != nil {
		m.cfg = m.app.GetSettings()
	}
	m.viewport.GotoTop()
	m.updateViewportContent()
}

// Grupos principales (Nivel 0)
const (
	groupProfiles = iota
	groupPlatforms
	groupSchedule
	groupDatabase
	groupObservability
	groupAdvanced
	groupCredentials
	totalGroups
)

func (m *settingsModel) groupTitle(idx int) string {
	switch idx {
	case groupProfiles:
		return "Perfiles de backup"
	case groupPlatforms:
		return "Plataformas de almacenamiento"
	case groupSchedule:
		return "Programación y Tareas"
	case groupDatabase:
		return "Base de datos SQL Server"
	case groupObservability:
		return "Observabilidad (Supabase)"
	case groupAdvanced:
		return "Concurrencia y Avanzado"
	case groupCredentials:
		return "Credenciales (R2)"
	default:
		return ""
	}
}

func (m *settingsModel) groupSummary(idx int) string {
	switch idx {
	case groupProfiles:
		return fmt.Sprintf("%d perfiles · activo: %s", len(m.cfg.Profiles), m.cfg.ActiveProfile)
	case groupPlatforms:
		r2 := "desactivado"
		if m.cfg.Cloudflare.Enabled {
			r2 = fmt.Sprintf("activo (%d copias)", m.cfg.Cloudflare.Keep)
		}
		unc := "desactivado"
		if m.cfg.RemoteServer.Enabled {
			unc = fmt.Sprintf("activo (%d copias)", m.cfg.RemoteServer.Keep)
		}
		return fmt.Sprintf("Local · R2: %s · UNC: %s", r2, unc)
	case groupSchedule:
		return fmt.Sprintf("modo: %s a las %s · sync post-backup: %v", m.cfg.Schedule.Mode, m.cfg.Schedule.TimeOfDay, m.cfg.Schedule.SyncAfterBackup)
	case groupDatabase:
		auth := "Windows Auth"
		if strings.ToLower(strings.TrimSpace(m.cfg.AuthMode)) == "sql" || strings.TrimSpace(m.cfg.User) != "" {
			auth = fmt.Sprintf("SQL (%s)", m.cfg.User)
		}
		return fmt.Sprintf("%s en %s (%s) · conserva %d copias", m.cfg.Database, m.cfg.Server, auth, m.cfg.Retain)
	case groupObservability:
		if !m.cfg.Supabase.Enabled {
			return "Desactivado"
		}
		keyStatus := "sin API key"
		if m.cfg.Supabase.APIKey != "" {
			keyStatus = "API key configurada"
		} else if os.Getenv("SUPABASE_KEY") != "" || os.Getenv("SUPABASE_API_KEY") != "" || os.Getenv("SUPABASE_ACCESS_TOKEN") != "" {
			keyStatus = "API key (env)"
		}
		return fmt.Sprintf("%s · %s", m.cfg.Supabase.URL, keyStatus)
	case groupAdvanced:
		return fmt.Sprintf("Login timeout: %ds · Backup timeout: %ds", m.cfg.LoginTimeoutSec, m.cfg.BackupTimeoutSec)
	case groupCredentials:
		return "Cifradas con Windows DPAPI (config.dat)"
	default:
		return ""
	}
}

func (m *settingsModel) groupHelp(idx int) string {
	switch idx {
	case groupProfiles:
		return "Crear, duplicar, renombrar, cambiar tipo o plataformas de cada perfil."
	case groupPlatforms:
		return "Configuración y diagnóstico de destinos: Local, Cloudflare R2 y Servidor UNC."
	case groupSchedule:
		return "Horarios, frecuencia, días de corrida y sincronización con el Programador de Windows."
	case groupDatabase:
		return "Parámetros del servidor SQL Server, base de datos y retención de copias locales."
	case groupObservability:
		return "Envío de telemetría y eventos de corrida a proyecto Supabase."
	case groupAdvanced:
		return "Timeouts de conexión, estado del lock de exclusión mutua y reintentos."
	case groupCredentials:
		return "Editar claves R2 cifradas con Windows Data Protection API (DPAPI)."
	default:
		return ""
	}
}

func (m *settingsModel) currentGroupName() string {
	return m.groupTitle(m.groupIdx)
}

func (m *settingsModel) subTitle() string {
	switch m.groupIdx {
	case groupPlatforms:
		switch m.fieldIdx {
		case 0:
			return "Destino Local"
		case 1:
			return "Cloudflare R2"
		case 2:
			return "Servidor UNC"
		}
	case groupSchedule:
		return "Tarea de Windows"
	case groupProfiles:
		if m.fieldIdx >= 0 && m.fieldIdx < len(m.cfg.Profiles) {
			return fmt.Sprintf("Overrides: %s", m.cfg.Profiles[m.fieldIdx].Name)
		}
	}
	return ""
}

// -------------------------------------------------------------
// Definición de campos para Nivel 1 y Nivel 2
// -------------------------------------------------------------

func (m *settingsModel) scheduleFields() []settingsField {
	return []settingsField{
		{
			Label: "Modo de cadencia", Kind: kindChoice, Options: []string{"daily", "weekly", "interval"},
			Help: "daily = diario a hora fija; weekly = días seleccionados; interval = cada N minutos.",
			Get:  func(s application.Settings) string { return s.Schedule.Mode },
			Set:  func(s *application.Settings, v string) error { s.Schedule.Mode = strings.ToLower(v); return nil },
		},
		{
			Label: "Hora de inicio (HH:MM)", Kind: kindText,
			Help: "Hora en formato 24 h (HH:MM) para modos daily y weekly.",
			Get:  func(s application.Settings) string { return s.Schedule.TimeOfDay },
			Set:  func(s *application.Settings, v string) error { s.Schedule.TimeOfDay = v; return nil },
		},
		{
			Label: "Días activos (weekly)", Kind: kindMulti, MultiType: "weekdays",
			Help: "Días de la semana para el modo weekly (navegá y marcá con Espacio).",
			Get:  func(s application.Settings) string { return joinWeekdays(s.Schedule.Weekdays) },
			Set:  func(s *application.Settings, v string) error { s.Schedule.Weekdays = splitList(v); return nil },
		},
		{
			Label: "Frecuencia (minutos)", Kind: kindInt,
			Help: "Intervalo en minutos para el modo interval (mínimo 5).",
			Get:  func(s application.Settings) string { return strconv.Itoa(s.Schedule.IntervalMinutes) },
			Set: func(s *application.Settings, v string) error {
				n, err := parseInt(v)
				if err != nil {
					return err
				}
				s.Schedule.IntervalMinutes = n
				return nil
			},
		},
		{
			Label: "Duración máxima (minutos)", Kind: kindInt,
			Help: "Corta la ejecución si excede este tiempo (0 = sin límite).",
			Get:  func(s application.Settings) string { return strconv.Itoa(s.Schedule.MaxDurationMin) },
			Set: func(s *application.Settings, v string) error {
				n, err := parseInt(v)
				if err != nil {
					return err
				}
				s.Schedule.MaxDurationMin = n
				return nil
			},
		},
		{
			Label: "Nombre de tarea Windows", Kind: kindText,
			Help: "Nombre identificador en el Programador de tareas de Windows.",
			Get:  func(s application.Settings) string { return s.Schedule.TaskName },
			Set:  func(s *application.Settings, v string) error { s.Schedule.TaskName = v; return nil },
		},
		{
			Label: "Sync tras backup", Kind: kindBool, Options: boolOptions(),
			Help: "Si está activo, al terminar el backup local se sincroniza automáticamente a destinos remotos.",
			Get:  func(s application.Settings) string { return strconv.FormatBool(s.Schedule.SyncAfterBackup) },
			Set: func(s *application.Settings, v string) error {
				b, err := parseBool(v)
				if err != nil {
					return err
				}
				s.Schedule.SyncAfterBackup = b
				return nil
			},
		},
		{
			Label: "Programación activa", Kind: kindBool, Options: boolOptions(),
			Help: "Habilita o deshabilita la ejecución programada automática en Windows.",
			Get:  func(s application.Settings) string { return strconv.FormatBool(s.Schedule.Enabled) },
			Set: func(s *application.Settings, v string) error {
				b, err := parseBool(v)
				if err != nil {
					return err
				}
				s.Schedule.Enabled = b
				return nil
			},
		},
	}
}

func (m *settingsModel) databaseFields() []settingsField {
	return []settingsField{
		{
			Label: "Servidor SQL", Kind: kindText,
			Help: "Host o host\\instancia de SQL Server, ej: Caproba01\\vbadilla.",
			Get:  func(s application.Settings) string { return s.Server },
			Set:  func(s *application.Settings, v string) error { s.Server = v; return nil },
		},
		{
			Label: "Base de datos", Kind: kindText,
			Help: "Nombre de la base a respaldar (solo letras, dígitos y guión bajo).",
			Get:  func(s application.Settings) string { return s.Database },
			Set:  func(s *application.Settings, v string) error { s.Database = v; return nil },
		},
		{
			Label: "Carpeta local de backups", Kind: kindText,
			Help: "Ruta local donde SQL Server escribe los .bak (el servicio SQL debe tener acceso de escritura).",
			Get:  func(s application.Settings) string { return s.BackupDir },
			Set:  func(s *application.Settings, v string) error { s.BackupDir = v; return nil },
		},
		{
			Label: "Copias locales a conservar", Kind: kindInt,
			Help: "Cantidad de copias .bak que se retienen localmente.",
			Get:  func(s application.Settings) string { return strconv.Itoa(s.Retain) },
			Set: func(s *application.Settings, v string) error {
				n, err := parseInt(v)
				if err != nil {
					return err
				}
				s.Retain = n
				return nil
			},
		},
		{
			Label: "Timeout de conexión (s)", Kind: kindInt,
			Help: "Segundos de espera para establecer conexión con SQL Server.",
			Get:  func(s application.Settings) string { return strconv.Itoa(s.LoginTimeoutSec) },
			Set: func(s *application.Settings, v string) error {
				n, err := parseInt(v)
				if err != nil {
					return err
				}
				s.LoginTimeoutSec = n
				return nil
			},
		},
		{
			Label: "Timeout de backup (s)", Kind: kindInt,
			Help: "Segundos máximos permitidos para el BACKUP DATABASE (0 = sin límite).",
			Get:  func(s application.Settings) string { return strconv.Itoa(s.BackupTimeoutSec) },
			Set: func(s *application.Settings, v string) error {
				n, err := parseInt(v)
				if err != nil {
					return err
				}
				s.BackupTimeoutSec = n
				return nil
			},
		},
		{
			Label: "Modo de autenticación", Kind: kindChoice, Options: []string{"windows", "sql"},
			Help: "windows = Windows Integrated Auth (SSO); sql = Usuario y contraseña.",
			Get: func(s application.Settings) string {
				if s.AuthMode == "" {
					return "windows"
				}
				return s.AuthMode
			},
			Set: func(s *application.Settings, v string) error { s.AuthMode = strings.ToLower(v); return nil },
		},
		{
			Label: "Usuario SQL (modo sql)", Kind: kindText,
			Help: "Usuario para autenticación SQL (ej: sa). Dejar vacío si se usa Windows Auth.",
			Get:  func(s application.Settings) string { return s.User },
			Set:  func(s *application.Settings, v string) error { s.User = v; return nil },
		},
		{
			Label: "Contraseña SQL (modo sql)", Kind: kindPassword,
			Help: "Contraseña para autenticación SQL.",
			Get:  func(s application.Settings) string { return s.Password },
			Set:  func(s *application.Settings, v string) error { s.Password = v; return nil },
		},
		{
			Label: "Ruta en motor SQL (Docker)", Kind: kindText,
			Help: "Ruta que ve el motor SQL (ej: /var/opt/mssql/backup). Dejar vacío para usar carpeta local.",
			Get:  func(s application.Settings) string { return s.SQLBackupDir },
			Set:  func(s *application.Settings, v string) error { s.SQLBackupDir = v; return nil },
		},
	}
}

func (m *settingsModel) observabilityFields() []settingsField {
	return []settingsField{
		{
			Label: "Enviar eventos a Supabase", Kind: kindBool, Options: boolOptions(),
			Help: "Activa el reporte centralizado de corridas y fallas a Supabase.",
			Get:  func(s application.Settings) string { return strconv.FormatBool(s.Supabase.Enabled) },
			Set: func(s *application.Settings, v string) error {
				b, err := parseBool(v)
				if err != nil {
					return err
				}
				s.Supabase.Enabled = b
				return nil
			},
		},
		{
			Label: "URL del proyecto Supabase", Kind: kindText,
			Help: "URL HTTPS del proyecto Supabase que recibe los eventos.",
			Get:  func(s application.Settings) string { return s.Supabase.URL },
			Set:  func(s *application.Settings, v string) error { s.Supabase.URL = strings.TrimSpace(v); return nil },
		},
		{
			Label: "Clave API de Supabase", Kind: kindPassword,
			Help: "Token anon o service_role. Se oculta por seguridad; si está vacía busca SUPABASE_KEY.",
			Get:  func(s application.Settings) string { return s.Supabase.APIKey },
			Set:  func(s *application.Settings, v string) error { s.Supabase.APIKey = strings.TrimSpace(v); return nil },
			Display: func(s application.Settings) string {
				if s.Supabase.APIKey != "" {
					return "••••••••"
				}
				if os.Getenv("SUPABASE_KEY") != "" || os.Getenv("SUPABASE_API_KEY") != "" || os.Getenv("SUPABASE_ACCESS_TOKEN") != "" {
					return "(hereda SUPABASE_KEY)"
				}
				return "(no configurada)"
			},
		},
		{
			Label: "Timeout HTTP Supabase (s)", Kind: kindInt,
			Help: "Tiempo máximo por petición a la API de Supabase.",
			Get:  func(s application.Settings) string { return strconv.Itoa(s.Supabase.TimeoutSec) },
			Set: func(s *application.Settings, v string) error {
				n, err := parseInt(v)
				if err != nil {
					return err
				}
				s.Supabase.TimeoutSec = n
				return nil
			},
		},
	}
}

func (m *settingsModel) advancedFields() []settingsField {
	return []settingsField{
		{
			Label: "Timeout sincronización remota", Kind: kindText,
			Help: "Tiempo máximo de sincronización remota calculado según backends activos.",
			Get: func(s application.Settings) string {
				if m.app != nil {
					return m.app.RemoteSyncTimeout().String()
				}
				return "300s"
			},
			Set: func(s *application.Settings, v string) error { return nil },
		},
	}
}

func (m *settingsModel) platformLocalFields() []settingsField {
	return []settingsField{
		{
			Label: "Carpeta local", Kind: kindText,
			Help: "Ruta local donde SQL Server escribe los .bak.",
			Get:  func(s application.Settings) string { return s.BackupDir },
			Set:  func(s *application.Settings, v string) error { s.BackupDir = v; return nil },
		},
		{
			Label: "Copias a conservar", Kind: kindInt,
			Help: "Retención de copias .bak locales.",
			Get:  func(s application.Settings) string { return strconv.Itoa(s.Retain) },
			Set: func(s *application.Settings, v string) error {
				n, err := parseInt(v)
				if err != nil {
					return err
				}
				s.Retain = n
				return nil
			},
		},
	}
}

func (m *settingsModel) platformR2Fields() []settingsField {
	return []settingsField{
		{
			Label: "Subida a Cloudflare R2", Kind: kindBool, Options: boolOptions(),
			Help: "Activa o desactiva la réplica en la nube R2.",
			Get:  func(s application.Settings) string { return strconv.FormatBool(s.Cloudflare.Enabled) },
			Set: func(s *application.Settings, v string) error {
				b, err := parseBool(v)
				if err != nil {
					return err
				}
				s.Cloudflare.Enabled = b
				return nil
			},
		},
		{
			Label: "Copias en la nube (keep)", Kind: kindInt,
			Help: "Cantidad de copias retenidas en Cloudflare R2.",
			Get:  func(s application.Settings) string { return strconv.Itoa(s.Cloudflare.Keep) },
			Set: func(s *application.Settings, v string) error {
				n, err := parseInt(v)
				if err != nil {
					return err
				}
				s.Cloudflare.Keep = n
				return nil
			},
		},
		{
			Label: "Timeout de subida (s)", Kind: kindInt,
			Help: "Tiempo límite en segundos para subir a R2.",
			Get:  func(s application.Settings) string { return strconv.Itoa(s.Cloudflare.TimeoutSec) },
			Set: func(s *application.Settings, v string) error {
				n, err := parseInt(v)
				if err != nil {
					return err
				}
				s.Cloudflare.TimeoutSec = n
				return nil
			},
		},
		{
			Label: "Reintentos de subida", Kind: kindInt,
			Help: "Reintentos con backoff exponencial ante fallas de red con R2.",
			Get:  func(s application.Settings) string { return strconv.Itoa(s.Cloudflare.UploadRetries) },
			Set: func(s *application.Settings, v string) error {
				n, err := parseInt(v)
				if err != nil {
					return err
				}
				s.Cloudflare.UploadRetries = n
				return nil
			},
		},
	}
}

func (m *settingsModel) platformUNCFields() []settingsField {
	return []settingsField{
		{
			Label: "Copia a servidor UNC", Kind: kindBool, Options: boolOptions(),
			Help: "Activa o desactiva la copia al recurso compartido de red.",
			Get:  func(s application.Settings) string { return strconv.FormatBool(s.RemoteServer.Enabled) },
			Set: func(s *application.Settings, v string) error {
				b, err := parseBool(v)
				if err != nil {
					return err
				}
				s.RemoteServer.Enabled = b
				return nil
			},
		},
		{
			Label: "Ruta de red UNC", Kind: kindText,
			Help: "Ruta compartida de Windows, ej: \\\\servidor\\backups.",
			Get:  func(s application.Settings) string { return s.RemoteServer.RemotePath },
			Set:  func(s *application.Settings, v string) error { s.RemoteServer.RemotePath = v; return nil },
		},
		{
			Label: "Copias en servidor (keep)", Kind: kindInt,
			Help: "Cantidad de copias retenidas en el recurso compartido.",
			Get:  func(s application.Settings) string { return strconv.Itoa(s.RemoteServer.Keep) },
			Set: func(s *application.Settings, v string) error {
				n, err := parseInt(v)
				if err != nil {
					return err
				}
				s.RemoteServer.Keep = n
				return nil
			},
		},
		{
			Label: "Timeout de copia UNC (s)", Kind: kindInt,
			Help: "Tiempo límite en segundos para copiar al recurso compartido.",
			Get:  func(s application.Settings) string { return strconv.Itoa(s.RemoteServer.TimeoutSec) },
			Set: func(s *application.Settings, v string) error {
				n, err := parseInt(v)
				if err != nil {
					return err
				}
				s.RemoteServer.TimeoutSec = n
				return nil
			},
		},
	}
}

func (m *settingsModel) profileOverrideFields() []settingsField {
	if m.fieldIdx < 0 || m.fieldIdx >= len(m.cfg.Profiles) {
		return nil
	}
	profileIdx := m.fieldIdx

	return []settingsField{
		{
			Label: "R2: Copias retenidas (keep)", Kind: kindInt,
			Help:  fmt.Sprintf("Copias a retener en Cloudflare R2 para este perfil (vacío o 0 = hereda global: %d).", m.cfg.Cloudflare.Keep),
			Get: func(s application.Settings) string {
				if profileIdx < len(s.Profiles) && s.Profiles[profileIdx].Overrides.Cloudflare != nil && s.Profiles[profileIdx].Overrides.Cloudflare.Keep != nil {
					return strconv.Itoa(*s.Profiles[profileIdx].Overrides.Cloudflare.Keep)
				}
				return ""
			},
			Display: func(s application.Settings) string {
				if profileIdx < len(s.Profiles) && s.Profiles[profileIdx].Overrides.Cloudflare != nil && s.Profiles[profileIdx].Overrides.Cloudflare.Keep != nil {
					return fmt.Sprintf("%d (override)", *s.Profiles[profileIdx].Overrides.Cloudflare.Keep)
				}
				return fmt.Sprintf("Heredado (%d)", s.Cloudflare.Keep)
			},
			Set: func(s *application.Settings, v string) error {
				if profileIdx >= len(s.Profiles) {
					return fmt.Errorf("perfil no encontrado")
				}
				v = strings.TrimSpace(v)
				if v == "" || v == "0" || strings.EqualFold(v, "heredar") {
					if s.Profiles[profileIdx].Overrides.Cloudflare != nil {
						s.Profiles[profileIdx].Overrides.Cloudflare.Keep = nil
					}
					m.cleanupOverrides(&s.Profiles[profileIdx])
					return nil
				}
				n, err := parseInt(v)
				if err != nil {
					return err
				}
				if n < 1 {
					return fmt.Errorf("la cantidad de copias debe ser >= 1 (o vacío para heredar)")
				}
				if s.Profiles[profileIdx].Overrides.Cloudflare == nil {
					s.Profiles[profileIdx].Overrides.Cloudflare = &application.PlatformCloudflareOverride{}
				}
				s.Profiles[profileIdx].Overrides.Cloudflare.Keep = &n
				return nil
			},
		},
		{
			Label: "R2: Timeout subida (s)", Kind: kindInt,
			Help:  fmt.Sprintf("Tiempo límite en segundos para subir a R2 (vacío = hereda global: %d s).", m.cfg.Cloudflare.TimeoutSec),
			Get: func(s application.Settings) string {
				if profileIdx < len(s.Profiles) && s.Profiles[profileIdx].Overrides.Cloudflare != nil && s.Profiles[profileIdx].Overrides.Cloudflare.TimeoutSec != nil {
					return strconv.Itoa(*s.Profiles[profileIdx].Overrides.Cloudflare.TimeoutSec)
				}
				return ""
			},
			Display: func(s application.Settings) string {
				if profileIdx < len(s.Profiles) && s.Profiles[profileIdx].Overrides.Cloudflare != nil && s.Profiles[profileIdx].Overrides.Cloudflare.TimeoutSec != nil {
					return fmt.Sprintf("%d s (override)", *s.Profiles[profileIdx].Overrides.Cloudflare.TimeoutSec)
				}
				return fmt.Sprintf("Heredado (%d s)", s.Cloudflare.TimeoutSec)
			},
			Set: func(s *application.Settings, v string) error {
				if profileIdx >= len(s.Profiles) {
					return fmt.Errorf("perfil no encontrado")
				}
				v = strings.TrimSpace(v)
				if v == "" || strings.EqualFold(v, "heredar") {
					if s.Profiles[profileIdx].Overrides.Cloudflare != nil {
						s.Profiles[profileIdx].Overrides.Cloudflare.TimeoutSec = nil
					}
					m.cleanupOverrides(&s.Profiles[profileIdx])
					return nil
				}
				n, err := parseInt(v)
				if err != nil {
					return err
				}
				if n < 0 {
					return fmt.Errorf("el timeout no puede ser negativo")
				}
				if s.Profiles[profileIdx].Overrides.Cloudflare == nil {
					s.Profiles[profileIdx].Overrides.Cloudflare = &application.PlatformCloudflareOverride{}
				}
				s.Profiles[profileIdx].Overrides.Cloudflare.TimeoutSec = &n
				return nil
			},
		},
		{
			Label: "R2: Reintentos de subida", Kind: kindInt,
			Help:  fmt.Sprintf("Reintentos en caso de falla transitoria a R2 (vacío = hereda global: %d).", m.cfg.Cloudflare.UploadRetries),
			Get: func(s application.Settings) string {
				if profileIdx < len(s.Profiles) && s.Profiles[profileIdx].Overrides.Cloudflare != nil && s.Profiles[profileIdx].Overrides.Cloudflare.UploadRetries != nil {
					return strconv.Itoa(*s.Profiles[profileIdx].Overrides.Cloudflare.UploadRetries)
				}
				return ""
			},
			Display: func(s application.Settings) string {
				if profileIdx < len(s.Profiles) && s.Profiles[profileIdx].Overrides.Cloudflare != nil && s.Profiles[profileIdx].Overrides.Cloudflare.UploadRetries != nil {
					return fmt.Sprintf("%d (override)", *s.Profiles[profileIdx].Overrides.Cloudflare.UploadRetries)
				}
				return fmt.Sprintf("Heredado (%d)", s.Cloudflare.UploadRetries)
			},
			Set: func(s *application.Settings, v string) error {
				if profileIdx >= len(s.Profiles) {
					return fmt.Errorf("perfil no encontrado")
				}
				v = strings.TrimSpace(v)
				if v == "" || strings.EqualFold(v, "heredar") {
					if s.Profiles[profileIdx].Overrides.Cloudflare != nil {
						s.Profiles[profileIdx].Overrides.Cloudflare.UploadRetries = nil
					}
					m.cleanupOverrides(&s.Profiles[profileIdx])
					return nil
				}
				n, err := parseInt(v)
				if err != nil {
					return err
				}
				if n < 0 {
					return fmt.Errorf("los reintentos no pueden ser negativos")
				}
				if s.Profiles[profileIdx].Overrides.Cloudflare == nil {
					s.Profiles[profileIdx].Overrides.Cloudflare = &application.PlatformCloudflareOverride{}
				}
				s.Profiles[profileIdx].Overrides.Cloudflare.UploadRetries = &n
				return nil
			},
		},
		{
			Label: "UNC: Copias en servidor (keep)", Kind: kindInt,
			Help:  fmt.Sprintf("Copias retenidas en recurso compartido para este perfil (vacío o 0 = hereda global: %d).", m.cfg.RemoteServer.Keep),
			Get: func(s application.Settings) string {
				if profileIdx < len(s.Profiles) && s.Profiles[profileIdx].Overrides.RemoteServer != nil && s.Profiles[profileIdx].Overrides.RemoteServer.Keep != nil {
					return strconv.Itoa(*s.Profiles[profileIdx].Overrides.RemoteServer.Keep)
				}
				return ""
			},
			Display: func(s application.Settings) string {
				if profileIdx < len(s.Profiles) && s.Profiles[profileIdx].Overrides.RemoteServer != nil && s.Profiles[profileIdx].Overrides.RemoteServer.Keep != nil {
					return fmt.Sprintf("%d (override)", *s.Profiles[profileIdx].Overrides.RemoteServer.Keep)
				}
				return fmt.Sprintf("Heredado (%d)", s.RemoteServer.Keep)
			},
			Set: func(s *application.Settings, v string) error {
				if profileIdx >= len(s.Profiles) {
					return fmt.Errorf("perfil no encontrado")
				}
				v = strings.TrimSpace(v)
				if v == "" || v == "0" || strings.EqualFold(v, "heredar") {
					if s.Profiles[profileIdx].Overrides.RemoteServer != nil {
						s.Profiles[profileIdx].Overrides.RemoteServer.Keep = nil
					}
					m.cleanupOverrides(&s.Profiles[profileIdx])
					return nil
				}
				n, err := parseInt(v)
				if err != nil {
					return err
				}
				if n < 1 {
					return fmt.Errorf("la cantidad de copias debe ser >= 1 (o vacío para heredar)")
				}
				if s.Profiles[profileIdx].Overrides.RemoteServer == nil {
					s.Profiles[profileIdx].Overrides.RemoteServer = &application.PlatformServerOverride{}
				}
				s.Profiles[profileIdx].Overrides.RemoteServer.Keep = &n
				return nil
			},
		},
		{
			Label: "UNC: Timeout de copia (s)", Kind: kindInt,
			Help:  fmt.Sprintf("Tiempo límite en segundos para copiar a servidor UNC (vacío = hereda global: %d s).", m.cfg.RemoteServer.TimeoutSec),
			Get: func(s application.Settings) string {
				if profileIdx < len(s.Profiles) && s.Profiles[profileIdx].Overrides.RemoteServer != nil && s.Profiles[profileIdx].Overrides.RemoteServer.TimeoutSec != nil {
					return strconv.Itoa(*s.Profiles[profileIdx].Overrides.RemoteServer.TimeoutSec)
				}
				return ""
			},
			Display: func(s application.Settings) string {
				if profileIdx < len(s.Profiles) && s.Profiles[profileIdx].Overrides.RemoteServer != nil && s.Profiles[profileIdx].Overrides.RemoteServer.TimeoutSec != nil {
					return fmt.Sprintf("%d s (override)", *s.Profiles[profileIdx].Overrides.RemoteServer.TimeoutSec)
				}
				return fmt.Sprintf("Heredado (%d s)", s.RemoteServer.TimeoutSec)
			},
			Set: func(s *application.Settings, v string) error {
				if profileIdx >= len(s.Profiles) {
					return fmt.Errorf("perfil no encontrado")
				}
				v = strings.TrimSpace(v)
				if v == "" || strings.EqualFold(v, "heredar") {
					if s.Profiles[profileIdx].Overrides.RemoteServer != nil {
						s.Profiles[profileIdx].Overrides.RemoteServer.TimeoutSec = nil
					}
					m.cleanupOverrides(&s.Profiles[profileIdx])
					return nil
				}
				n, err := parseInt(v)
				if err != nil {
					return err
				}
				if n < 0 {
					return fmt.Errorf("el timeout no puede ser negativo")
				}
				if s.Profiles[profileIdx].Overrides.RemoteServer == nil {
					s.Profiles[profileIdx].Overrides.RemoteServer = &application.PlatformServerOverride{}
				}
				s.Profiles[profileIdx].Overrides.RemoteServer.TimeoutSec = &n
				return nil
			},
		},
	}
}

func (m *settingsModel) cleanupOverrides(p *application.Profile) {
	if p.Overrides.Cloudflare != nil {
		if p.Overrides.Cloudflare.Keep == nil && p.Overrides.Cloudflare.TimeoutSec == nil && p.Overrides.Cloudflare.UploadRetries == nil {
			p.Overrides.Cloudflare = nil
		}
	}
	if p.Overrides.RemoteServer != nil {
		if p.Overrides.RemoteServer.Keep == nil && p.Overrides.RemoteServer.TimeoutSec == nil {
			p.Overrides.RemoteServer = nil
		}
	}
}

func (m *settingsModel) currentFields() []settingsField {
	switch m.level {
	case settingsLevelFields:
		switch m.groupIdx {
		case groupSchedule:
			return m.scheduleFields()
		case groupDatabase:
			return m.databaseFields()
		case groupObservability:
			return m.observabilityFields()
		case groupAdvanced:
			return m.advancedFields()
		}
	case settingsLevelSub:
		switch m.groupIdx {
		case groupPlatforms:
			switch m.fieldIdx {
			case 0:
				return m.platformLocalFields()
			case 1:
				return m.platformR2Fields()
			case 2:
				return m.platformUNCFields()
			}
		case groupProfiles:
			return m.profileOverrideFields()
		}
	}
	return nil
}

func (m *settingsModel) currentField() *settingsField {
	fields := m.currentFields()
	idx := m.fieldIdx
	if m.level == settingsLevelSub && (m.groupIdx == groupPlatforms || m.groupIdx == groupProfiles) {
		idx = m.subIdx
	}
	if idx < 0 || idx >= len(fields) {
		return nil
	}
	return &fields[idx]
}

// -------------------------------------------------------------
// Ciclo Update y Manejo de Teclado
// -------------------------------------------------------------

func (m *settingsModel) update(msg tea.Msg) (*settingsModel, tea.Cmd) {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		if m.editing && m.multi == nil {
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	}

	// 1. Manejo de confirmModel modal activo
	if m.confirm != nil {
		switch key.String() {
		case "left", "right", "tab", "h", "l":
			m.confirm.move()
			return m, nil
		case "enter":
			if m.confirm.ok {
				m.finishConfirm()
				return m, nil
			}
			m.confirm = nil
			m.confirmType = confirmNone
			return m, nil
		case "esc":
			m.confirm = nil
			m.confirmType = confirmNone
			return m, nil
		}
		return m, nil
	}

	// 2. Manejo de prompting (ingreso de texto modal para nuevo/renombrar perfil)
	if m.prompting {
		switch key.String() {
		case "esc":
			m.prompting = false
			m.promptType = promptNone
			m.input.Blur()
			return m, nil
		case "enter":
			val := strings.TrimSpace(m.input.Value())
			m.prompting = false
			m.input.Blur()
			m.finishPrompt(val)
			return m, nil
		default:
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}
	}

	// 3. Edición de un campo tipo kindMulti (checkboxes)
	if m.editing && m.multi != nil {
		switch key.String() {
		case "up", "k":
			m.multi.move(-1)
			return m, nil
		case "down", "j":
			m.multi.move(1)
			return m, nil
		case " ", "space", "enter":
			m.multi.toggle()
			return m, nil
		case "esc":
			m.applyMulti()
			return m, nil
		case "ctrl+s":
			m.applyMulti()
			m.save()
			return m, nil
		}
		return m, nil
	}

	// 4. Edición de un campo simple de texto / numérico
	if m.editing {
		switch key.String() {
		case "esc":
			m.editing = false
			m.input.Blur()
			return m, nil
		case "enter":
			m.applyInput()
			return m, nil
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}

	// 5. Navegación normal según nivel
	switch key.String() {
	case "pgup", "pgdown":
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd

	case "esc":
		m.saved = false
		m.err = nil
		m.notice = ""
		if m.level == settingsLevelSub {
			m.level = settingsLevelFields
			m.subIdx = 0
			m.viewport.GotoTop()
			m.updateViewportContent()
			return m, nil
		}
		if m.level == settingsLevelFields {
			m.level = settingsLevelGroups
			m.fieldIdx = 0
			m.viewport.GotoTop()
			m.updateViewportContent()
			return m, nil
		}
		return m, func() tea.Msg { return backToDashboardMsg{} }

	case "ctrl+s":
		m.save()
		return m, nil

	case "up", "k":
		m.move(-1)
		return m, nil

	case "down", "j":
		m.move(1)
		return m, nil

	case "enter", "right", "l", " ", "space":
		return m.activate()

	case "left", "h":
		if m.level == settingsLevelSub {
			m.level = settingsLevelFields
			m.subIdx = 0
			m.viewport.GotoTop()
			m.updateViewportContent()
			return m, nil
		}
		if m.level == settingsLevelFields {
			m.level = settingsLevelGroups
			m.fieldIdx = 0
			m.viewport.GotoTop()
			m.updateViewportContent()
			return m, nil
		}

	// Atajos contextuales para Perfiles (Nivel 1 de Perfiles)
	case "u", "U":
		if m.level == settingsLevelFields && m.groupIdx == groupProfiles {
			m.useActiveProfile()
			return m, nil
		}
	case "n", "N":
		if m.level == settingsLevelFields && m.groupIdx == groupProfiles {
			m.startNewProfilePrompt()
			return m, nil
		}
	case "d", "D":
		if m.level == settingsLevelFields && m.groupIdx == groupProfiles {
			m.duplicateProfile()
			return m, nil
		}
		if m.level == settingsLevelSub && m.groupIdx == groupSchedule {
			m.deleteWindowsTask()
			return m, nil
		}
	case "r", "R":
		if m.level == settingsLevelFields && m.groupIdx == groupProfiles {
			m.startRenameProfilePrompt()
			return m, nil
		}
	case "x", "X":
		if m.level == settingsLevelFields && m.groupIdx == groupProfiles {
			m.deleteProfile()
			return m, nil
		}
	case "o", "O":
		if m.level == settingsLevelFields && m.groupIdx == groupProfiles {
			if m.fieldIdx >= 0 && m.fieldIdx < len(m.cfg.Profiles) {
				m.level = settingsLevelSub
				m.subIdx = 0
				m.viewport.GotoTop()
				m.updateViewportContent()
				return m, nil
			}
		}

	// Atajos contextuales para Plataformas / Tareas (Probar conexión, Credenciales, Instalar tarea)
	case "t", "T":
		if m.level == settingsLevelFields && m.groupIdx == groupSchedule {
			// Entrar a subpantalla de tarea de Windows
			m.level = settingsLevelSub
			m.refreshWindowsTaskStatus()
			return m, nil
		}
		if m.level == settingsLevelSub && m.groupIdx == groupPlatforms {
			if m.fieldIdx == 1 {
				m.runPlatformCheck("Cloudflare")
				return m, nil
			} else if m.fieldIdx == 2 {
				m.runPlatformCheck("UNC")
				return m, nil
			}
		}
	case "c", "C":
		if m.level == settingsLevelSub && m.groupIdx == groupPlatforms && m.fieldIdx == 1 {
			return m, func() tea.Msg { return openCredentialsMsg{} }
		}
	case "i", "I":
		if m.level == settingsLevelSub && m.groupIdx == groupSchedule {
			m.installWindowsTask()
			return m, nil
		}
	}

	return m, nil
}

func (m *settingsModel) move(delta int) {
	m.saved = false
	m.err = nil

	switch m.level {
	case settingsLevelGroups:
		n := totalGroups
		m.groupIdx = (m.groupIdx + delta + n) % n
	case settingsLevelFields:
		switch m.groupIdx {
		case groupProfiles:
			n := len(m.cfg.Profiles)
			if n > 0 {
				m.fieldIdx = (m.fieldIdx + delta + n) % n
			}
		case groupPlatforms:
			m.fieldIdx = (m.fieldIdx + delta + 3) % 3
		default:
			fields := m.currentFields()
			if len(fields) > 0 {
				m.fieldIdx = (m.fieldIdx + delta + len(fields)) % len(fields)
			}
		}
	case settingsLevelSub:
		switch m.groupIdx {
		case groupPlatforms, groupProfiles:
			fields := m.currentFields()
			if len(fields) > 0 {
				m.subIdx = (m.subIdx + delta + len(fields)) % len(fields)
			}
		}
	}
	m.updateViewportContent()
}

func (m *settingsModel) activate() (*settingsModel, tea.Cmd) {
	m.saved = false
	m.err = nil

	if m.level == settingsLevelGroups {
		if m.groupIdx == groupCredentials {
			return m, func() tea.Msg { return openCredentialsMsg{} }
		}
		m.level = settingsLevelFields
		m.fieldIdx = 0
		m.viewport.GotoTop()
		m.updateViewportContent()
		return m, nil
	}

	if m.level == settingsLevelFields {
		if m.groupIdx == groupPlatforms {
			m.level = settingsLevelSub
			m.subIdx = 0
			m.viewport.GotoTop()
			m.updateViewportContent()
			return m, nil
		}
		if m.groupIdx == groupProfiles {
			// Alternar o marcar como activo
			m.useActiveProfile()
			m.updateViewportContent()
			return m, nil
		}
	}

	// Estamos en un campo editable (Nivel 1 o Nivel 2)
	f := m.currentField()
	if f == nil {
		return m, nil
	}

	switch f.Kind {
	case kindBool:
		cur, _ := parseBool(f.Get(m.cfg))
		return m, m.setField(f, strconv.FormatBool(!cur))
	case kindChoice:
		cur := f.Get(m.cfg)
		idx := 0
		for i, o := range f.Options {
			if o == cur {
				idx = i
				break
			}
		}
		next := f.Options[(idx+1)%len(f.Options)]
		return m, m.setField(f, next)
	case kindMulti:
		m.editing = true
		curValues := splitList(f.Get(m.cfg))
		if f.MultiType == "platforms" {
			ms := platformsMultiselect(curValues)
			m.multi = &ms
		} else {
			ms := weekdaysMultiselect(curValues)
			m.multi = &ms
		}
		return m, nil
	default:
		m.editing = true
		if f.Kind == kindPassword {
			m.input.EchoMode = textinput.EchoPassword
			m.input.EchoCharacter = '•'
		} else {
			m.input.EchoMode = textinput.EchoNormal
		}
		m.input.SetValue(f.Get(m.cfg))
		m.input.CursorEnd()
		return m, m.input.Focus()
	}
}

func (m *settingsModel) applyInput() {
	f := m.currentField()
	if f == nil {
		m.editing = false
		return
	}
	cmd := m.setField(f, m.input.Value())
	m.editing = false
	m.input.EchoMode = textinput.EchoNormal
	m.input.Blur()
	_ = cmd
}

func (m *settingsModel) applyMulti() {
	if m.multi == nil {
		m.editing = false
		return
	}
	f := m.currentField()
	if f != nil {
		checked := m.multi.checked()
		_ = f.Set(&m.cfg, strings.Join(checked, ","))
		m.dirty = true
		m.save()
	}
	m.editing = false
	m.multi = nil
}

func (m *settingsModel) setField(f *settingsField, raw string) tea.Cmd {
	err := f.Set(&m.cfg, raw)
	if err != nil {
		m.err = err
		m.saved = false
		return nil
	}
	m.err = nil
	m.dirty = true
	m.saved = false
	m.save()
	return nil
}

func (m *settingsModel) save() {
	m.err = nil
	m.saved = false

	// Validación visible de dependencias
	if strings.EqualFold(m.cfg.Schedule.Mode, "weekly") && len(m.cfg.Schedule.Weekdays) == 0 {
		m.err = fmt.Errorf("el modo weekly requiere al menos un día seleccionado")
		return
	}

	if m.app == nil {
		m.err = fmt.Errorf("no hay instancia de aplicación conectada")
		return
	}

	if err := m.app.SaveSettings(m.cfg); err != nil {
		m.dirty = true
		m.err = err
		return
	}
	m.dirty = false
	m.saved = true
}

// -------------------------------------------------------------
// Operaciones de Perfiles
// -------------------------------------------------------------

func (m *settingsModel) useActiveProfile() {
	if m.fieldIdx < 0 || m.fieldIdx >= len(m.cfg.Profiles) {
		return
	}
	p := m.cfg.Profiles[m.fieldIdx]
	m.cfg.ActiveProfile = p.Name
	if m.app != nil {
		_ = m.app.UseProfile(p.Name)
	}
	m.save()
	m.notice = fmt.Sprintf("Perfil activo cambiado a %q. Recordá reiniciar el agente para rearmar backends.", p.Name)
}

func (m *settingsModel) startNewProfilePrompt() {
	m.prompting = true
	m.promptType = promptNewProfile
	m.promptTitle = "Nombre del nuevo perfil:"
	m.input.SetValue("")
	m.input.Focus()
}

func (m *settingsModel) startRenameProfilePrompt() {
	if m.fieldIdx < 0 || m.fieldIdx >= len(m.cfg.Profiles) {
		return
	}
	cur := m.cfg.Profiles[m.fieldIdx].Name
	m.prompting = true
	m.promptType = promptRenameProfile
	m.promptTitle = fmt.Sprintf("Nuevo nombre para el perfil %q:", cur)
	m.input.SetValue(cur)
	m.input.Focus()
}

func (m *settingsModel) finishPrompt(val string) {
	switch m.promptType {
	case promptNewProfile:
		m.createProfile(val)
	case promptRenameProfile:
		m.renameProfile(val)
	}
	m.promptType = promptNone
	m.updateViewportContent()
}

func (m *settingsModel) createProfile(name string) {
	name = strings.TrimSpace(name)
	if name == "" {
		m.err = fmt.Errorf("el nombre del perfil no puede estar vacío")
		return
	}
	if _, ok := m.cfg.ProfileByName(name); ok {
		m.err = fmt.Errorf("ya existe un perfil con el nombre %q", name)
		return
	}

	platforms := m.cfg.EnabledPlatforms()
	kind := application.KindFull
	if strings.EqualFold(name, "dev") && len(platforms) == 1 {
		kind = application.KindDev
	}

	newP := application.Profile{
		Name:      name,
		Kind:      kind,
		Platforms: platforms,
	}
	m.cfg.Profiles = append(m.cfg.Profiles, newP)
	m.fieldIdx = len(m.cfg.Profiles) - 1
	m.dirty = true
	m.save()
	if m.err == nil {
		m.notice = fmt.Sprintf("Perfil %q creado correctamente.", name)
	}
}

func (m *settingsModel) renameProfile(newName string) {
	if m.fieldIdx < 0 || m.fieldIdx >= len(m.cfg.Profiles) {
		return
	}
	cur := m.cfg.Profiles[m.fieldIdx].Name
	newName = strings.TrimSpace(newName)
	if newName == "" || newName == cur {
		return
	}
	if _, ok := m.cfg.ProfileByName(newName); ok {
		m.err = fmt.Errorf("ya existe un perfil con el nombre %q", newName)
		return
	}
	m.cfg.Profiles[m.fieldIdx].Name = newName
	if m.cfg.ActiveProfile == cur {
		m.cfg.ActiveProfile = newName
	}
	m.dirty = true
	m.save()
	if m.err == nil {
		m.notice = fmt.Sprintf("Perfil renombrado a %q.", newName)
	}
}

func (m *settingsModel) duplicateProfile() {
	if m.fieldIdx < 0 || m.fieldIdx >= len(m.cfg.Profiles) {
		return
	}
	src := m.cfg.Profiles[m.fieldIdx]
	newName := src.Name + "_copia"
	dup := src
	dup.Name = newName
	m.cfg.Profiles = append(m.cfg.Profiles, dup)
	m.fieldIdx = len(m.cfg.Profiles) - 1
	m.dirty = true
	m.save()
	m.notice = fmt.Sprintf("Perfil %q duplicado como %q.", src.Name, newName)
	m.updateViewportContent()
}

func (m *settingsModel) deleteProfile() {
	if len(m.cfg.Profiles) <= 1 {
		m.err = fmt.Errorf("no es posible eliminar el único perfil existente")
		return
	}
	if m.fieldIdx < 0 || m.fieldIdx >= len(m.cfg.Profiles) {
		return
	}
	p := m.cfg.Profiles[m.fieldIdx]
	m.confirm = &confirmModel{
		title: "Eliminar Perfil",
		text:  fmt.Sprintf("¿Estás seguro de que querés eliminar el perfil %q?", p.Name),
		ok:    false,
	}
	m.confirmType = confirmDeleteProfile
}

func (m *settingsModel) executeDeleteProfile() {
	if m.fieldIdx < 0 || m.fieldIdx >= len(m.cfg.Profiles) {
		return
	}
	p := m.cfg.Profiles[m.fieldIdx]
	m.cfg.Profiles = append(m.cfg.Profiles[:m.fieldIdx], m.cfg.Profiles[m.fieldIdx+1:]...)
	if m.cfg.ActiveProfile == p.Name {
		m.cfg.ActiveProfile = m.cfg.Profiles[0].Name
	}
	if m.fieldIdx >= len(m.cfg.Profiles) {
		m.fieldIdx = len(m.cfg.Profiles) - 1
	}
	m.dirty = true
	m.save()
	if m.err == nil {
		m.notice = fmt.Sprintf("Perfil %q eliminado correctamente.", p.Name)
	}
}

// -------------------------------------------------------------
// Operaciones de Tareas de Windows (Scheduler)
// -------------------------------------------------------------

func (m *settingsModel) refreshWindowsTaskStatus() {
	taskName := m.cfg.TaskNameForProfile(m.cfg.ActiveProfile)
	st, err := scheduler.New().Status(context.Background(), taskName)
	if err != nil {
		m.taskStatusText = "Error consultando tarea"
		return
	}
	if !st.Installed {
		m.taskStatusText = "NO INSTALADA"
	} else {
		state := st.StateText
		if !st.Enabled {
			state += " (deshabilitada)"
		}
		m.taskStatusText = fmt.Sprintf("INSTALADA [%s]", state)
	}
}

func (m *settingsModel) installWindowsTask() {
	taskName := m.cfg.TaskNameForProfile(m.cfg.ActiveProfile)
	m.confirm = &confirmModel{
		title: "Instalar Tarea en Windows",
		text:  fmt.Sprintf("¿Desea registrar la tarea %q en el Programador de Windows?", taskName),
		ok:    true,
	}
	m.confirmType = confirmInstallWindowsTask
}

func (m *settingsModel) executeInstallWindowsTask() {
	exePath, err := os.Executable()
	if err != nil {
		m.err = err
		return
	}
	if !m.cfg.Schedule.Enabled {
		m.cfg.Schedule.Enabled = true
		m.dirty = true
		if m.app != nil {
			_ = m.app.SaveSettings(m.cfg)
		}
	}
	spec, err := scheduler.SpecForProfile(m.cfg, m.cfg.ActiveProfile, exePath)
	if err != nil {
		m.err = err
		return
	}
	err = scheduler.New().Install(context.Background(), spec)
	if err != nil {
		if pErr, ok := err.(*scheduler.PermissionError); ok {
			m.err = fmt.Errorf("permiso de administrador requerido. Ejecutá en PowerShell elevado:\n%s", pErr.Command)
		} else {
			m.err = err
		}
		return
	}
	m.notice = fmt.Sprintf("Tarea %q instalada exitosamente.", spec.TaskName)
	m.refreshWindowsTaskStatus()
}

func (m *settingsModel) deleteWindowsTask() {
	taskName := m.cfg.TaskNameForProfile(m.cfg.ActiveProfile)
	m.confirm = &confirmModel{
		title: "Eliminar Tarea de Windows",
		text:  fmt.Sprintf("¿Desea eliminar la tarea %q del Programador de Windows?", taskName),
		ok:    false,
	}
	m.confirmType = confirmDeleteWindowsTask
}

func (m *settingsModel) executeDeleteWindowsTask() {
	taskName := m.cfg.TaskNameForProfile(m.cfg.ActiveProfile)
	err := scheduler.New().Delete(context.Background(), taskName)
	if err != nil {
		if pErr, ok := err.(*scheduler.PermissionError); ok {
			m.err = fmt.Errorf("permiso de administrador requerido:\n%s", pErr.Command)
		} else {
			m.err = err
		}
		return
	}
	m.notice = fmt.Sprintf("Tarea %q eliminada de Windows.", taskName)
	m.refreshWindowsTaskStatus()
}

func (m *settingsModel) finishConfirm() {
	kind := m.confirmType
	m.confirm = nil
	m.confirmType = confirmNone

	switch kind {
	case confirmDeleteProfile:
		m.executeDeleteProfile()
	case confirmDeleteWindowsTask:
		m.executeDeleteWindowsTask()
	case confirmInstallWindowsTask:
		m.executeInstallWindowsTask()
	}
	m.updateViewportContent()
}

func (m *settingsModel) runPlatformCheck(target string) {
	if m.app == nil {
		return
	}
	m.notice = "Verificando salud de plataforma..."
	checks := m.app.CheckPlatforms(context.Background())
	for _, c := range checks {
		if strings.Contains(strings.ToLower(c.Name), strings.ToLower(target)) {
			if c.OK {
				m.notice = fmt.Sprintf("✔ Prueba exitosa [%s]: %s", c.Name, c.Detail)
				m.err = nil
			} else {
				m.err = fmt.Errorf("falló verificación [%s]: %s", c.Name, c.Detail)
			}
			return
		}
	}
}

// -------------------------------------------------------------
// Renderizado View
// -------------------------------------------------------------

func (m settingsModel) view() string {
	s := m.styles
	var b strings.Builder

	// Modal de confirmación tiene precedencia visual
	if m.confirm != nil {
		return m.confirm.view(s)
	}

	// Modal prompt para nuevo/renombrar perfil
	if m.prompting {
		b.WriteString(s.AppTitle.Render("AJUSTES › PERFILES"))
		b.WriteString("\n\n")
		b.WriteString(s.Subtitle.Render(m.promptTitle))
		b.WriteString("\n\n")
		b.WriteString(m.input.View())
		b.WriteString("\n\n")
		b.WriteString(s.HelpBar.Render(
			s.Key.Render("[Enter]") + " " + s.Desc.Render("Confirmar") + "  " +
				s.Key.Render("[Esc]") + " " + s.Desc.Render("Cancelar")))
		return s.Box.Render(b.String())
	}

	// Encabezado principal
	header := s.AppTitle.Render("AJUSTES")
	if m.dirty {
		header += "  " + s.Warning.Render("● sin guardar")
	} else if m.saved {
		header += "  " + s.Success.Render("✔ guardado")
	}
	b.WriteString(header)
	b.WriteString("\n")

	// Breadcrumb de 3 niveles
	switch m.level {
	case settingsLevelGroups:
		b.WriteString(s.Subtitle.Render("Ajustes"))
	case settingsLevelFields:
		b.WriteString(s.Subtitle.Render(fmt.Sprintf("Ajustes › %s", m.currentGroupName())))
	case settingsLevelSub:
		b.WriteString(s.Subtitle.Render(fmt.Sprintf("Ajustes › %s › %s", m.currentGroupName(), m.subTitle())))
	}
	b.WriteString("\n\n")

	// Contenido scrolleable a través del viewport
	bodyStr := m.buildBodyContent()
	m.viewport.SetContent(bodyStr)
	m.ensureActiveVisible(bodyStr)

	b.WriteString(m.viewport.View())
	b.WriteString("\n\n")

	b.WriteString(s.HelpBar.Render(strings.Join(m.helpKeys(), "  ")))
	b.WriteString("\n")
	b.WriteString(s.Muted.Render("  Los cambios se guardan en config.json. Backends y credenciales se rearman al reiniciar el agente."))

	return s.Box.Render(b.String())
}

func (m settingsModel) viewGroups(b *strings.Builder) {
	s := m.styles
	for i := 0; i < totalGroups; i++ {
		cursor := "  "
		title := m.groupTitle(i)
		if i == m.groupIdx {
			cursor = s.InputPrompt.Render("▶ ")
			title = s.Value.Render(title)
		} else {
			title = s.Desc.Render(title)
		}
		summary := s.Muted.Render(m.groupSummary(i))
		b.WriteString(fmt.Sprintf("%s%-30s %s\n", cursor, title, summary))
		if i == m.groupIdx {
			b.WriteString(s.Muted.Render("    " + m.groupHelp(i)))
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
}

func (m settingsModel) viewProfiles(b *strings.Builder) {
	s := m.styles
	b.WriteString(s.SectionHeader.Render("PERFILES DE BACKUP DISPONIBLES"))
	b.WriteString("\n\n")

	for i, p := range m.cfg.Profiles {
		cursor := "  "
		name := p.Name
		isActive := p.Name == m.cfg.ActiveProfile

		if i == m.fieldIdx {
			cursor = s.InputPrompt.Render("▶ ")
			name = s.Value.Render(p.Name)
		} else {
			name = s.Desc.Render(p.Name)
		}

		activeMarker := " "
		if isActive {
			activeMarker = s.Success.Render("● ACTIVO")
		}

		platforms := append([]string{"local"}, p.Platforms...)
		platStr := strings.Join(platforms, ", ")

		overridesSummary := ""
		if p.Overrides.Cloudflare != nil || p.Overrides.RemoteServer != nil {
			var ovParts []string
			if p.Overrides.Cloudflare != nil && (p.Overrides.Cloudflare.Keep != nil || p.Overrides.Cloudflare.TimeoutSec != nil || p.Overrides.Cloudflare.UploadRetries != nil) {
				ovParts = append(ovParts, "R2")
			}
			if p.Overrides.RemoteServer != nil && (p.Overrides.RemoteServer.Keep != nil || p.Overrides.RemoteServer.TimeoutSec != nil) {
				ovParts = append(ovParts, "UNC")
			}
			if len(ovParts) > 0 {
				overridesSummary = s.Warning.Render(fmt.Sprintf("  [overrides: %s]", strings.Join(ovParts, "/")))
			}
		}

		b.WriteString(fmt.Sprintf("%s%-18s  %-12s  [tipo: %-7s]  [destinos: %s]%s\n",
			cursor, name, activeMarker, p.Kind, platStr, overridesSummary))
		b.WriteString("\n")
	}

	b.WriteString(s.Muted.Render("Atajos: [U] Usar activo  [O] Overrides  [N] Nuevo  [D] Duplicar  [R] Renombrar  [X] Eliminar"))
	b.WriteString("\n")
}

func (m settingsModel) viewPlatformsMenu(b *strings.Builder) {
	s := m.styles
	b.WriteString(s.SectionHeader.Render("DESTINOS DE ALMACENAMIENTO"))
	b.WriteString("\n\n")

	items := []struct {
		title string
		desc  string
	}{
		{"Destino Local", "Carpeta en el servidor SQL donde se generan los archivos .bak."},
		{"Cloudflare R2 (nube)", "Almacenamiento remoto de objetos compatible con S3."},
		{"Servidor externo (UNC)", "Copia secundaria a carpeta compartida de red \\\\servidor\\recurso."},
	}

	for i, it := range items {
		cursor := "  "
		title := it.title
		if i == m.fieldIdx {
			cursor = s.InputPrompt.Render("▶ ")
			title = s.Value.Render(it.title)
		} else {
			title = s.Desc.Render(it.title)
		}
		b.WriteString(fmt.Sprintf("%s%-26s %s\n\n", cursor, title, s.Muted.Render(it.desc)))
	}
}

func (m settingsModel) viewWindowsTaskSub(b *strings.Builder) {
	s := m.styles
	b.WriteString(s.SectionHeader.Render("GESTIÓN DE TAREA PROGRAMADA DE WINDOWS"))
	b.WriteString("\n\n")

	taskName := m.cfg.TaskNameForProfile(m.cfg.ActiveProfile)
	b.WriteString(fmt.Sprintf("  %s %s\n", s.Label.Render("Perfil asociado:"), s.Value.Render(m.cfg.ActiveProfile)))
	b.WriteString(fmt.Sprintf("  %s %s\n", s.Label.Render("Nombre de tarea:"), s.Value.Render(taskName)))
	b.WriteString(fmt.Sprintf("  %s %s\n\n", s.Label.Render("Estado actual:  "), s.Value.Render(m.taskStatusText)))

	b.WriteString(s.Desc.Render("Esta acción instala o actualiza el ejecutable en el Programador de Tareas."))
	b.WriteString("\n")
	b.WriteString(s.Muted.Render("Tip: Podés abrir el Programador de Tareas en Windows ejecutando Win + R › taskschd.msc"))
	b.WriteString("\n\n")
	b.WriteString(s.Muted.Render("Atajos: [I] Instalar tarea  [D] Eliminar tarea  [Esc] Volver a Programación"))
	b.WriteString("\n")
}

func (m settingsModel) viewFields(b *strings.Builder) {
	s := m.styles
	fields := m.currentFields()
	if len(fields) == 0 {
		return
	}

	curIdx := m.fieldIdx
	if m.level == settingsLevelSub && (m.groupIdx == groupPlatforms || m.groupIdx == groupProfiles) {
		curIdx = m.subIdx
	}

	for i, f := range fields {
		selected := i == curIdx
		label := f.Label
		value := f.Get(m.cfg)
		if f.Display != nil {
			value = f.Display(m.cfg)
		}

		if f.Kind == kindBool {
			if v, err := parseBool(value); err == nil {
				value = boolText(v)
			}
		}
		if f.Kind == kindPassword && value != "" && f.Display == nil {
			value = "••••••••"
		}

		if selected && m.editing {
			if f.Kind == kindMulti && m.multi != nil {
				b.WriteString(s.InputPrompt.Render("▶ " + label))
				b.WriteString("\n")
				b.WriteString(viewMultiselect(*m.multi, s))
				b.WriteString(s.Muted.Render("    [↑/↓] Navegar  [Espacio/Enter] Alternar  [Esc] Guardar"))
				b.WriteString("\n\n")
				continue
			}

			b.WriteString(s.InputPrompt.Render("▶ " + label))
			b.WriteString("\n")
			b.WriteString("  ")
			b.WriteString(m.input.View())
			b.WriteString("\n\n")
			continue
		}

		if selected {
			b.WriteString(s.InputPrompt.Render(fmt.Sprintf("▶ %-30s", label)))
			b.WriteString(" ")
			b.WriteString(s.Value.Render(value))
			b.WriteString("\n")
			b.WriteString(s.Muted.Render("    " + f.Help))
			b.WriteString("\n\n")
		} else {
			b.WriteString(s.Label.Render(fmt.Sprintf("  %-30s", label)))
			b.WriteString(" ")
			b.WriteString(s.Desc.Render(value))
			b.WriteString("\n\n")
		}
	}

	// Atajos contextuales en plataformas y perfiles
	if m.level == settingsLevelSub && m.groupIdx == groupPlatforms {
		if m.fieldIdx == 1 {
			b.WriteString(s.Muted.Render("Atajos: [T] Probar conexión R2  [C] Cargar credenciales R2"))
			b.WriteString("\n")
		} else if m.fieldIdx == 2 {
			b.WriteString(s.Muted.Render("Atajos: [T] Probar acceso UNC"))
			b.WriteString("\n")
		}
	} else if m.level == settingsLevelSub && m.groupIdx == groupProfiles {
		b.WriteString(s.Muted.Render("Atajos: [Enter] Editar  [Esc] Volver a perfiles  (dejá el campo vacío para heredar)"))
		b.WriteString("\n")
	} else if m.level == settingsLevelFields && m.groupIdx == groupSchedule {
		b.WriteString(s.Muted.Render("Atajos: [T] Gestionar tarea en Windows"))
		b.WriteString("\n")
	}
}

func (m settingsModel) helpKeys() []string {
	s := m.styles
	if m.editing {
		if m.multi != nil {
			return []string{
				fmt.Sprintf("%s %s", s.Key.Render("[↑/↓]"), s.Desc.Render("Navegar")),
				fmt.Sprintf("%s %s", s.Key.Render("[Espacio]"), s.Desc.Render("Marcar")),
				fmt.Sprintf("%s %s", s.Key.Render("[Esc]"), s.Desc.Render("Confirmar")),
			}
		}
		return []string{
			fmt.Sprintf("%s %s", s.Key.Render("[Enter]"), s.Desc.Render("Confirmar valor")),
			fmt.Sprintf("%s %s", s.Key.Render("[Esc]"), s.Desc.Render("Cancelar")),
		}
	}

	if m.level == settingsLevelGroups {
		return []string{
			fmt.Sprintf("%s %s", s.Key.Render("[↑/↓]"), s.Desc.Render("Navegar")),
			fmt.Sprintf("%s %s", s.Key.Render("[Enter]"), s.Desc.Render("Abrir grupo")),
			fmt.Sprintf("%s %s", s.Key.Render("[Ctrl+S]"), s.Desc.Render("Guardar")),
			fmt.Sprintf("%s %s", s.Key.Render("[Esc]"), s.Desc.Render("Volver")),
		}
	}

	return []string{
		fmt.Sprintf("%s %s", s.Key.Render("[↑/↓]"), s.Desc.Render("Navegar")),
		fmt.Sprintf("%s %s", s.Key.Render("[Enter]"), s.Desc.Render("Editar / Alternar")),
		fmt.Sprintf("%s %s", s.Key.Render("[Ctrl+S]"), s.Desc.Render("Guardar")),
		fmt.Sprintf("%s %s", s.Key.Render("[Esc]"), s.Desc.Render("Atrás")),
	}
}
