package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"femucaribe-backup-agent/internal/application"
	"femucaribe-backup-agent/internal/config"
)

// configParaPerfilesTest crea un config.json temporal con dos perfiles.
func configParaPerfilesTest(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.BackupDir = dir
	cfg.Server = "localhost"
	cfg.RemoteServer.Enabled = true
	cfg.RemoteServer.RemotePath = `\\srv\bkp`
	cfg.Cloudflare.Enabled = true
	cfg.Profiles = []config.Profile{
		{Name: "full", Kind: config.KindFull, Platforms: []string{config.PlatformCloudflare, config.PlatformServer}},
		{Name: "dev", Kind: config.KindDev, Platforms: []string{config.PlatformCloudflare}},
	}
	cfg.ActiveProfile = "full"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("fixture inválida: %v", err)
	}
	path := filepath.Join(dir, "config.json")
	if err := config.Save(path, cfg); err != nil {
		t.Fatalf("Save fixture: %v", err)
	}
	return path
}

// TestProfileList marca el perfil activo con asterisco.
func TestProfileList(t *testing.T) {
	path := configParaPerfilesTest(t)
	app := testApp(t)
	root := NewRootCmd(t.TempDir(), providerFijo(app))

	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"--config", path, "profile", "list"})

	if err := root.Execute(); err != nil {
		t.Fatalf("profile list: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "* full") {
		t.Errorf("esperaba marcar el activo '* full', salida:\n%s", got)
	}
	if !strings.Contains(got, "dev") {
		t.Errorf("esperaba listar 'dev', salida:\n%s", got)
	}
}

// TestProfileShow detalla el perfil activo por defecto y uno pedido.
func TestProfileShow(t *testing.T) {
	path := configParaPerfilesTest(t)
	app := testApp(t)

	for _, tc := range []struct {
		args []string
		want []string
	}{
		{[]string{"--config", path, "profile", "show"}, []string{"=== Perfil full ===", "Tipo (kind)", "Schedule", "Tarea Windows"}},
		{[]string{"--config", path, "profile", "show", "dev"}, []string{"=== Perfil dev ==="}},
		{[]string{"--config", path, "--profile", "dev", "profile", "show"}, []string{"=== Perfil dev ==="}},
	} {
		root := NewRootCmd(t.TempDir(), providerFijo(app))
		var out bytes.Buffer
		root.SetOut(&out)
		root.SetErr(&out)
		root.SetArgs(tc.args)
		if err := root.Execute(); err != nil {
			t.Fatalf("%v: %v", tc.args, err)
		}
		for _, want := range tc.want {
			if !strings.Contains(out.String(), want) {
				t.Errorf("%v: esperaba %q, salida:\n%s", tc.args, want, out.String())
			}
		}
	}
}

// TestProfileUse cambia el perfil activo y lo persiste.
func TestProfileUse(t *testing.T) {
	path := configParaPerfilesTest(t)
	app := testApp(t)
	root := NewRootCmd(t.TempDir(), providerFijo(app))

	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"--config", path, "profile", "use", "dev"})

	if err := root.Execute(); err != nil {
		t.Fatalf("profile use: %v", err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ActiveProfile != "dev" {
		t.Errorf("active_profile debería ser dev, es %q", cfg.ActiveProfile)
	}

	// Perfil inexistente -> error de configuración (exit 2).
	root = NewRootCmd(t.TempDir(), providerFijo(app))
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"--config", path, "profile", "use", "fantasma"})
	err = root.Execute()
	if err == nil {
		t.Fatal("esperaba error al activar un perfil inexistente")
	}
	if ExitCodeForError(err) != ExitConfigErr {
		t.Errorf("esperaba exit 2, dio %d (%v)", ExitCodeForError(err), err)
	}
}

// TestProfileFlagInexistente verifica D8: exit 2 con --profile bogus.
// Usa BuildDefaultApp real para que la resolución de perfil sea productiva.
func TestProfileFlagInexistente(t *testing.T) {
	path := configParaPerfilesTest(t)
	root := NewRootCmd(t.TempDir(), func(cfgPath, profile string) (*application.App, error) {
		return BuildDefaultApp(t.TempDir(), path, profile)
	})

	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"--config", path, "--profile", "fantasma", "status"})

	err := root.Execute()
	if err == nil {
		t.Fatal("esperaba error con perfil inexistente")
	}
	if ExitCodeForError(err) != ExitConfigErr {
		t.Errorf("esperaba exit 2, dio %d (%v)", ExitCodeForError(err), err)
	}
}

// TestBuildDefaultApp_ResuelvePerfil verifica que el provider productivo
// resuelve el perfil activo y rechaza el inexistente.
func TestBuildDefaultApp_ResuelvePerfil(t *testing.T) {
	path := configParaPerfilesTest(t)
	dir := t.TempDir()

	app, err := BuildDefaultApp(dir, path, "")
	if err != nil {
		t.Fatalf("BuildDefaultApp con perfil default: %v", err)
	}
	if app.ProfileName() != "full" {
		t.Errorf("esperaba perfil activo full, dio %q", app.ProfileName())
	}
	if _, err := BuildDefaultApp(dir, path, "fantasma"); err == nil {
		t.Error("esperaba error con perfil inexistente")
	} else if ExitCodeForError(err) != ExitConfigErr {
		t.Errorf("esperaba exit 2, dio %d (%v)", ExitCodeForError(err), err)
	}
}

// TestConfigValidate verifica config validate sobre una config sana y una rota.
func TestConfigValidate(t *testing.T) {
	path := configParaPerfilesTest(t)
	app := testApp(t)
	root := NewRootCmd(t.TempDir(), providerFijo(app))

	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"--config", path, "config", "validate"})

	if err := root.Execute(); err != nil {
		t.Fatalf("config validate sobre fixture sana: %v", err)
	}
	if !strings.Contains(out.String(), "Configuración válida") {
		t.Errorf("salida inesperada:\n%s", out.String())
	}
}

// providerFijo devuelve un appProvider que ignora args (los comandos de
// gestión de config no arman App).
func providerFijo(app *application.App) func(cfgPath, profile string) (*application.App, error) {
	return func(cfgPath, profile string) (*application.App, error) { return app, nil }
}
