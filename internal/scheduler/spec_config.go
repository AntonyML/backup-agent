package scheduler

import (
	"fmt"
	"strings"

	"femucaribe-backup-agent/internal/config"
)

// SpecForProfile arma la Spec de la tarea de Windows para un perfil (D1/D2):
// resuelve el schedule efectivo (propio o heredado del global), el nombre de
// tarea (explícito o "FEMUCARIBE-Backup-<perfil>") y la acción
// "backup --unattended --profile <perfil>".
func SpecForProfile(cfg config.Config, profileName, exePath string) (Spec, error) {
	profile, ok := cfg.ProfileByName(profileName)
	if !ok {
		return Spec{}, fmt.Errorf("scheduler: el perfil %q no existe", profileName)
	}
	sched := cfg.EffectiveSchedule(profile)
	return Spec{
		TaskName:        cfg.TaskNameForProfile(profileName),
		ExePath:         exePath,
		ProfileName:     profile.Name,
		Enabled:         sched.Enabled,
		Mode:            sched.Mode,
		TimeOfDay:       sched.TimeOfDay,
		Weekdays:        sched.Weekdays,
		IntervalMinutes: sched.IntervalMinutes,
		MaxDurationMin:  sched.MaxDurationMin,
		Description:     strings.TrimSpace("Backup FEMUCARIBE · perfil " + profile.Name),
	}, nil
}