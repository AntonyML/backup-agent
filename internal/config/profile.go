package config

import (
	"fmt"
	"sort"
	"strings"
)

// Nombres canónicos de plataformas de destino. "local" es implícito en todos
// los perfiles y por lo tanto no puede listarse en Profile.Platforms.
const (
	PlatformCloudflare = "cloudflare"
	PlatformServer     = "remote_server"
	PlatformSupabase   = "supabase"
	PlatformLocal      = "local" // implícito: no se lista en perfiles
	InitialProfileName = "full"
	KindLocal          = "local"
	KindDev            = "dev"
	KindHibrido        = "hibrido"
	KindFull           = "full"
)

// ValidKinds lista los tipos de perfil aceptados.
var ValidKinds = []string{KindLocal, KindDev, KindHibrido, KindFull}

// ValidPlatforms lista las plataformas que un perfil puede referenciar.
// Diseñado para crecer con plataformas futuras (s3, azure) sin cambiar el esquema.
var ValidPlatforms = []string{PlatformCloudflare, PlatformServer, PlatformSupabase}

// PlatformCloudflareOverride agrupa los overrides de plataforma para Cloudflare R2.
// Punteros: nil = heredar el valor global de plataforma sin override.
type PlatformCloudflareOverride struct {
	Keep          *int `json:"keep,omitempty"`
	TimeoutSec    *int `json:"timeout_sec,omitempty"`
	UploadRetries *int `json:"upload_retries,omitempty"`
}

// PlatformServerOverride agrupa los overrides de plataforma para el servidor UNC.
type PlatformServerOverride struct {
	Keep       *int `json:"keep,omitempty"`
	TimeoutSec *int `json:"timeout_sec,omitempty"`
}

// PlatformSupabaseOverride agrupa los overrides de plataforma para Supabase Storage.
type PlatformSupabaseOverride struct {
	Keep       *int `json:"keep,omitempty"`
	TimeoutSec *int `json:"timeout_sec,omitempty"`
}

// ProfileOverrides contiene los overrides de retención/timeout/reintentos por
// plataforma del perfil (D5: la retención por perfil se expresa como override
// sobre los valores globales de la plataforma).
type ProfileOverrides struct {
	Cloudflare   *PlatformCloudflareOverride `json:"cloudflare,omitempty"`
	RemoteServer *PlatformServerOverride     `json:"remote_server,omitempty"`
	Supabase     *PlatformSupabaseOverride   `json:"supabase,omitempty"`
}

// Profile es un perfil de backup de primera clase (D10):
// el destino LOCAL siempre está implícito; Platforms lista solo las
// plataformas remotas del perfil. Schedule vacío (nil) hereda el global (D1).
type Profile struct {
	Name      string           `json:"name"`
	Kind      string           `json:"kind"`
	Platforms []string         `json:"platforms,omitempty"`
	Schedule  *ScheduleConfig  `json:"schedule,omitempty"`
	Overrides ProfileOverrides `json:"overrides,omitempty"`
}

// EnabledPlatforms devuelve las plataformas remotas habilitadas globalmente,
// ordenadas para comparaciones estables.
func (c Config) EnabledPlatforms() []string {
	var out []string
	if c.Cloudflare.Enabled {
		out = append(out, PlatformCloudflare)
	}
	if c.RemoteServer.Enabled {
		out = append(out, PlatformServer)
	}
	if c.Supabase.Storage.Enabled {
		out = append(out, PlatformSupabase)
	}
	sort.Strings(out)
	return out
}

// ProfileByName busca un perfil por nombre (case-sensitive).
func (c Config) ProfileByName(name string) (Profile, bool) {
	for _, p := range c.Profiles {
		if p.Name == name {
			return p, true
		}
	}
	return Profile{}, false
}

// ActiveProfileConfig devuelve el perfil activo.
func (c Config) ActiveProfileConfig() (Profile, bool) {
	return c.ProfileByName(c.ActiveProfile)
}

// TaskNamePrefix es el prefijo por convención de las tareas de Windows (D2).
const TaskNamePrefix = "FEMUCARIBE-Backup-"

// TaskNameForProfile resuelve el nombre de la tarea de Windows de un perfil:
// el schedule del perfil (o el global heredado) puede fijarlo explícitamente
// en TaskName; si está vacío se usa la convención "FEMUCARIBE-Backup-<perfil>" (D2).
func (c Config) TaskNameForProfile(name string) string {
	if p, ok := c.ProfileByName(name); ok {
		if s := c.EffectiveSchedule(p); strings.TrimSpace(s.TaskName) != "" {
			return strings.TrimSpace(s.TaskName)
		}
	}
	return TaskNamePrefix + name
}

// EffectiveSchedule resuelve el schedule de un perfil: si el perfil no define
// uno propio (nil), hereda el schedule global (D1).
func (c Config) EffectiveSchedule(p Profile) ScheduleConfig {
	if p.Schedule != nil {
		return *p.Schedule
	}
	return c.Schedule
}

// EnsureMigrated convierte una Config en formato legacy (sin perfiles) al
// formato nuevo, creando el perfil inicial "full" con todas las plataformas
// habilitadas y el schedule global (D11). Es idempotente: sobre una config ya
// migrada no cambia nada. Devuelve la config (posiblemente la misma) y si
// hubo cambios.
func (c Config) EnsureMigrated() (Config, bool) {
	changed := false
	if len(c.Profiles) == 0 {
		c.Profiles = []Profile{{
			Name:      InitialProfileName,
			Kind:      KindFull,
			Platforms: c.EnabledPlatforms(),
		}}
		c.ActiveProfile = InitialProfileName
		changed = true
	}
	if c.ActiveProfile == "" {
		c.ActiveProfile = c.Profiles[0].Name
		changed = true
	}
	// Un perfil "full" es por definición "todas las plataformas habilitadas"
	// (D10/D11): re-sincronizamos sus plataformas. Esto también cubre el caso
	// de una config legacy que se mergea sobre defaults con el perfil full vacío.
	want := c.EnabledPlatforms()
	for i := range c.Profiles {
		if c.Profiles[i].Kind != KindFull {
			continue
		}
		if !sameStringSet(c.Profiles[i].Platforms, want) {
			c.Profiles[i].Platforms = append([]string(nil), want...)
			changed = true
		}
	}
	return c, changed
}

func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	ma := map[string]bool{}
	for _, v := range a {
		ma[v] = true
	}
	for _, v := range b {
		if !ma[v] {
			return false
		}
	}
	return true
}

// ValidateProfiles valida la sección de perfiles: unicidad de nombres, kind
// coherente con las plataformas, perfil activo existente, schedule por perfil
// y overrides con rangos válidos. Una config legacy (sin perfiles) es válida:
// la migración es responsabilidad de EnsureMigrated.
func (c Config) ValidateProfiles() error {
	if len(c.Profiles) == 0 {
		if c.ActiveProfile != "" {
			return fmt.Errorf("config: active_profile %q definido sin perfiles", c.ActiveProfile)
		}
		return nil
	}

	enabled := map[string]bool{}
	for _, p := range c.EnabledPlatforms() {
		enabled[p] = true
	}

	seen := map[string]bool{}
	for _, p := range c.Profiles {
		if !dbNameRe.MatchString(p.Name) {
			return fmt.Errorf("config: perfil %q inválido (solo letras, dígitos y _)", p.Name)
		}
		if seen[p.Name] {
			return fmt.Errorf("config: nombre de perfil duplicado %q", p.Name)
		}
		seen[p.Name] = true
		if !contains(ValidKinds, p.Kind) {
			return fmt.Errorf("config: perfil %q tiene kind %q inválido (local, dev, hibrido o full)", p.Name, p.Kind)
		}
		platSeen := map[string]bool{}
		for _, plat := range p.Platforms {
			if !contains(ValidPlatforms, plat) {
				return fmt.Errorf("config: perfil %q referencia plataforma desconocida %q", p.Name, plat)
			}
			if plat == PlatformLocal {
				return fmt.Errorf("config: perfil %q no debe listar %q: el destino local es implícito", p.Name, PlatformLocal)
			}
			if platSeen[plat] {
				return fmt.Errorf("config: perfil %q lista la plataforma %q duplicada", p.Name, plat)
			}
			platSeen[plat] = true
			if !enabled[plat] {
				return fmt.Errorf("config: perfil %q referencia la plataforma %q que no está habilitada globalmente", p.Name, plat)
			}
		}
		switch p.Kind {
		case KindLocal:
			if len(p.Platforms) != 0 {
				return fmt.Errorf("config: perfil %q de tipo local no puede tener plataformas remotas", p.Name)
			}
		case KindDev:
			if len(p.Platforms) != 1 {
				return fmt.Errorf("config: perfil %q de tipo dev debe tener exactamente 1 plataforma, tiene %d", p.Name, len(p.Platforms))
			}
		case KindHibrido:
			if len(p.Platforms) != 2 {
				return fmt.Errorf("config: perfil %q de tipo hibrido debe tener exactamente 2 plataformas, tiene %d", p.Name, len(p.Platforms))
			}
		case KindFull:
			want := c.EnabledPlatforms()
			if len(p.Platforms) != len(want) {
				return fmt.Errorf("config: perfil %q de tipo full debe incluir todas las plataformas habilitadas (%v), tiene %v",
					p.Name, strings.Join(want, ","), strings.Join(p.Platforms, ","))
			}
			for _, w := range want {
				if !platSeen[w] {
					return fmt.Errorf("config: perfil %q de tipo full debe incluir la plataforma habilitada %q", p.Name, w)
				}
			}
		}
		if p.Schedule != nil {
			if err := p.Schedule.Validate(); err != nil {
				return fmt.Errorf("config: schedule del perfil %q: %w", p.Name, err)
			}
		}
		if err := validateOverrides(p.Overrides); err != nil {
			return fmt.Errorf("config: overrides del perfil %q: %w", p.Name, err)
		}
	}

	if c.ActiveProfile == "" {
		return fmt.Errorf("config: active_profile es obligatorio cuando existen perfiles")
	}
	if _, ok := c.ProfileByName(c.ActiveProfile); !ok {
		return fmt.Errorf("config: active_profile %q no coincide con ningún perfil", c.ActiveProfile)
	}
	return nil
}

func validateOverrides(o ProfileOverrides) error {
	if o.Cloudflare != nil {
		if o.Cloudflare.Keep != nil && *o.Cloudflare.Keep < 1 {
			return fmt.Errorf("cloudflare.keep debe ser >= 1, recibí %d", *o.Cloudflare.Keep)
		}
		if o.Cloudflare.TimeoutSec != nil && *o.Cloudflare.TimeoutSec < 0 {
			return fmt.Errorf("cloudflare.timeout_sec no puede ser negativo")
		}
		if o.Cloudflare.UploadRetries != nil && *o.Cloudflare.UploadRetries < 0 {
			return fmt.Errorf("cloudflare.upload_retries no puede ser negativo")
		}
	}
	if o.RemoteServer != nil {
		if o.RemoteServer.Keep != nil && *o.RemoteServer.Keep < 1 {
			return fmt.Errorf("remote_server.keep debe ser >= 1, recibí %d", *o.RemoteServer.Keep)
		}
		if o.RemoteServer.TimeoutSec != nil && *o.RemoteServer.TimeoutSec < 0 {
			return fmt.Errorf("remote_server.timeout_sec no puede ser negativo")
		}
	}
	if o.Supabase != nil {
		if o.Supabase.Keep != nil && *o.Supabase.Keep < 1 {
			return fmt.Errorf("supabase.keep debe ser >= 1, recibí %d", *o.Supabase.Keep)
		}
		if o.Supabase.TimeoutSec != nil && *o.Supabase.TimeoutSec < 0 {
			return fmt.Errorf("supabase.timeout_sec no puede ser negativo")
		}
	}
	return nil
}
