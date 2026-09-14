// Package scheduler integra el agente con el Programador de tareas de Windows
// vía schtasks.exe (D7). La tarea se registra desde un XML generado a partir del
// ScheduleConfig del perfil (D1/D2), lo que permite fijar ExecutionTimeLimit
// (MaxDurationMin) y MultipleInstancesPolicy=IgnoreNew, que schtasks.exe no
// expone por línea de comandos.
//
// El paquete no conoce perfiles ni la capa de aplicación: recibe una Spec ya
// resuelta. Toda la lógica de mapeo es pura y testeable; el único efecto de
// lado es la invocación de schtasks.exe.
package scheduler

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// weekdayNames mapea los tokens de config.ScheduleConfig.Weekdays a los
// nombres de elemento XML de Task Scheduler.
var weekdayNames = map[string]string{
	"mon": "Monday",
	"tue": "Tuesday",
	"wed": "Wednesday",
	"thu": "Thursday",
	"fri": "Friday",
	"sat": "Saturday",
	"sun": "Sunday",
}

// Args ejecuta las acciones del agente en modo desatendido.
const unattendedArgs = "backup --unattended"

// Spec describe la tarea de Windows derivada del schedule de un perfil.
type Spec struct {
	// TaskName es el nombre de la tarea (ej: FEMUCARIBE-Backup-full).
	TaskName string
	// ExePath es la ruta absoluta del ejecutable del agente.
	ExePath string
	// ProfileName se agrega como --profile a la acción de la tarea.
	ProfileName string
	// Enabled=false deshabilita la tarea sin borrarla.
	Enabled bool
	// Mode es daily|weekly|interval.
	Mode string
	// TimeOfDay es HH:MM (modos daily y weekly).
	TimeOfDay string
	// Weekdays son los días activos en modo weekly (mon..sun).
	Weekdays []string
	// IntervalMinutes es la frecuencia del modo interval (>= 5).
	IntervalMinutes int
	// MaxDurationMin > 0 se traduce a ExecutionTimeLimit PT<n>M.
	MaxDurationMin int
	// Description es la descripción visible en el Programador.
	Description string
}

// TriggerKind describe el tipo de disparador resultante.
type TriggerKind string

const (
	TriggerDaily    TriggerKind = "daily"
	TriggerWeekly   TriggerKind = "weekly"
	TriggerInterval TriggerKind = "interval"
)

// trigger es la representación intermedia del disparador (pura).
type trigger struct {
	kind     TriggerKind
	endpoint string // HH:MM (daily/weekly)
	days     []string
	everyMin int
}

// triggerFor valida el schedule y devuelve el trigger a usar.
func triggerFor(spec Spec) (trigger, error) {
	mode := strings.ToLower(strings.TrimSpace(spec.Mode))
	switch mode {
	case "daily":
		if err := validateTimeOfDay(spec.TimeOfDay); err != nil {
			return trigger{}, err
		}
		return trigger{kind: TriggerDaily, endpoint: spec.TimeOfDay}, nil
	case "weekly":
		if err := validateTimeOfDay(spec.TimeOfDay); err != nil {
			return trigger{}, err
		}
		if len(spec.Weekdays) == 0 {
			return trigger{}, fmt.Errorf("scheduler: el modo weekly requiere al menos un día")
		}
		days, err := normalizeWeekdays(spec.Weekdays)
		if err != nil {
			return trigger{}, err
		}
		return trigger{kind: TriggerWeekly, endpoint: spec.TimeOfDay, days: days}, nil
	case "interval":
		if spec.IntervalMinutes < 5 {
			return trigger{}, fmt.Errorf("scheduler: el modo interval requiere interval_minutes >= 5, recibí %d", spec.IntervalMinutes)
		}
		return trigger{kind: TriggerInterval, everyMin: spec.IntervalMinutes}, nil
	default:
		return trigger{}, fmt.Errorf("scheduler: modo de schedule %q inválido (daily, weekly o interval)", spec.Mode)
	}
}

func validateTimeOfDay(v string) error {
	if _, err := time.Parse("15:04", v); err != nil {
		return fmt.Errorf("scheduler: time_of_day %q inválido (formato HH:MM)", v)
	}
	return nil
}

// normalizeWeekdays traduce mon..sun a nombres XML, ordenados y sin duplicados.
func normalizeWeekdays(days []string) ([]string, error) {
	seen := map[string]bool{}
	for _, d := range days {
		key := strings.ToLower(strings.TrimSpace(d))
		name, ok := weekdayNames[key]
		if !ok {
			return nil, fmt.Errorf("scheduler: día %q inválido (mon..sun)", d)
		}
		seen[name] = true
	}
	if len(seen) == 0 {
		return nil, fmt.Errorf("scheduler: el modo weekly requiere al menos un día válido")
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out, nil
}

// ActionArgs devuelve los argumentos con los que la tarea invoca al agente.
func ActionArgs(spec Spec) string {
	if strings.TrimSpace(spec.ProfileName) == "" {
		return unattendedArgs
	}
	return unattendedArgs + " --profile " + spec.ProfileName
}

// CommandLine devuelve la línea de comando exacta que registra la tarea.
// Es la que se muestra al operador cuando falta permiso de administrador (D7).
func CommandLine(spec Spec) string {
	xmlPath := XMLFileName(spec.TaskName)
	return fmt.Sprintf("schtasks /Create /TN %q /XML %q /F", spec.TaskName, xmlPath)
}

// CommandLineDelete devuelve el comando de borrado (para degradación D7).
func CommandLineDelete(taskName string) string {
	return fmt.Sprintf("schtasks /Delete /TN %q /F", taskName)
}

// XMLFileName es el nombre del archivo XML temporal de la tarea.
func XMLFileName(taskName string) string {
	safe := strings.NewReplacer(`\`, "-", `/`, "-", `:`, "-", `*`, "-", `?`, "-", `"`, "-", "<", "-", ">", "-", "|", "-").Replace(taskName)
	return filepath.Join("%TEMP%", "FEMUCARIBE-Backup-"+safe+".xml")
}

// durationLimit devuelve el ExecutionTimeLimit ISO-8601 (PT<n>M) o "" si no hay límite.
func durationLimit(maxDurationMin int) string {
	if maxDurationMin <= 0 {
		return ""
	}
	if maxDurationMin%60 == 0 {
		return fmt.Sprintf("PT%dH", maxDurationMin/60)
	}
	return fmt.Sprintf("PT%dM", maxDurationMin)
}