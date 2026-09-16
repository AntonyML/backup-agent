package application

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// PlatformCheck describe el resultado de una verificación de salud.
type PlatformCheck struct {
	Name   string
	OK     bool
	Detail string
}

// CheckPlatforms verifica conectividad de SQL Server, escritura local y
// disponibilidad de cada plataforma del perfil activo. Nunca hace backup ni
// modifica estado: es de solo diagnóstico (comando doctor).
func (a *App) CheckPlatforms(ctx context.Context) []PlatformCheck {
	var checks []PlatformCheck

	// 1. SQL Server
	db, err := a.sqlEngine.Open(a.sqlConnectOptions())
	if err != nil {
		checks = append(checks, PlatformCheck{Name: "SQL Server", OK: false, Detail: err.Error()})
	} else {
		_ = db.Close()
		authDesc := "Windows Auth"
		if strings.ToLower(strings.TrimSpace(a.cfg.AuthMode)) == "sql" || strings.TrimSpace(a.cfg.User) != "" {
			authDesc = fmt.Sprintf("SQL Auth (%s)", a.cfg.User)
		}
		checks = append(checks, PlatformCheck{Name: "SQL Server", OK: true, Detail: fmt.Sprintf("conexión OK a %s (%s, BD: %s)", a.cfg.Server, authDesc, a.cfg.Database)})
	}

	// 2. Carpeta local de backups
	checks = append(checks, checkLocalDir(a.cfg.BackupDir))

	// 3. Plataformas remotas del perfil
	checks = append(checks, a.checkRemotePlatforms(ctx)...)

	// 4. Supabase (observabilidad)
	checks = append(checks, a.checkSupabase())

	return checks
}

func checkLocalDir(dir string) PlatformCheck {
	if strings.TrimSpace(dir) == "" {
		return PlatformCheck{Name: "Carpeta local", OK: false, Detail: "backup_dir vacío"}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return PlatformCheck{Name: "Carpeta local", OK: false, Detail: err.Error()}
	}
	tmp := filepath.Join(dir, ".write-test.tmp")
	if err := os.WriteFile(tmp, []byte("ok"), 0o600); err != nil {
		return PlatformCheck{Name: "Carpeta local", OK: false, Detail: fmt.Sprintf("sin permiso de escritura en %s: %v", dir, err)}
	}
	_ = os.Remove(tmp)
	return PlatformCheck{Name: "Carpeta local", OK: true, Detail: fmt.Sprintf("escritura OK en %s", dir)}
}

func (a *App) checkRemotePlatforms(ctx context.Context) []PlatformCheck {
	var checks []PlatformCheck

	hasR2 := false
	for _, b := range a.backends {
		if !strings.EqualFold(b.Name(), "r2") {
			continue
		}
		hasR2 = true
		cctx, cancel := context.WithTimeout(ctx, time.Duration(a.cloudflareTimeoutSec())*time.Second)
		_, err := b.LatestRemote(cctx)
		cancel()
		if err != nil {
			checks = append(checks, PlatformCheck{Name: "Cloudflare R2", OK: false, Detail: err.Error()})
		} else {
			checks = append(checks, PlatformCheck{Name: "Cloudflare R2", OK: true, Detail: "bucket accesible"})
		}
	}
	if !hasR2 {
		detail := "no habilitada"
		if a.cfg.Cloudflare.Enabled {
			detail = "habilitada sin credenciales válidas en config.dat"
		}
		checks = append(checks, PlatformCheck{Name: "Cloudflare R2", OK: false, Detail: detail})
	}

	if a.cfg.RemoteServer.Enabled {
		if _, err := os.Stat(a.cfg.RemoteServer.RemotePath); err != nil {
			checks = append(checks, PlatformCheck{Name: "Servidor UNC", OK: false, Detail: err.Error()})
		} else {
			checks = append(checks, PlatformCheck{Name: "Servidor UNC", OK: true, Detail: a.cfg.RemoteServer.RemotePath + " accesible"})
		}
	} else {
		checks = append(checks, PlatformCheck{Name: "Servidor UNC", OK: false, Detail: "no habilitada"})
	}

	hasSupabaseStorage := false
	for _, b := range a.backends {
		if !strings.EqualFold(b.Name(), "supabase") {
			continue
		}
		hasSupabaseStorage = true
		cctx, cancel := context.WithTimeout(ctx, time.Duration(a.supabaseStorageTimeoutSec())*time.Second)
		_, err := b.LatestRemote(cctx)
		cancel()
		if err != nil {
			checks = append(checks, PlatformCheck{Name: "Supabase Storage", OK: false, Detail: err.Error()})
		} else {
			checks = append(checks, PlatformCheck{Name: "Supabase Storage", OK: true, Detail: "bucket accesible"})
		}
	}
	if !hasSupabaseStorage {
		detail := "no habilitada"
		if a.cfg.Supabase.Storage.Enabled {
			detail = "habilitada pero cliente no inicializado (verificar supabase.enabled y api_key)"
		}
		checks = append(checks, PlatformCheck{Name: "Supabase Storage", OK: false, Detail: detail})
	}

	return checks
}

func (a *App) checkSupabase() PlatformCheck {
	if !a.cfg.Supabase.Enabled {
		return PlatformCheck{Name: "Supabase", OK: false, Detail: "no habilitada"}
	}
	if a.eventRepo == nil {
		return PlatformCheck{Name: "Supabase", OK: false, Detail: "API key no configurada (TUI / SUPABASE_KEY)"}
	}
	return PlatformCheck{Name: "Supabase", OK: true, Detail: "cliente listo"}
}