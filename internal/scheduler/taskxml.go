package scheduler

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf16"
)

// startBoundary arma el StartBoundary ISO-8601 que Task Scheduler espera, con
// la fecha de `now` y la hora del schedule. Para daily/weekly es la primera
// ocurrencia (hoy si aún no pasó, mañana si ya pasó); para interval solo importa
// que exista un ancla.
func startBoundary(now time.Time, hhmm string) (string, error) {
	hm, err := time.Parse("15:04", hhmm)
	if err != nil {
		return "", fmt.Errorf("scheduler: time_of_day %q inválido (formato HH:MM)", hhmm)
	}
	start := time.Date(now.Year(), now.Month(), now.Day(), hm.Hour(), hm.Minute(), 0, 0, time.Local)
	if !start.After(now) {
		start = start.AddDate(0, 0, 1)
	}
	return start.Format("2006-01-02T15:04:05"), nil
}

// TaskXML genera el XML de definición de tarea (schema 1.2) que se entrega a
// "schtasks /Create /XML". Incluye el disparador derivado del ScheduleConfig,
// ExecutionTimeLimit cuando hay MaxDurationMin y MultipleInstancesPolicy
// IgnoreNew para que el lock no se pise con corridas solapadas.
func TaskXML(spec Spec, now time.Time) (string, error) {
	if strings.TrimSpace(spec.TaskName) == "" {
		return "", fmt.Errorf("scheduler: task_name es obligatorio")
	}
	if strings.TrimSpace(spec.ExePath) == "" {
		return "", fmt.Errorf("scheduler: la ruta del ejecutable es obligatoria")
	}
	trg, err := triggerFor(spec)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-16"?>` + "\n")
	b.WriteString(`<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">` + "\n")
	b.WriteString("  <RegistrationInfo>\n")
	b.WriteString("    <Description>" + xmlEscape(descriptionFor(spec)) + "</Description>\n")
	b.WriteString("    <URI>\\" + xmlEscape(spec.TaskName) + "</URI>\n")
	b.WriteString("  </RegistrationInfo>\n")
	b.WriteString(triggerXML(trg, now))
	b.WriteString("  <Principals>\n")
	b.WriteString("    <Principal id=\"Author\">\n")
	b.WriteString("      <UserId>S-1-5-18</UserId>\n")
	b.WriteString("      <RunLevel>HighestAvailable</RunLevel>\n")
	b.WriteString("    </Principal>\n")
	b.WriteString("  </Principals>\n")
	b.WriteString(settingsXML(spec))
	b.WriteString("  <Actions Context=\"Author\">\n")
	b.WriteString("    <Exec>\n")
	b.WriteString("      <Command>" + xmlEscape(spec.ExePath) + "</Command>\n")
	b.WriteString("      <Arguments>" + xmlEscape(ActionArgs(spec)) + "</Arguments>\n")
	b.WriteString("      <WorkingDirectory>" + xmlEscape(dirOf(spec.ExePath)) + "</WorkingDirectory>\n")
	b.WriteString("    </Exec>\n")
	b.WriteString("  </Actions>\n")
	b.WriteString("</Task>\n")
	return b.String(), nil
}

// triggerXML genera el bloque <Triggers> según el tipo de disparador.
func triggerXML(trg trigger, now time.Time) string {
	var b strings.Builder
	b.WriteString("  <Triggers>\n")
	switch trg.kind {
	case TriggerDaily:
		start, _ := startBoundary(now, trg.endpoint)
		b.WriteString("    <CalendarTrigger>\n")
		b.WriteString("      <StartBoundary>" + start + "</StartBoundary>\n")
		b.WriteString("      <Enabled>true</Enabled>\n")
		b.WriteString("      <ScheduleByDay>\n        <DaysInterval>1</DaysInterval>\n      </ScheduleByDay>\n")
		b.WriteString("    </CalendarTrigger>\n")
	case TriggerWeekly:
		start, _ := startBoundary(now, trg.endpoint)
		b.WriteString("    <CalendarTrigger>\n")
		b.WriteString("      <StartBoundary>" + start + "</StartBoundary>\n")
		b.WriteString("      <Enabled>true</Enabled>\n")
		b.WriteString("      <ScheduleByWeek>\n        <DaysOfWeek>\n")
		for _, d := range trg.days {
			b.WriteString("          <" + d + " />\n")
		}
		b.WriteString("        </DaysOfWeek>\n        <WeeksInterval>1</WeeksInterval>\n      </ScheduleByWeek>\n")
		b.WriteString("    </CalendarTrigger>\n")
	case TriggerInterval:
		start, _ := startBoundary(now, "00:00")
		b.WriteString("    <TimeTrigger>\n")
		b.WriteString("      <StartBoundary>" + start + "</StartBoundary>\n")
		b.WriteString("      <Enabled>true</Enabled>\n")
		b.WriteString("      <Repetition>\n")
		b.WriteString(fmt.Sprintf("        <Interval>PT%dM</Interval>\n", trg.everyMin))
		b.WriteString("        <StopAtDurationEnd>false</StopAtDurationEnd>\n")
		b.WriteString("      </Repetition>\n")
		b.WriteString("    </TimeTrigger>\n")
	}
	b.WriteString("  </Triggers>\n")
	return b.String()
}

// settingsXML genera el bloque <Settings> con el límite de ejecución (D9:
// MaxDurationMin -> ExecutionTimeLimit) y anti-solapamiento (D2: lock por perfil).
func settingsXML(spec Spec) string {
	var b strings.Builder
	b.WriteString("  <Settings>\n")
	b.WriteString("    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>\n")
	b.WriteString("    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>\n")
	b.WriteString("    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>\n")
	b.WriteString("    <AllowHardTerminate>true</AllowHardTerminate>\n")
	b.WriteString("    <StartWhenAvailable>true</StartWhenAvailable>\n")
	b.WriteString("    <RunOnlyIfNetworkAvailable>false</RunOnlyIfNetworkAvailable>\n")
	b.WriteString("    <IdleSettings>\n      <StopOnIdleEnd>false</StopOnIdleEnd>\n      <RestartOnIdle>false</RestartOnIdle>\n    </IdleSettings>\n")
	b.WriteString("    <AllowStartOnDemand>true</AllowStartOnDemand>\n")
	b.WriteString(fmt.Sprintf("    <Enabled>%t</Enabled>\n", spec.Enabled))
	b.WriteString("    <Hidden>false</Hidden>\n")
	b.WriteString("    <RunOnlyIfIdle>false</RunOnlyIfIdle>\n")
	b.WriteString("    <WakeToRun>false</WakeToRun>\n")
	if limit := durationLimit(spec.MaxDurationMin); limit != "" {
		b.WriteString("    <ExecutionTimeLimit>" + limit + "</ExecutionTimeLimit>\n")
	} else {
		b.WriteString("    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>\n")
	}
	b.WriteString("    <Priority>7</Priority>\n")
	b.WriteString("  </Settings>\n")
	return b.String()
}

func descriptionFor(spec Spec) string {
	if strings.TrimSpace(spec.Description) != "" {
		return spec.Description
	}
	if strings.TrimSpace(spec.ProfileName) != "" {
		return "Backup FEMUCARIBE (perfil " + spec.ProfileName + ")"
	}
	return "Backup FEMUCARIBE"
}

func dirOf(path string) string {
	idx := strings.LastIndexAny(path, `\/`)
	if idx <= 0 {
		return "."
	}
	return path[:idx]
}

func xmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")
	return r.Replace(s)
}

// EncodeUTF16LE convierte el XML a UTF-16LE con BOM, que es lo que Task
// Scheduler espera leer en los archivos /XML.
func EncodeUTF16LE(xml string) []byte {
	codeUnits := utf16.Encode([]rune(xml))
	out := make([]byte, 0, len(codeUnits)*2+2)
	out = append(out, 0xFF, 0xFE) // BOM little-endian
	for _, u := range codeUnits {
		out = append(out, byte(u), byte(u>>8))
	}
	return out
}