package application

import (
	"path/filepath"
	"sort"
	"time"

	"femucaribe-backup-agent/internal/state"
)

// state_Load y baseName existen como indirecciones para que profiles.go no
// acumule imports de infraestructura.
func state_Load(path string) (*state.State, error) { return state.Load(path) }

func baseName(path string) string { return filepath.Base(path) }

// pendingPlatformNames devuelve las plataformas con subida pendiente, ordenadas.
func pendingPlatformNames(p *state.ProfileState) []string {
	var out []string
	for plat, pending := range p.PendingSync {
		if pending {
			out = append(out, plat)
		}
	}
	sort.Strings(out)
	return out
}

// weekdayIndex mapea mon..sun a time.Weekday.
func weekdayIndex(d string) (time.Weekday, bool) {
	switch d {
	case "sun":
		return time.Sunday, true
	case "mon":
		return time.Monday, true
	case "tue":
		return time.Tuesday, true
	case "wed":
		return time.Wednesday, true
	case "thu":
		return time.Thursday, true
	case "fri":
		return time.Friday, true
	case "sat":
		return time.Saturday, true
	}
	return time.Sunday, false
}

// nextDaily devuelve la próxima ocurrencia de HH:MM (formato "YYYY-MM-DD HH:MM")
// en hora local.
func nextDaily(hhmm string, now time.Time) string {
	hm, err := time.Parse("15:04", hhmm)
	if err != nil {
		return ""
	}
	start := time.Date(now.Year(), now.Month(), now.Day(), hm.Hour(), hm.Minute(), 0, 0, time.Local)
	if !start.After(now) {
		start = start.AddDate(0, 0, 1)
	}
	return start.Format("2006-01-02 15:04")
}

// nextWeekly devuelve la próxima ocurrencia dentro de los días dados.
func nextWeekly(hhmm string, days []string, now time.Time) string {
	hm, err := time.Parse("15:04", hhmm)
	if err != nil || len(days) == 0 {
		return ""
	}
	best := ""
	for _, d := range days {
		wd, ok := weekdayIndex(d)
		if !ok {
			continue
		}
		candidate := time.Date(now.Year(), now.Month(), now.Day(), hm.Hour(), hm.Minute(), 0, 0, time.Local)
		delta := (int(wd) - int(candidate.Weekday()) + 7) % 7
		candidate = candidate.AddDate(0, 0, delta)
		if !candidate.After(now) {
			candidate = candidate.AddDate(0, 0, 7)
		}
		text := candidate.Format("2006-01-02 15:04")
		if best == "" || text < best {
			best = text
		}
	}
	return best
}