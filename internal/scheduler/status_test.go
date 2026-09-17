package scheduler

import (
	"context"
	"errors"
	"strings"
	"testing"

	"femucaribe-backup-agent/internal/config"
)

// TestManager_Install_Invocacion verifica los argumentos con los que se llama a schtasks.
func TestManager_Install_Invocacion(t *testing.T) {
	fe := &fakeExec{}
	m := NewWithExecutor(fe)

	spec := baseSpec()
	spec.MaxDurationMin = 30
	if err := m.Install(context.Background(), spec); err != nil {
		t.Fatalf("Install: %v", err)
	}

	got := strings.Join(fe.lastArgs, " ")
	if !strings.Contains(got, "/Create") || !strings.Contains(got, "/TN") || !strings.Contains(got, "/XML") || !strings.Contains(got, "/F") {
		t.Errorf("argumentos inesperados: %v", fe.lastArgs)
	}
	if !strings.Contains(got, "FEMUCARIBE-Backup-full") {
		t.Errorf("debería usar el task name: %v", fe.lastArgs)
	}
}

// TestManager_Install_SinPermisos_DegradaConComando verifica D7: el error de
// permisos expone el comando exacto para copiar.
func TestManager_Install_SinPermisos_DegradaConComando(t *testing.T) {
	fe := &fakeExec{
		stderr: "ERROR: Access is denied.",
		err:    errors.New("exit status 1"),
	}
	m := NewWithExecutor(fe)

	err := m.Install(context.Background(), baseSpec())
	if err == nil {
		t.Fatal("esperaba error")
	}
	if !errors.Is(err, ErrPermission) {
		t.Fatalf("esperaba ErrPermission, dio: %v", err)
	}
	var perm *PermissionError
	if !errors.As(err, &perm) {
		t.Fatalf("esperaba *PermissionError, dio: %v", err)
	}
	if !strings.Contains(perm.Command, "schtasks /Create") || !strings.Contains(perm.Command, "FEMUCARIBE-Backup-full") {
		t.Errorf("el comando de degradación debe ser exacto y copiable: %q", perm.Command)
	}
}

// TestManager_Install_ErrorGenerico verifica que un fallo no clasificado no degrade.
func TestManager_Install_ErrorGenerico(t *testing.T) {
	fe := &fakeExec{stderr: "algo raro pasó", err: errors.New("exit status 1")}
	m := NewWithExecutor(fe)
	err := m.Install(context.Background(), baseSpec())
	if err == nil || errors.Is(err, ErrPermission) {
		t.Fatalf("esperaba error genérico, dio: %v", err)
	}
}

// TestManager_Delete_NoInstalada verifica el centinela ErrNotInstalled.
func TestManager_Delete_NoInstalada(t *testing.T) {
	fe := &fakeExec{stderr: "ERROR: The system cannot find the file specified.", err: errors.New("exit status 1")}
	m := NewWithExecutor(fe)
	if err := m.Delete(context.Background(), "T"); !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("esperaba ErrNotInstalled, dio: %v", err)
	}
}

// TestManager_SetEnabled verifica los flags /ENABLE y /DISABLE.
func TestManager_SetEnabled(t *testing.T) {
	for _, tc := range []struct {
		enabled bool
		want    string
	}{
		{true, "/ENABLE"},
		{false, "/DISABLE"},
	} {
		fe := &fakeExec{}
		m := NewWithExecutor(fe)
		if err := m.SetEnabled(context.Background(), "T", tc.enabled); err != nil {
			t.Fatalf("SetEnabled: %v", err)
		}
		got := strings.Join(fe.lastArgs, " ")
		if !strings.Contains(got, "/Change") || !strings.Contains(got, tc.want) {
			t.Errorf("esperaba %s, argumentos: %v", tc.want, fe.lastArgs)
		}
	}
}

// TestCommandLine_Formato verifica el comando mostrado en la degradación (D7).
func TestCommandLine_Formato(t *testing.T) {
	spec := baseSpec()
	if got := CommandLine(spec); !strings.HasPrefix(got, "schtasks /Create /TN ") {
		t.Errorf("formato inesperado: %q", got)
	}
	if got := CommandLineDelete("X"); got != `schtasks /Delete /TN "X" /F` {
		t.Errorf("formato inesperado: %q", got)
	}
}

// TestManager_Status_NoInstalada verifica que la ausencia no sea error.
func TestManager_Status_NoInstalada(t *testing.T) {
	fe := &fakeExec{stderr: "ERROR: The system cannot find the file specified.", err: errors.New("exit status 1")}
	m := NewWithExecutor(fe)
	st, err := m.Status(context.Background(), "T")
	if err != nil {
		t.Fatalf("Status no debería fallar si la tarea no existe: %v", err)
	}
	if st.Installed {
		t.Error("no debería figurar instalada")
	}
}

// TestManager_Status_ParseoIngles verifica la lectura de /Query en inglés.
func TestManager_Status_ParseoIngles(t *testing.T) {
	fe := &fakeExec{stdout: "TaskName:                              \\FEMUCARIBE-Backup-full\r\nStatus:                                Ready\r\n"}
	m := NewWithExecutor(fe)
	st, err := m.Status(context.Background(), "FEMUCARIBE-Backup-full")
	if err != nil {
		t.Fatal(err)
	}
	if !st.Installed || !st.Enabled || st.Running {
		t.Errorf("estado inesperado: %+v", st)
	}
}

// TestManager_Status_ParseoEspanolDeshabilitada verifica la salida localizada.
func TestManager_Status_ParseoEspanolDeshabilitada(t *testing.T) {
	fe := &fakeExec{stdout: "Nombre de tarea:                       \\FEMUCARIBE-Backup-full\r\nEstado:                                Deshabilitada\r\n"}
	m := NewWithExecutor(fe)
	st, err := m.Status(context.Background(), "FEMUCARIBE-Backup-full")
	if err != nil {
		t.Fatal(err)
	}
	if !st.Installed {
		t.Error("debería figurar instalada")
	}
	if st.Enabled {
		t.Errorf("debería figurar deshabilitada, estado: %q", st.StateText)
	}
}

// TestManager_Status_ParseoEspanolEnEjecucion verifica el estado Running localizado.
func TestManager_Status_ParseoEspanolEnEjecucion(t *testing.T) {
	fe := &fakeExec{stdout: "Estado:                                En ejecución\r\n"}
	m := NewWithExecutor(fe)
	st, err := m.Status(context.Background(), "T")
	if err != nil {
		t.Fatal(err)
	}
	if !st.Running || !st.Enabled {
		t.Errorf("estado inesperado: %+v", st)
	}
}

// TestSpecForProfile_IntegracionConfig verifica D1/D2 de punta a punta.
func TestSpecForProfile_IntegracionConfig(t *testing.T) {
	cfg := config.Default()
	cfg.RemoteServer.Enabled = true
	cfg.RemoteServer.RemotePath = `\\srv\bkp`
	cfg.Cloudflare.Enabled = true
	cfg.Schedule = config.ScheduleConfig{
		Enabled: true, Mode: "weekly", TimeOfDay: "22:00",
		Weekdays: []string{"mon", "fri"}, TaskName: "FEMUCARIBE-Backup-Diario", MaxDurationMin: 45,
	}
	cfg, _ = cfg.EnsureMigrated()

	spec, err := SpecForProfile(cfg, config.InitialProfileName, `C:\Agente\backup-agent.exe`)
	if err != nil {
		t.Fatalf("SpecForProfile: %v", err)
	}
	if spec.TaskName != "FEMUCARIBE-Backup-Diario" {
		t.Errorf("debería respetar el TaskName explícito, dio %q", spec.TaskName)
	}
	if spec.Mode != "weekly" || spec.TimeOfDay != "22:00" || spec.MaxDurationMin != 45 {
		t.Errorf("schedule heredado incorrecto: %+v", spec)
	}

	// Sin TaskName explícito: convención FEMUCARIBE-Backup-<perfil>.
	cfg.Schedule.TaskName = ""
	spec, err = SpecForProfile(cfg, config.InitialProfileName, `C:\Agente\backup-agent.exe`)
	if err != nil {
		t.Fatal(err)
	}
	expectedTaskName := config.TaskNamePrefix + config.InitialProfileName
	if spec.TaskName != expectedTaskName {
		t.Errorf("esperaba convención de nombre %q, dio %q", expectedTaskName, spec.TaskName)
	}

	// Perfil inexistente -> error (mapea a exit 2 en CLI).
	if _, err := SpecForProfile(cfg, "no_existe", "x.exe"); err == nil {
		t.Error("esperaba error para perfil inexistente")
	}
}