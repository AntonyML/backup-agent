package application

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"femucaribe-backup-agent/internal/config"
)

// ProfileInfo describe un perfil para la TUI sin exponer la config completa.
type ProfileInfo struct {
	Name        string
	Kind        string
	Active      bool
	Platforms   []string
	ScheduleText string
	Inherits    bool
	TaskName    string
	// NextRun es la próxima corrida estimada ("" si no se puede estimar).
	NextRun string
}

// ProfileDetail agrega el estado operativo del perfil activo para la TUI.
type ProfileDetail struct {
	ProfileInfo
	LastRun    string
	LastFile   string
	HasPending bool
	Pending    []string
}

// ListProfiles devuelve todos los perfiles con el activo marcado.
func (a *App) ListProfiles() []ProfileInfo {
	infos := make([]ProfileInfo, 0, len(a.cfg.Profiles))
	for _, p := range a.cfg.Profiles {
		infos = append(infos, a.profileInfo(p))
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].Name < infos[j].Name })
	return infos
}

// ActiveProfileDetail devuelve el detalle del perfil con el que opera la App.
func (a *App) ActiveProfileDetail() ProfileDetail {
	p := a.activeProfile()
	d := ProfileDetail{ProfileInfo: a.profileInfo(p)}
	st, err := state_Load(a.statePath)
	if err != nil {
		return d
	}
	pst, ok := st.ProfileIfExists(p.Name)
	if !ok {
		return d
	}
	d.LastRun = pst.LastRunDate
	if pst.LastBackupFile != "" {
		d.LastFile = baseName(pst.LastBackupFile)
	}
	d.Pending = pendingPlatformNames(pst)
	d.HasPending = len(d.Pending) > 0
	return d
}

// UseProfile fija el perfil activo y lo persiste (usado por TUI y CLI).
func (a *App) UseProfile(name string) error {
	if _, ok := a.cfg.ProfileByName(name); !ok {
		return fmt.Errorf("%w: el perfil %q no existe", ErrUnknownProfile, name)
	}
	a.cfg.ActiveProfile = name
	if err := config.Save(a.ConfigPath(), a.cfg); err != nil {
		return err
	}
	return nil
}

// NextRunForProfile estima la próxima corrida de un schedule (hora local).
// Devuelve "" si el schedule está deshabilitado o no se puede estimar.
func NextRunForProfile(sched config.ScheduleConfig, now time.Time) string {
	if !sched.Enabled {
		return ""
	}
	switch strings.ToLower(sched.Mode) {
	case "daily":
		return nextDaily(sched.TimeOfDay, now)
	case "weekly":
		return nextWeekly(sched.TimeOfDay, sched.Weekdays, now)
	case "interval":
		if sched.IntervalMinutes < 5 {
			return ""
		}
		if sched.TimeOfDay != "" {
			return nextDaily(sched.TimeOfDay, now)
		}
		return fmt.Sprintf("cada %d min", sched.IntervalMinutes)
	default:
		return ""
	}
}

// profileInfo arma la ficha de presentación de un perfil.
func (a *App) profileInfo(p config.Profile) ProfileInfo {
	info := ProfileInfo{
		Name:      p.Name,
		Kind:      p.Kind,
		Active:    p.Name == a.profileName,
		Platforms: append([]string{config.PlatformLocal}, p.Platforms...),
		Inherits:  p.Schedule == nil,
		TaskName:  a.cfg.TaskNameForProfile(p.Name),
	}
	sched := a.cfg.EffectiveSchedule(p)
	info.ScheduleText = ScheduleSummary(sched)
	info.NextRun = NextRunForProfile(sched, time.Now())
	return info
}

// ScheduleSummary resume un ScheduleConfig para la UI.
func ScheduleSummary(s config.ScheduleConfig) string {
	if !s.Enabled {
		return "deshabilitado"
	}
	switch strings.ToLower(s.Mode) {
	case "interval":
		return fmt.Sprintf("cada %d min", s.IntervalMinutes)
	case "weekly":
		return fmt.Sprintf("%s a las %s", strings.Join(s.Weekdays, ","), s.TimeOfDay)
	default:
		return "todos los días a las " + s.TimeOfDay
	}
}