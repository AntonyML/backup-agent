package config

import "testing"

// baseConDosPlataformas arma una config con ambas plataformas habilitadas,
// para probar las reglas de kind/plataformas (D10).
func baseConDosPlataformas() Config {
	c := Default()
	c.RemoteServer.Enabled = true
	c.RemoteServer.RemotePath = `\\srv\bkp`
	c.Cloudflare.Enabled = true
	return c
}

// TestEffectiveSchedule_Herencia verifica D1 (perfil sin schedule hereda global).
func TestEffectiveSchedule_Herencia(t *testing.T) {
	c := Default()
	c.Schedule = ScheduleConfig{Enabled: true, Mode: "daily", TimeOfDay: "23:00", TaskName: "global"}

	sinPropio := Profile{Name: "p", Kind: KindLocal}
	if got := c.EffectiveSchedule(sinPropio); got.TaskName != "global" || got.TimeOfDay != "23:00" {
		t.Errorf("perfil sin schedule debe heredar el global: %+v", got)
	}

	conPropio := Profile{Name: "q", Kind: KindLocal, Schedule: &ScheduleConfig{
		Enabled: true, Mode: "interval", IntervalMinutes: 30, TaskName: "propio",
	}}
	got := c.EffectiveSchedule(conPropio)
	if got.TaskName != "propio" || got.Mode != "interval" || got.IntervalMinutes != 30 {
		t.Errorf("perfil con schedule propio debe usarlo: %+v", got)
	}
}

// TestValidateProfiles_Reglas cubre las reglas de kind/plataformas (D10).
func TestValidateProfiles_Reglas(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*Config)
		wantErr bool
	}{
		{"local sin plataformas es válido", func(c *Config) {
			c.Profiles = []Profile{{Name: "solo_local", Kind: KindLocal}}
			c.ActiveProfile = "solo_local"
		}, false},
		{"local con plataformas es inválido", func(c *Config) {
			c.Profiles = []Profile{{Name: "x", Kind: KindLocal, Platforms: []string{PlatformCloudflare}}}
			c.ActiveProfile = "x"
		}, true},
		{"dev exige exactamente 1", func(c *Config) {
			c.Profiles = []Profile{{Name: "dev1", Kind: KindDev, Platforms: []string{PlatformCloudflare}}}
			c.ActiveProfile = "dev1"
		}, false},
		{"dev con 2 es inválido", func(c *Config) {
			c.Profiles = []Profile{{Name: "dev2", Kind: KindDev, Platforms: []string{PlatformCloudflare, PlatformServer}}}
			c.ActiveProfile = "dev2"
		}, true},
		{"hibrido exige exactamente 2", func(c *Config) {
			c.Profiles = []Profile{{Name: "hib", Kind: KindHibrido, Platforms: []string{PlatformCloudflare, PlatformServer}}}
			c.ActiveProfile = "hib"
		}, false},
		{"hibrido con 1 es inválido", func(c *Config) {
			c.Profiles = []Profile{{Name: "hib1", Kind: KindHibrido, Platforms: []string{PlatformCloudflare}}}
			c.ActiveProfile = "hib1"
		}, true},
		{"full exige todas las habilitadas", func(c *Config) {
			c.Profiles = []Profile{{Name: "completo", Kind: KindFull, Platforms: []string{PlatformCloudflare, PlatformServer}}}
			c.ActiveProfile = "completo"
		}, false},
		{"full incompleto es inválido", func(c *Config) {
			c.Profiles = []Profile{{Name: "completo", Kind: KindFull, Platforms: []string{PlatformCloudflare}}}
			c.ActiveProfile = "completo"
		}, true},
		{"nombre duplicado es inválido", func(c *Config) {
			c.Profiles = []Profile{{Name: "dup", Kind: KindLocal}, {Name: "dup", Kind: KindLocal}}
			c.ActiveProfile = "dup"
		}, true},
		{"nombre con guión es inválido", func(c *Config) {
			c.Profiles = []Profile{{Name: "con-guion", Kind: KindLocal}}
			c.ActiveProfile = "con-guion"
		}, true},
		{"plataforma desconocida es inválida", func(c *Config) {
			c.Profiles = []Profile{{Name: "p", Kind: KindDev, Platforms: []string{"s3"}}}
			c.ActiveProfile = "p"
		}, true},
		{"plataforma local explícita es inválida", func(c *Config) {
			c.Profiles = []Profile{{Name: "p", Kind: KindDev, Platforms: []string{PlatformLocal}}}
			c.ActiveProfile = "p"
		}, true},
		{"plataforma duplicada es inválida", func(c *Config) {
			c.Profiles = []Profile{{Name: "p", Kind: KindHibrido, Platforms: []string{PlatformCloudflare, PlatformCloudflare}}}
			c.ActiveProfile = "p"
		}, true},
		{"plataforma deshabilitada globalmente es inválida", func(c *Config) {
			c.Cloudflare.Enabled = false
			c.Profiles = []Profile{{Name: "p", Kind: KindDev, Platforms: []string{PlatformCloudflare}}}
			c.ActiveProfile = "p"
		}, true},
		{"kind desconocido es inválido", func(c *Config) {
			c.Profiles = []Profile{{Name: "p", Kind: "espejo"}}
			c.ActiveProfile = "p"
		}, true},
		{"active_profile inexistente es inválido", func(c *Config) {
			c.Profiles = []Profile{{Name: "p", Kind: KindLocal}}
			c.ActiveProfile = "otro"
		}, true},
		{"active_profile vacío con perfiles es inválido", func(c *Config) {
			c.Profiles = []Profile{{Name: "p", Kind: KindLocal}}
			c.ActiveProfile = ""
		}, true},
		{"schedule de perfil inválido es inválido", func(c *Config) {
			c.Profiles = []Profile{{Name: "p", Kind: KindLocal, Schedule: &ScheduleConfig{Mode: "cada_rato"}}}
			c.ActiveProfile = "p"
		}, true},
		{"schedule de perfil válido se acepta", func(c *Config) {
			c.Profiles = []Profile{{Name: "p", Kind: KindLocal, Schedule: &ScheduleConfig{
				Enabled: true, Mode: "weekly", TimeOfDay: "22:00", Weekdays: []string{"mon"}, TaskName: "T",
			}}}
			c.ActiveProfile = "p"
		}, false},
		{"override keep inválido es inválido", func(c *Config) {
			zero := 0
			c.Profiles = []Profile{{
				Name: "p", Kind: KindDev, Platforms: []string{PlatformCloudflare},
				Overrides: ProfileOverrides{Cloudflare: &PlatformCloudflareOverride{Keep: &zero}},
			}}
			c.ActiveProfile = "p"
		}, true},
		{"overrides válidos son aceptados", func(c *Config) {
			keep, to := 7, 900
			c.Profiles = []Profile{{
				Name: "p", Kind: KindDev, Platforms: []string{PlatformCloudflare},
				Overrides: ProfileOverrides{Cloudflare: &PlatformCloudflareOverride{Keep: &keep, TimeoutSec: &to}},
			}}
			c.ActiveProfile = "p"
		}, false},
		{"config legacy sin perfiles sigue siendo válida", func(c *Config) {
			c.Profiles = nil
			c.ActiveProfile = ""
		}, false},
		{"active_profile sin perfiles es inválido", func(c *Config) {
			c.Profiles = nil
			c.ActiveProfile = "fantasma"
		}, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := baseConDosPlataformas()
			tc.mutate(&cfg)
			err := cfg.Validate()
			if tc.wantErr && err == nil {
				t.Errorf("esperaba error de validación, no hubo")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("no esperaba error, dio: %v", err)
			}
		})
	}
}