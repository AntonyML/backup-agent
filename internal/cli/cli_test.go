package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"femucaribe-backup-agent/internal/application"
	"femucaribe-backup-agent/internal/config"

	"github.com/spf13/cobra"
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
	cmd := NewRootCmd(t.TempDir(), func(cfgPath, profile string) (*application.App, error) {
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
		t.Errorf("esperaba cÃ³digo ExitGeneralErr (%d), dio %d", ExitGeneralErr, ExitCodeForError(err))
	}
}

func TestRootCmd_Help_Success(t *testing.T) {
	app := testApp(t)
	cmd := NewRootCmd(t.TempDir(), func(cfgPath, profile string) (*application.App, error) {
		return app, nil
	})

	var outBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetArgs([]string{"--help"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("--help deberÃ­a salir sin error: %v", err)
	}
	if ExitCodeForError(err) != ExitOK {
		t.Errorf("cÃ³digo de salida para --help debe ser ExitOK")
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
		{errors.New("error genÃ©rico"), ExitGeneralErr},
	}

	for _, tc := range tests {
		got := ExitCodeForError(tc.err)
		if got != tc.code {
			t.Errorf("para error %v: esperaba cÃ³digo %d, dio %d", tc.err, tc.code, got)
		}
	}
}

func TestStatusCmd_Execution(t *testing.T) {
	app := testApp(t)
	cmd := NewRootCmd(t.TempDir(), func(cfgPath, profile string) (*application.App, error) {
		return app, nil
	})

	var outBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetArgs([]string{"status"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("status fallÃ³: %v", err)
	}

	outStr := outBuf.String()
	if !strings.Contains(outStr, "CONTABILIDAD") || !strings.Contains(outStr, "Base de datos:") {
		t.Errorf("salida de status inesperada:\n%s", outStr)
	}
}

func TestInteractive_LaunchAndQuit(t *testing.T) {
	app := testApp(t)
	in := strings.NewReader("q")
	var outBuf bytes.Buffer

	err := runInteractive(context.Background(), app, t.TempDir(), in, &outBuf)
	if err != nil {
		t.Fatalf("runInteractive fallÃ³: %v", err)
	}
}

// TestUnattendedStdinGuard verifica la garantÃ­a de no-stdin (D8): cualquier
// lectura de stdin en modo unattended debe fallar de inmediato.
func TestUnattendedStdinGuard(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader("dato inesperado\n"))
	cmd.SetErr(io.Discard)

	installUnattendedStdinGuard(cmd)

	if _, err := cmd.InOrStdin().Read(make([]byte, 1)); !errors.Is(err, errUnattended) {
		t.Fatalf("esperaba errUnattended al leer stdin, dio: %v", err)
	}
}

// TestComandosDeclaranUnattended verifica que backup y sync expongan el flag.
func TestComandosDeclaranUnattended(t *testing.T) {
	app := testApp(t)
	root := NewRootCmd(t.TempDir(), func(cfgPath, profile string) (*application.App, error) { return app, nil })

	for _, name := range []string{"backup", "sync"} {
		var found *cobra.Command
		for _, c := range root.Commands() {
			if c.Name() == name {
				found = c
				break
			}
		}
		if found == nil {
			t.Fatalf("no encontrÃ© el comando %q", name)
		}
		if found.Flags().Lookup("unattended") == nil {
			t.Errorf("el comando %q deberÃ­a exponer --unattended", name)
		}
	}
}


