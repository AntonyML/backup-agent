package ui

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"femucaribe-backup-agent/internal/application"
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
