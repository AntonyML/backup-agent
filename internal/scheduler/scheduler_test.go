package scheduler

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// fakeExec permite simular schtasks sin Windows.
type fakeExec struct {
	lastArgs []string
	stdout   string
	stderr   string
	err      error
	calls    int
}

func (f *fakeExec) run(ctx context.Context, args ...string) (string, string, error) {
	f.calls++
	f.lastArgs = args
	return f.stdout, f.stderr, f.err
}

func baseSpec() Spec {
	return Spec{
		TaskName:    "FEMUCARIBE-Backup-full",
		ExePath:     `C:\Agente\backup-agent.exe`,
		ProfileName: "full",
		Enabled:     true,
		Mode:        "daily",
		TimeOfDay:   "23:00",
	}
}

// TestActionArgs verifica la acción instalada (D8: unattended + perfil).
func TestActionArgs(t *testing.T) {
	spec := baseSpec()
	if got := ActionArgs(spec); got != "backup --unattended --profile full" {
		t.Errorf("acción incorrecta: %q", got)
	}
	spec.ProfileName = ""
	if got := ActionArgs(spec); got != "backup --unattended" {
		t.Errorf("acción sin perfil incorrecta: %q", got)
	}
}

// TestTriggerFor_Mapeo cubre el mapeo ScheduleConfig -> disparador (sección 3.3).
func TestTriggerFor_Mapeo(t *testing.T) {
	cases := []struct {
		name    string
		spec    Spec
		want    TriggerKind
		wantErr bool
	}{
		{"daily", Spec{Mode: "daily", TimeOfDay: "23:00"}, TriggerDaily, false},
		{"weekly", Spec{Mode: "weekly", TimeOfDay: "22:30", Weekdays: []string{"mon", "wed", "fri"}}, TriggerWeekly, false},
		{"interval", Spec{Mode: "interval", IntervalMinutes: 60}, TriggerInterval, false},
		{"daily sin hora es inválido", Spec{Mode: "daily"}, TriggerDaily, true},
		{"weekly sin días es inválido", Spec{Mode: "weekly", TimeOfDay: "22:30"}, TriggerWeekly, true},
		{"weekly con día inválido es inválido", Spec{Mode: "weekly", TimeOfDay: "22:30", Weekdays: []string{"lunes"}}, TriggerWeekly, true},
		{"interval < 5 es inválido", Spec{Mode: "interval", IntervalMinutes: 3}, TriggerInterval, true},
		{"modo vacío es inválido", Spec{}, TriggerDaily, true},
		{"hora inválida es inválida", Spec{Mode: "daily", TimeOfDay: "25:99"}, TriggerDaily, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			trg, err := triggerFor(tc.spec)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("esperaba error")
				}
				return
			}
			if err != nil {
				t.Fatalf("no esperaba error: %v", err)
			}
			if trg.kind != tc.want {
				t.Errorf("esperaba %q, dio %q", tc.want, trg.kind)
			}
		})
	}
}

// TestNormalizeWeekdays verifica deduplicación y traducción a nombres XML.
func TestNormalizeWeekdays(t *testing.T) {
	got, err := normalizeWeekdays([]string{"mon", "WED", "mon", " fri "})
	if err != nil {
		t.Fatalf("no esperaba error: %v", err)
	}
	if strings.Join(got, ",") != "Friday,Monday,Wednesday" {
		t.Errorf("días normalizados incorrectos: %v", got)
	}
}

// TestDurationLimit verifica MaxDurationMin -> ExecutionTimeLimit (D9).
func TestDurationLimit(t *testing.T) {
	if got := durationLimit(0); got != "" {
		t.Errorf("0 debería significar sin límite, dio %q", got)
	}
	if got := durationLimit(90); got != "PT90M" {
		t.Errorf("90 min debería ser PT90M, dio %q", got)
	}
	if got := durationLimit(60); got != "PT1H" {
		t.Errorf("60 min debería ser PT1H, dio %q", got)
	}
}

// Compilación de referencia para el executor productivo.
var _ executor = SchtasksExec{}

var _ = context.Background
var _ = errors.Is

// TestTaskXML_Daily verifica el XML del disparador diario y sus settings.
func TestTaskXML_Daily(t *testing.T) {
	now := time.Date(2026, 9, 14, 10, 0, 0, 0, time.Local)
	spec := baseSpec()
	spec.MaxDurationMin = 90

	xml, err := TaskXML(spec, now)
	if err != nil {
		t.Fatalf("TaskXML: %v", err)
	}
	mustContain := []string{
		"<ScheduleByDay>",
		"<DaysInterval>1</DaysInterval>",
		"<StartBoundary>2026-09-14T23:00:00</StartBoundary>",
		"<ExecutionTimeLimit>PT90M</ExecutionTimeLimit>",
		"<MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>",
		`<Command>C:\Agente\backup-agent.exe</Command>`,
		"<Arguments>backup --unattended --profile full</Arguments>",
		"<Enabled>true</Enabled>",
	}
	for _, want := range mustContain {
		if !strings.Contains(xml, want) {
			t.Errorf("el XML debería contener %q", want)
		}
	}
}

// TestTaskXML_StartBoundaryRolla verifica que una hora ya pasada use mañana.
func TestTaskXML_StartBoundaryRolla(t *testing.T) {
	now := time.Date(2026, 9, 14, 23, 30, 0, 0, time.Local)
	spec := baseSpec() // 23:00 ya pasó
	xml, err := TaskXML(spec, now)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(xml, "<StartBoundary>2026-09-15T23:00:00</StartBoundary>") {
		t.Errorf("esperaba start boundary de mañana")
	}
}

// TestTaskXML_WeeklyEInterval verifican los otros dos disparadores.
func TestTaskXML_WeeklyEInterval(t *testing.T) {
	now := time.Date(2026, 9, 14, 10, 0, 0, 0, time.Local)

	weekly := baseSpec()
	weekly.Mode = "weekly"
	weekly.TimeOfDay = "22:30"
	weekly.Weekdays = []string{"mon", "wed", "fri"}
	xml, err := TaskXML(weekly, now)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"<ScheduleByWeek>", "<Monday />", "<Wednesday />", "<Friday />", "<WeeksInterval>1</WeeksInterval>"} {
		if !strings.Contains(xml, want) {
			t.Errorf("XML weekly debería contener %q", want)
		}
	}

	interval := baseSpec()
	interval.Mode = "interval"
	interval.IntervalMinutes = 60
	xml, err = TaskXML(interval, now)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"<TimeTrigger>", "<Interval>PT60M</Interval>"} {
		if !strings.Contains(xml, want) {
			t.Errorf("XML interval debería contener %q", want)
		}
	}
}

// TestTaskXML_Disabled verifica Enabled=false (D2/D9).
func TestTaskXML_Disabled(t *testing.T) {
	spec := baseSpec()
	spec.Enabled = false
	xml, err := TaskXML(spec, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(xml, "<Enabled>false</Enabled>") {
		t.Errorf("esperaba tarea deshabilitada en el XML")
	}
}

// TestEncodeUTF16LE verifica el BOM y la codificación esperada por schtasks.
func TestEncodeUTF16LE(t *testing.T) {
	out := EncodeUTF16LE("<Task/>")
	if len(out) < 2 || out[0] != 0xFF || out[1] != 0xFE {
		t.Fatalf("esperaba BOM UTF-16LE, obtuve %v", out)
	}
	if out[2] != '<' || out[3] != 0x00 {
		t.Errorf("codificación little-endian incorrecta: %v", out[:6])
	}
}