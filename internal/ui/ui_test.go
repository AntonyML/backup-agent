package ui

import (
	"context"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"femucaribe-backup-agent/internal/application"
	"femucaribe-backup-agent/internal/secrets"
)


func TestDashboard_RenderStates(t *testing.T) {
	styles := DefaultStyles()
	dash := newDashboardModel(styles)

	dash.setStatus(
		application.BackupStatus{
			Result:   "success",
			Filename: "CONTABILIDAD_20260910_1200.bak",
			SHA256:   "abcd1234efgh5678",
		},
		[]application.BackendStatus{
			{Name: "Local", Configured: true, LastSyncOK: true, StatusText: "OK"},
			{Name: "R2", Configured: true, PendingSync: true, StatusText: "PENDING"},
			{Name: "Server", Configured: false, StatusText: "Not configured"},
		},
	)

	view := dash.view()

	if !strings.Contains(view, "FEMUCARIBE BACKUP AGENT") {
		t.Errorf("título no presente en dashboard view")
	}
	if !strings.Contains(view, "EXITOSO") {
		t.Errorf("resultado EXITOSO no encontrado en view")
	}
	if !strings.Contains(view, "Local") || !strings.Contains(view, "OK") {
		t.Errorf("backend Local OK no encontrado en view")
	}
	if !strings.Contains(view, "R2") || !strings.Contains(view, "PENDING") {
		t.Errorf("backend R2 PENDING no encontrado en view")
	}
	if !strings.Contains(view, "Server") || !strings.Contains(view, "Not configured") {
		t.Errorf("backend Server no configurado no encontrado en view")
	}
}


func TestAppModel_Navigation(t *testing.T) {
	appModel := NewApp(nil, t.TempDir())

	// Inicialmente en Dashboard
	if appModel.screen != screenDashboard {
		t.Fatalf("esperaba screenDashboard inicial, dio %v", appModel.screen)
	}

	keyPress := func(key string) tea.KeyPressMsg {
		return tea.KeyPressMsg{Code: []rune(key)[0], Text: key}
	}

	// Presionar S -> ir a Status
	m, _ := appModel.Update(keyPress("s"))
	appModel = m.(AppModel)
	if appModel.screen != screenStatus {
		t.Fatalf("esperaba screenStatus tras presionar 's', dio %v", appModel.screen)
	}

	// Presionar Esc -> volver a Dashboard
	m, _ = appModel.Update(tea.KeyPressMsg{Code: 27, Text: "esc"})
	appModel = m.(AppModel)
	if appModel.screen != screenDashboard {
		t.Fatalf("esperaba screenDashboard tras presionar 'esc', dio %v", appModel.screen)
	}

	// Presionar L -> ir a Logs
	m, _ = appModel.Update(keyPress("l"))
	appModel = m.(AppModel)
	if appModel.screen != screenLogs {
		t.Fatalf("esperaba screenLogs tras presionar 'l', dio %v", appModel.screen)
	}

	// Presionar Q -> volver a Dashboard
	m, _ = appModel.Update(keyPress("q"))
	appModel = m.(AppModel)
	if appModel.screen != screenDashboard {
		t.Fatalf("esperaba screenDashboard tras presionar 'q', dio %v", appModel.screen)
	}

	// Presionar H -> ir a Help
	m, _ = appModel.Update(keyPress("h"))
	appModel = m.(AppModel)
	if appModel.screen != screenHelp {
		t.Fatalf("esperaba screenHelp tras presionar 'h', dio %v", appModel.screen)
	}

	// Cambiar tema de ayuda con tecla 2
	m, _ = appModel.Update(keyPress("2"))
	appModel = m.(AppModel)
	if appModel.help.currentTopic != topicBackup {
		t.Fatalf("esperaba topicBackup en ayuda, dio %v", appModel.help.currentTopic)
	}

	// Presionar Esc -> volver a Dashboard
	m, _ = appModel.Update(tea.KeyPressMsg{Code: 27, Text: "esc"})
	appModel = m.(AppModel)
	if appModel.screen != screenDashboard {
		t.Fatalf("esperaba screenDashboard, dio %v", appModel.screen)
	}
}

// TestStatic_NoForbiddenImports verifica que ningún archivo dentro de internal/ui importe
// internal/storage o internal/state directamente, cumpliendo la regla de Clean Architecture.
func TestStatic_NoForbiddenImports(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}

	fset := token.NewFileSet()
	for _, f := range files {
		node, err := parser.ParseFile(fset, f, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("error parseando %s: %v", f, err)
		}

		for _, imp := range node.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			if strings.Contains(path, "femucaribe-backup-agent/internal/storage") {
				t.Errorf("VIOLACIÓN DE ARQUITECTURA: archivo %s importa storage (%s)", f, path)
			}
			if strings.Contains(path, "femucaribe-backup-agent/internal/state") {
				t.Errorf("VIOLACIÓN DE ARQUITECTURA: archivo %s importa state (%s)", f, path)
			}
		}
	}
}

type mockAppConnector struct {
	settings Settings
	saved    Settings
}

func (m *mockAppConnector) GetSettings() Settings { return m.settings }
func (m *mockAppConnector) SaveSettings(s Settings) error {
	m.saved = s
	m.settings = s
	return nil
}
func (m *mockAppConnector) ConfigPath() string { return "config.json" }
func (m *mockAppConnector) GetTUIStatus(ctx context.Context) (application.BackupStatus, []application.BackendStatus, error) {
	return application.BackupStatus{}, nil, nil
}
func (m *mockAppConnector) Status(ctx context.Context) (*application.StatusReport, error) {
	return &application.StatusReport{Profile: "full"}, nil
}
func (m *mockAppConnector) TailLogs(ctx context.Context, n int) ([]string, error) { return nil, nil }
func (m *mockAppConnector) SaveR2Credentials(endpoint, bucket, accessKeyID, secretAccessKey string) error {
	return nil
}
func (m *mockAppConnector) GetR2Credentials() (*secrets.Credentials, error) { return nil, nil }
func (m *mockAppConnector) ProfileName() string                            { return "full" }
func (m *mockAppConnector) ListProfiles() []application.ProfileInfo {
	return []application.ProfileInfo{{Name: "full", Active: true}}
}
func (m *mockAppConnector) ActiveProfileDetail() application.ProfileDetail {
	return application.ProfileDetail{ProfileInfo: application.ProfileInfo{Name: "full", Active: true}}
}
func (m *mockAppConnector) UseProfile(name string) error             { return nil }
func (m *mockAppConnector) RemoteSyncTimeout() time.Duration        { return 300 * time.Second }
func (m *mockAppConnector) CheckPlatforms(ctx context.Context) []application.PlatformCheck {
	return []application.PlatformCheck{{Name: "Local", OK: true, Detail: "ok"}}
}
func (m *mockAppConnector) Backup(ctx context.Context, opts application.BackupOptions) error { return nil }
func (m *mockAppConnector) Sync(ctx context.Context, opts application.SyncOptions) error     { return nil }

func TestSettings_IntegrationFlow(t *testing.T) {
	initialSettings := application.Settings{
		ActiveProfile: "full",
		Profiles: []application.Profile{
			{
				Name:      "full",
				Kind:      application.KindFull,
				Platforms: []string{application.PlatformCloudflare},
			},
		},
		Schedule: application.ScheduleConfig{
			Enabled:   true,
			Mode:      "weekly",
			TimeOfDay: "23:00",
			Weekdays:  []string{"mon"},
		},
	}

	mock := &mockAppConnector{settings: initialSettings}
	appModel := NewApp(mock, t.TempDir())

	keyPress := func(key string) tea.KeyPressMsg {
		return tea.KeyPressMsg{Code: []rune(key)[0], Text: key}
	}

	// 1. En Dashboard, presionar C para entrar a Ajustes
	m, _ := appModel.Update(keyPress("c"))
	appModel = m.(AppModel)
	if appModel.screen != screenSettings {
		t.Fatalf("esperaba screenSettings tras presionar 'c', dio %v", appModel.screen)
	}

	// 2. Navegar a Programación (índice 2)
	m, _ = appModel.Update(keyPress("j"))
	appModel = m.(AppModel)
	m, _ = appModel.Update(keyPress("j"))
	appModel = m.(AppModel)
	if appModel.settings.groupIdx != groupSchedule {
		t.Fatalf("esperaba groupSchedule (2), dio %d", appModel.settings.groupIdx)
	}

	// 3. Entrar a Programación
	m, _ = appModel.Update(tea.KeyPressMsg{Code: 13, Text: "enter"})
	appModel = m.(AppModel)
	if appModel.settings.level != settingsLevelFields {
		t.Fatalf("esperaba settingsLevelFields, dio %v", appModel.settings.level)
	}

	// 4. Navegar a campo Días (weekly) (índice 2 en scheduleFields)
	m, _ = appModel.Update(keyPress("j"))
	appModel = m.(AppModel)
	m, _ = appModel.Update(keyPress("j"))
	appModel = m.(AppModel)
	if appModel.settings.fieldIdx != 2 {
		t.Fatalf("esperaba fieldIdx 2 (Días), dio %d", appModel.settings.fieldIdx)
	}

	// 5. Activar edición kindMulti con enter
	m, _ = appModel.Update(tea.KeyPressMsg{Code: 13, Text: "enter"})
	appModel = m.(AppModel)
	if !appModel.settings.editing || appModel.settings.multi == nil {
		t.Fatalf("esperaba editing = true y multi != nil")
	}

	// 6. Mover cursor en multiselect a 'tue' (cursor 1) y marcarlo con espacio
	m, _ = appModel.Update(keyPress("j"))
	appModel = m.(AppModel)
	m, _ = appModel.Update(keyPress(" "))
	appModel = m.(AppModel)

	// 7. Confirmar y guardar con Esc
	m, _ = appModel.Update(tea.KeyPressMsg{Code: 27, Text: "esc"})
	appModel = m.(AppModel)

	if appModel.settings.editing {
		t.Fatalf("esperaba salir de modo editing tras esc")
	}

	// Verificar que se persistió en el conector
	foundTue := false
	for _, d := range mock.saved.Schedule.Weekdays {
		if d == "tue" {
			foundTue = true
			break
		}
	}
	if !foundTue {
		t.Errorf("esperaba que 'tue' estuviera en Weekdays guardados, dio: %v", mock.saved.Schedule.Weekdays)
	}

	// 8. Esc para volver a grupos
	m, _ = appModel.Update(tea.KeyPressMsg{Code: 27, Text: "esc"})
	appModel = m.(AppModel)
	if appModel.settings.level != settingsLevelGroups {
		t.Fatalf("esperaba volver a settingsLevelGroups, dio %v", appModel.settings.level)
	}

	// 9. Esc para volver al dashboard
	m, cmd := appModel.Update(tea.KeyPressMsg{Code: 27, Text: "esc"})
	appModel = m.(AppModel)
	if cmd != nil {
		msg := cmd()
		m, _ = appModel.Update(msg)
		appModel = m.(AppModel)
	}
	if appModel.screen != screenDashboard {
		t.Fatalf("esperaba volver a screenDashboard, dio %v", appModel.screen)
	}
}

func TestSettings_CreateProfileDev(t *testing.T) {
	initialSettings := application.Settings{
		ActiveProfile: "full",
		Profiles: []application.Profile{
			{Name: "full", Kind: application.KindFull},
		},
		Schedule: application.ScheduleConfig{
			Enabled: true, Mode: "daily", TimeOfDay: "12:00",
		},
	}
	mock := &mockAppConnector{settings: initialSettings}
	appModel := NewApp(mock, t.TempDir())
	keyPress := func(key string) tea.KeyPressMsg {
		return tea.KeyPressMsg{Code: []rune(key)[0], Text: key}
	}

	// 1. Enter settings with 'c'
	m, _ := appModel.Update(keyPress("c"))
	appModel = m.(AppModel)

	// 2. Enter Perfiles (group 0) with enter
	m, _ = appModel.Update(tea.KeyPressMsg{Code: 13, Text: "enter"})
	appModel = m.(AppModel)

	// 3. Press 'n' to create new profile
	m, _ = appModel.Update(keyPress("n"))
	appModel = m.(AppModel)

	if !appModel.settings.prompting {
		t.Fatalf("expected prompting = true after pressing 'n'")
	}

	// 4. Type 'd', 'e', 'v'
	m, _ = appModel.Update(keyPress("d"))
	appModel = m.(AppModel)
	t.Logf("after 'd', value: %q", appModel.settings.input.Value())
	m, _ = appModel.Update(keyPress("e"))
	appModel = m.(AppModel)
	t.Logf("after 'e', value: %q", appModel.settings.input.Value())
	m, _ = appModel.Update(keyPress("v"))
	appModel = m.(AppModel)
	t.Logf("after 'v', value: %q", appModel.settings.input.Value())

	// 5. Press Enter to confirm
	m, _ = appModel.Update(tea.KeyPressMsg{Code: 13, Text: "enter"})
	appModel = m.(AppModel)

	t.Logf("prompting: %v, err: %v, notice: %v, profiles: %d",
		appModel.settings.prompting, appModel.settings.err, appModel.settings.notice, len(appModel.settings.cfg.Profiles))
	if appModel.settings.err != nil {
		t.Errorf("unexpected error: %v", appModel.settings.err)
	}
	if len(appModel.settings.cfg.Profiles) != 2 {
		t.Errorf("expected 2 profiles, got %d", len(appModel.settings.cfg.Profiles))
	}
}

