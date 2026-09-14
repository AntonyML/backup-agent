package ui

import (
	"fmt"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"femucaribe-backup-agent/internal/application"
)

// settingsLevel indica si estamos viendo el índice de grupos o los campos de un grupo.
type settingsLevel int

const (
	settingsLevelGroups settingsLevel = iota
	settingsLevelFields
)

// fieldKind define cómo se edita un campo.
type fieldKind int

const (
	kindText fieldKind = iota
	kindInt
	kindBool
	kindChoice
	kindList
)

// settingsField es una fila editable de un grupo de ajustes.
// Get/Set traducen entre el valor mostrado (string) y la configuración tipada.
type settingsField struct {
	Label   string
	Help    string
	Kind    fieldKind
	Options []string
	Get     func(application.Settings) string
	Set     func(*application.Settings, string) error
}

// settingsGroup agrupa campos relacionados, como hace cualquier app de escritorio.
// credentials=true marca un grupo especial: navega a la pantalla de credenciales cifradas.
type settingsGroup struct {
	Title       string
	Help        string
	Credentials bool
	Summary     func(application.Settings) string
	Fields      []settingsField
}

// openCredentialsMsg indica al AppModel raíz que debe abrir la pantalla de credenciales.
type openCredentialsMsg struct{}

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

// settingsGroups devuelve la jerarquía completa de ajustes, en orden de uso real:
// primero lo cotidiano (copia, programación, destinos), después credenciales.
func settingsGroups() []settingsGroup {
	return []settingsGroup{
		{
			Title: "Copia de seguridad",
			Help:  "Origen de los datos y cuántas copias locales se conservan.",
			Summary: func(s application.Settings) string {
				return fmt.Sprintf("%s · conserva %d copias", s.Database, s.Retain)
			},
			Fields: []settingsField{
				{
					Label: "Carpeta local de backups", Kind: kindText,
					Help: "Ruta local donde SQL Server escribe los .bak (el servicio SQL debe poder escribirla).",
					Get:  func(s application.Settings) string { return s.BackupDir },
					Set:  func(s *application.Settings, v string) error { s.BackupDir = v; return nil },
				},
				{
					Label: "Servidor SQL", Kind: kindText,
					Help: "Host o host\\instancia del SQL Server, por ejemplo Caproba01\\vbadilla.",
					Get:  func(s application.Settings) string { return s.Server },
					Set:  func(s *application.Settings, v string) error { s.Server = v; return nil },
				},
				{
					Label: "Base de datos", Kind: kindText,
					Help: "Nombre de la base a respaldar. Solo letras, dígitos y guión bajo.",
					Get:  func(s application.Settings) string { return s.Database },
					Set:  func(s *application.Settings, v string) error { s.Database = v; return nil },
				},
				{
					Label: "Copias locales a conservar", Kind: kindInt,
					Help: "Cuántos .bak locales se mantienen antes de rotar el más viejo (retain).",
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
					Label: "Timeout de conexión SQL (s)", Kind: kindInt,
					Help: "Segundos máximos para conectar al SQL Server. 0 = sin límite.",
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
					Help: "Segundos máximos que puede durar el BACKUP DATABASE. 0 = sin límite.",
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
			},
		},
		{
			Title: "Programación",
			Help:  "Qué día, a qué hora y cada cuánto corren las tareas automáticas.",
			Summary: func(s application.Settings) string {
				if !s.Schedule.Enabled {
					return "Desactivada"
				}
				switch s.Schedule.Mode {
				case "interval":
					return fmt.Sprintf("Cada %d min", s.Schedule.IntervalMinutes)
				case "weekly":
					return fmt.Sprintf("%s a las %s", joinWeekdays(s.Schedule.Weekdays), s.Schedule.TimeOfDay)
				default:
					return "Todos los días a las " + s.Schedule.TimeOfDay
				}
			},
			Fields: []settingsField{
				{
					Label: "Tareas automáticas", Kind: kindBool, Options: boolOptions(),
					Help: "Activa o desactiva la ejecución programada de backup y sync.",
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
				{
					Label: "Modo", Kind: kindChoice, Options: []string{"daily", "weekly", "interval"},
					Help: "daily = todos los días · weekly = días elegidos · interval = cada N minutos.",
					Get:  func(s application.Settings) string { return s.Schedule.Mode },
					Set:  func(s *application.Settings, v string) error { s.Schedule.Mode = strings.ToLower(v); return nil },
				},
				{
					Label: "Hora de inicio", Kind: kindText,
					Help: "Hora de inicio en formato 24 h HH:MM (modos daily y weekly).",
					Get:  func(s application.Settings) string { return s.Schedule.TimeOfDay },
					Set:  func(s *application.Settings, v string) error { s.Schedule.TimeOfDay = v; return nil },
				},
				{
					Label: "Días (weekly)", Kind: kindList,
					Help: "Días activos separados por coma: mon,tue,wed,thu,fri,sat,sun.",
					Get:  func(s application.Settings) string { return joinWeekdays(s.Schedule.Weekdays) },
					Set:  func(s *application.Settings, v string) error { s.Schedule.Weekdays = splitList(v); return nil },
				},
				{
					Label: "Cada N minutos", Kind: kindInt,
					Help: "Frecuencia en minutos para el modo interval (mínimo 5).",
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
					Label: "Duración máxima (min)", Kind: kindInt,
					Help: "Corta la corrida si excede este tiempo. 0 = sin límite.",
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
					Label: "Nombre de la tarea", Kind: kindText,
					Help: "Nombre de la tarea en el Programador de tareas de Windows.",
					Get:  func(s application.Settings) string { return s.Schedule.TaskName },
					Set:  func(s *application.Settings, v string) error { s.Schedule.TaskName = v; return nil },
				},
				{
					Label: "Sync tras backup", Kind: kindBool, Options: boolOptions(),
					Help: "Si está activo, al terminar un backup se sincroniza automáticamente a los destinos remotos.",
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
			},
		},
		{
			Title: "Cloudflare R2 (nube)",
			Help:  "Cuántas copias se envían y se conservan en Cloudflare R2.",
			Summary: func(s application.Settings) string {
				if !s.Cloudflare.Enabled {
					return "Desactivado"
				}
				return fmt.Sprintf("Conserva %d copias · timeout %ds", s.Cloudflare.Keep, s.Cloudflare.TimeoutSec)
			},
			Fields: []settingsField{
				{
					Label: "Subida a la nube", Kind: kindBool, Options: boolOptions(),
					Help: "Activa la subida de los .bak a Cloudflare R2 (requiere credenciales configuradas).",
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
					Label: "Copias en la nube", Kind: kindInt,
					Help: "Cuántas copias se conservan en R2 antes de borrar la más antigua.",
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
					Help: "Tiempo máximo por operación contra R2.",
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
					Help: "Cantidad de reintentos ante fallos transitorios de red.",
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
			},
		},
		{
			Title: "Servidor externo (UNC)",
			Help:  "Copia adicional a un recurso compartido de red.",
			Summary: func(s application.Settings) string {
				if !s.RemoteServer.Enabled {
					return "Desactivado"
				}
				return fmt.Sprintf("%s · conserva %d", s.RemoteServer.RemotePath, s.RemoteServer.Keep)
			},
			Fields: []settingsField{
				{
					Label: "Copia a servidor", Kind: kindBool, Options: boolOptions(),
					Help: "Activa la copia a un recurso compartido de red (UNC).",
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
					Label: "Ruta remota", Kind: kindText,
					Help: `Ruta UNC destino, por ejemplo \\ServidorBackup\Backups\CONTABILIDAD\.`,
					Get:  func(s application.Settings) string { return s.RemoteServer.RemotePath },
					Set:  func(s *application.Settings, v string) error { s.RemoteServer.RemotePath = v; return nil },
				},
				{
					Label: "Copias en servidor", Kind: kindInt,
					Help: "Cuántas copias se conservan en el servidor externo.",
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
					Label: "Timeout de copia (s)", Kind: kindInt,
					Help: "Tiempo máximo para copiar cada archivo al recurso compartido.",
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
			},
		},
		{
			Title: "Eventos (Supabase)",
			Help:  "Registro centralizado del resultado de cada corrida.",
			Summary: func(s application.Settings) string {
				if !s.Supabase.Enabled {
					return "Desactivado"
				}
				return s.Supabase.URL
			},
			Fields: []settingsField{
				{
					Label: "Enviar eventos", Kind: kindBool, Options: boolOptions(),
					Help: "Activa el envío de eventos a Supabase. La API key se toma de variables de entorno.",
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
					Label: "URL del proyecto", Kind: kindText,
					Help: "URL https del proyecto Supabase que recibe los eventos.",
					Get:  func(s application.Settings) string { return s.Supabase.URL },
					Set:  func(s *application.Settings, v string) error { s.Supabase.URL = v; return nil },
				},
				{
					Label: "Timeout (s)", Kind: kindInt,
					Help: "Tiempo máximo de espera por request a Supabase.",
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
			},
		},
		{
			Title:       "Credenciales (R2)",
			Help:        "Endpoint, bucket y claves cifradas con DPAPI en config.dat.",
			Credentials: true,
			Summary: func(s application.Settings) string {
				return "Cifradas con Windows DPAPI"
			},
		},
	}
}

type settingsModel struct {
	app      *application.App
	styles   Styles
	level    settingsLevel
	cfg      application.Settings
	groupIdx int
	fieldIdx int
	editing  bool
	input    textinput.Model
	dirty    bool
	saved    bool
	err      error
}

func newSettingsModel(app *application.App, styles Styles) settingsModel {
	in := textinput.New()
	in.CharLimit = 200
	in.SetWidth(48)

	return settingsModel{
		app:    app,
		styles: styles,
		input:  in,
	}
}

// load recarga la configuración desde la aplicación (al entrar a la pantalla).
func (m *settingsModel) load() {
	m.level = settingsLevelGroups
	m.groupIdx = 0
	m.fieldIdx = 0
	m.editing = false
	m.dirty = false
	m.saved = false
	m.err = nil
	m.input.Blur()
	if m.app == nil {
		return
	}
	m.cfg = m.app.GetSettings()
}

func (m *settingsModel) groups() []settingsGroup { return settingsGroups() }

func (m *settingsModel) currentGroup() *settingsGroup {
	groups := m.groups()
	if m.groupIdx < 0 || m.groupIdx >= len(groups) {
		return nil
	}
	return &groups[m.groupIdx]
}

func (m *settingsModel) currentField() *settingsField {
	g := m.currentGroup()
	if g == nil || m.fieldIdx < 0 || m.fieldIdx >= len(g.Fields) {
		return nil
	}
	return &g.Fields[m.fieldIdx]
}

func (m *settingsModel) update(msg tea.Msg) (*settingsModel, tea.Cmd) {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		if m.editing {
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}
		return m, nil
	}

	// Edición de un campo de texto/numérico.
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

	switch key.String() {
	case "esc":
		if m.level == settingsLevelFields {
			m.level = settingsLevelGroups
			m.saved = false
			return m, nil
		}
		m.saved = false
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
	case "enter", "right", "l", " ":
		return m.activate()
	case "left", "h":
		if m.level == settingsLevelFields {
			m.level = settingsLevelGroups
			return m, nil
		}
	}
	return m, nil
}

func (m *settingsModel) move(delta int) {
	if m.level == settingsLevelGroups {
		n := len(m.groups())
		m.groupIdx = (m.groupIdx + delta + n) % n
		m.saved = false
		return
	}
	g := m.currentGroup()
	if g == nil || len(g.Fields) == 0 {
		return
	}
	m.fieldIdx = (m.fieldIdx + delta + len(g.Fields)) % len(g.Fields)
	m.saved = false
}

func (m *settingsModel) activate() (*settingsModel, tea.Cmd) {
	if m.level == settingsLevelGroups {
		g := m.currentGroup()
		if g == nil {
			return m, nil
		}
		if g.Credentials {
			return m, func() tea.Msg { return openCredentialsMsg{} }
		}
		if len(g.Fields) == 0 {
			return m, nil
		}
		m.level = settingsLevelFields
		m.fieldIdx = 0
		return m, nil
	}

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
	default:
		m.editing = true
		m.err = nil
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
	m.input.Blur()
	if m.err == nil {
		_ = cmd
	}
}

// setField aplica el valor y delega la persistencia: devuelve un comando de guardado.
// El guardado falla explícitamente (config.Validate) si el valor rompe una regla.
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

func (m settingsModel) view() string {
	s := m.styles
	var b strings.Builder

	header := s.AppTitle.Render("AJUSTES")
	if m.dirty {
		header += "  " + s.Warning.Render("● sin guardar")
	} else if m.saved {
		header += "  " + s.Success.Render("✔ guardado")
	}
	b.WriteString(header)
	b.WriteString("\n")

	if m.level == settingsLevelGroups {
		b.WriteString(s.Subtitle.Render("Elegí un grupo para revisar sus parámetros"))
		b.WriteString("\n")
	} else if g := m.currentGroup(); g != nil {
		b.WriteString(s.Subtitle.Render(fmt.Sprintf("Ajustes › %s", g.Title)))
		b.WriteString("\n")
	}
	b.WriteString("\n")

	if m.level == settingsLevelGroups {
		m.viewGroups(&b)
	} else {
		m.viewFields(&b)
	}

	if m.err != nil {
		b.WriteString("\n")
		b.WriteString(s.Error.Render(fmt.Sprintf("✖ %v", m.err)))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(s.HelpBar.Render(strings.Join(m.helpKeys(), "  ")))
	b.WriteString("\n")
	b.WriteString(s.Muted.Render("  Los cambios se guardan en config.json. Backends y credenciales se rearman al reiniciar el agente."))

	return s.Box.Render(b.String())
}

func (m settingsModel) viewGroups(b *strings.Builder) {
	s := m.styles
	for i, g := range m.groups() {
		cursor := "  "
		title := g.Title
		if i == m.groupIdx {
			cursor = s.InputPrompt.Render("▶ ")
			title = s.Value.Render(g.Title)
		} else {
			title = s.Desc.Render(g.Title)
		}
		summary := s.Muted.Render(g.Summary(m.cfg))
		b.WriteString(fmt.Sprintf("%s%-26s %s\n", cursor, title, summary))
		if i == m.groupIdx {
			b.WriteString(s.Muted.Render("    " + g.Help))
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
}

func (m settingsModel) viewFields(b *strings.Builder) {
	s := m.styles
	g := m.currentGroup()
	if g == nil {
		return
	}
	for i, f := range g.Fields {
		selected := i == m.fieldIdx
		label := f.Label
		value := f.Get(m.cfg)
		if f.Kind == kindBool {
			if v, err := parseBool(value); err == nil {
				value = boolText(v)
			}
		}
		if selected && m.editing {
			b.WriteString(s.InputPrompt.Render("▶ " + label))
			b.WriteString("\n")
			b.WriteString("  " + m.input.View())
			b.WriteString("\n\n")
			continue
		}
		if selected {
			b.WriteString(s.InputPrompt.Render(fmt.Sprintf("▶ %-28s", label)))
			b.WriteString(" ")
			b.WriteString(s.Value.Render(value))
			b.WriteString("\n")
			b.WriteString(s.Muted.Render("    " + f.Help))
			b.WriteString("\n\n")
		} else {
			b.WriteString(s.Label.Render(fmt.Sprintf("  %-28s", label)))
			b.WriteString(" ")
			b.WriteString(s.Desc.Render(value))
			b.WriteString("\n\n")
		}
	}
}

func (m settingsModel) helpKeys() []string {
	s := m.styles
	if m.editing {
		return []string{
			fmt.Sprintf("%s %s", s.Key.Render("[Enter]"), s.Desc.Render("Confirmar valor")),
			fmt.Sprintf("%s %s", s.Key.Render("[Esc]"), s.Desc.Render("Cancelar")),
		}
	}
	if m.level == settingsLevelGroups {
		return []string{
			fmt.Sprintf("%s %s", s.Key.Render("[↑/↓]"), s.Desc.Render("Navegar grupos")),
			fmt.Sprintf("%s %s", s.Key.Render("[Enter]"), s.Desc.Render("Abrir grupo")),
			fmt.Sprintf("%s %s", s.Key.Render("[Ctrl+S]"), s.Desc.Render("Guardar")),
			fmt.Sprintf("%s %s", s.Key.Render("[Esc]"), s.Desc.Render("Volver al Dashboard")),
		}
	}
	return []string{
		fmt.Sprintf("%s %s", s.Key.Render("[↑/↓]"), s.Desc.Render("Navegar campos")),
		fmt.Sprintf("%s %s", s.Key.Render("[Enter]"), s.Desc.Render("Editar o alternar")),
		fmt.Sprintf("%s %s", s.Key.Render("[Ctrl+S]"), s.Desc.Render("Guardar")),
		fmt.Sprintf("%s %s", s.Key.Render("[Esc]"), s.Desc.Render("Volver a grupos")),
	}
}

// backToDashboardMsg permite que el modelo de ajustes pida volver al dashboard sin acoplarse al modelo raíz.
type backToDashboardMsg struct{}
