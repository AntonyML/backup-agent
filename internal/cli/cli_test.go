package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"femucaribe-backup-agent/internal/application"
	"femucaribe-backup-agent/internal/config"
)

type dummyBackend struct{}

func (d dummyBackend) Name() string                                     { return "dummy" }
func (d dummyBackend) Upload(ctx context.Context, p string) error       { return nil }
func (d dummyBackend) Rotate(ctx context.Context, keep int) error       { return nil }
func (d dummyBackend) LatestRemote(ctx context.Context) (string, error) { return "", nil }

func testApp(t *testing.T) *application.App {
	tmpDir := t.TempDir()
	cfg := config.Config{
		BackupDir:        tmpDir,
		Server:           "localhost",
		Database:         "CONTABILIDAD",
		Retain:           3,
		LoginTimeoutSec:  15,
		BackupTimeoutSec: 60,
	}
	return application.New(application.Options{
		Config:       cfg,
		StatePath:    filepath.Join(tmpDir, "state.json"),
		LockPath:     filepath.Join(tmpDir, "agent.lock"),
		LogDir:       filepath.Join(tmpDir, "logs"),
		LocalBackend: dummyBackend{},
	})
}

func TestRootCmd_NoTTY_ShowsHelpAndFails(t *testing.T) {
	origIsTerminal := isTerminal
	defer func() { isTerminal = origIsTerminal }()
	isTerminal = func(f *os.File) bool { return false }

	app := testApp(t)
	cmd := NewRootCmd(t.TempDir(), func(cfgPath string) (*application.App, error) {
		return app, nil
	})

	var outBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	if !errors.Is(err, ErrNoTTY) {
		t.Fatalf("esperaba ErrNoTTY en entorno sin TTY y sin subcomando, dio: %v", err)
	}

	if ExitCodeForError(err) != ExitGeneralErr {
		t.Errorf("esperaba código ExitGeneralErr (%d), dio %d", ExitGeneralErr, ExitCodeForError(err))
	}
}

func TestRootCmd_Help_Success(t *testing.T) {
	app := testApp(t)
	cmd := NewRootCmd(t.TempDir(), func(cfgPath string) (*application.App, error) {
		return app, nil
	})

	var outBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetArgs([]string{"--help"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("--help debería salir sin error: %v", err)
	}
	if ExitCodeForError(err) != ExitOK {
		t.Errorf("código de salida para --help debe ser ExitOK")
	}
	if !strings.Contains(outBuf.String(), "backup-agent [flags]") {
		t.Errorf("salida de --help incompleta: %s", outBuf.String())
	}
}

func TestExitCodes_Mapping(t *testing.T) {
	tests := []struct {
		err  error
		code int
	}{
		{nil, ExitOK},
		{application.ErrAlreadyRanToday, ExitOK},
		{application.ErrInvalidConfig, ExitConfigErr},
		{ErrConfig, ExitConfigErr},
		{application.ErrPendingSync, ExitPendingSync},
		{application.ErrLocked, ExitLocked},
		{errors.New("error genérico"), ExitGeneralErr},
	}

	for _, tc := range tests {
		got := ExitCodeForError(tc.err)
		if got != tc.code {
			t.Errorf("para error %v: esperaba código %d, dio %d", tc.err, tc.code, got)
		}
	}
}

func TestStatusCmd_Execution(t *testing.T) {
	app := testApp(t)
	cmd := NewRootCmd(t.TempDir(), func(cfgPath string) (*application.App, error) {
		return app, nil
	})

	var outBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetArgs([]string{"status"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("status falló: %v", err)
	}

	outStr := outBuf.String()
	if !strings.Contains(outStr, "CONTABILIDAD") || !strings.Contains(outStr, "Base de datos:") {
		t.Errorf("salida de status inesperada:\n%s", outStr)
	}
}

func TestInteractive_Option3StatusThenExit(t *testing.T) {
	app := testApp(t)
	input := "3\n6\n"
	in := strings.NewReader(input)
	var outBuf bytes.Buffer

	err := runInteractive(context.Background(), app, t.TempDir(), in, &outBuf)
	if err != nil {
		t.Fatalf("runInteractive falló: %v", err)
	}

	outStr := outBuf.String()
	if !strings.Contains(outStr, "Estado Actual") || !strings.Contains(outStr, "CONTABILIDAD") {
		t.Errorf("menú interactivo no mostró estado:\n%s", outStr)
	}
	if !strings.Contains(outStr, "Saliendo...") {
		t.Errorf("menú interactivo no procesó salida:\n%s", outStr)
	}
}
