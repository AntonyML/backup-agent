package application

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"femucaribe-backup-agent/internal/config"
	"femucaribe-backup-agent/internal/secrets"
	"femucaribe-backup-agent/internal/state"
)

// BackupStatus contiene información consolidada de la última corrida de backup para la UI.
type BackupStatus struct {
	LastRun   time.Time     `json:"last_run"`
	Duration  time.Duration `json:"duration"`
	Result    string        `json:"result"` // "success" | "error" | "never_run" | "pending_sync"
	Filename  string        `json:"filename"`
	SHA256    string        `json:"sha256"`
}

// BackendStatus describe el estado operativo de un backend de almacenamiento.
type BackendStatus struct {
	Name        string `json:"name"`
	Configured  bool   `json:"configured"`
	LastSyncOK  bool   `json:"last_sync_ok"`
	PendingSync bool   `json:"pending_sync"`
	StatusText  string `json:"status_text"` // "OK" | "PENDING" | "ERROR" | "Not configured"
}

// LogEntry representa un registro estructurado simplificado.
type LogEntry struct {
	Time    time.Time `json:"time"`
	Level   string    `json:"level"`
	Message string    `json:"message"`
}

// GetTUIStatus devuelve los DTOs de presentación para la TUI sin exponer tipos de infraestructura.
func (a *App) GetTUIStatus(ctx context.Context) (BackupStatus, []BackendStatus, error) {
	st, err := state.Load(a.statePath)
	if err != nil {
		st = &state.State{}
	}
	pst := a.profileState(st)

	backupStatus := BackupStatus{
		Result: "never_run",
	}

	if pst.LastRunDate != "" {
		backupStatus.Filename = filepath.Base(pst.LastBackupFile)
		backupStatus.SHA256 = pst.SHA256
		if fi, err := os.Stat(pst.LastBackupFile); err == nil {
			backupStatus.LastRun = fi.ModTime()
		} else if parsed, err := time.Parse("2006-01-02", pst.LastRunDate); err == nil {
			backupStatus.LastRun = parsed
		}

		if pst.HasPending() {
			backupStatus.Result = "pending_sync"
		} else {
			backupStatus.Result = "success"
		}
	}

	// Backends
	var backendStatuses []BackendStatus

	// 1. Local
	localOK := pst.LastBackupFile != ""
	if _, err := os.Stat(pst.LastBackupFile); err != nil && pst.LastBackupFile != "" {
		localOK = false
	}
	localText := "OK"
	if !localOK && pst.LastRunDate != "" {
		localText = "ERROR"
	}
	backendStatuses = append(backendStatuses, BackendStatus{
		Name:        "Local",
		Configured:  true,
		LastSyncOK:  localOK,
		PendingSync: false,
		StatusText:  localText,
	})

	// 2. R2
	r2Configured := false
	for _, b := range a.backends {
		if strings.EqualFold(b.Name(), "r2") {
			r2Configured = true
			break
		}
	}
	r2LastSynced := pst.LastSyncedFiles[config.PlatformCloudflare]
	r2StatusText := "Not configured"
	if r2Configured {
		if pst.IsPending(config.PlatformCloudflare) {
			r2StatusText = "PENDING"
		} else {
			r2StatusText = "OK"
		}
	}
	backendStatuses = append(backendStatuses, BackendStatus{
		Name:        "R2",
		Configured:  r2Configured,
		LastSyncOK:  r2Configured && !pst.IsPending(config.PlatformCloudflare) && r2LastSynced != "",
		PendingSync: pst.IsPending(config.PlatformCloudflare),
		StatusText:  r2StatusText,
	})

	// 3. Server
	serverConfigured := false
	for _, b := range a.backends {
		if strings.EqualFold(b.Name(), "server") {
			serverConfigured = true
			break
		}
	}
	serverLastSynced := pst.LastSyncedFiles[config.PlatformServer]
	serverStatusText := "Not configured"
	if serverConfigured {
		if pst.IsPending(config.PlatformServer) {
			serverStatusText = "PENDING"
		} else {
			serverStatusText = "OK"
		}
	}
	backendStatuses = append(backendStatuses, BackendStatus{
		Name:        "Server",
		Configured:  serverConfigured,
		LastSyncOK:  serverConfigured && !pst.IsPending(config.PlatformServer) && serverLastSynced != "",
		PendingSync: pst.IsPending(config.PlatformServer),
		StatusText:  serverStatusText,
	})

	// 4. Supabase
	supabaseConfigured := a.cfg.Supabase.Enabled
	supabaseStatusText := "Disabled"
	if supabaseConfigured {
		if len(st.PendingEvents) > 0 {
			supabaseStatusText = "PENDING"
		} else if a.eventRepo != nil {
			supabaseStatusText = "Connected"
		} else {
			supabaseStatusText = "Not configured"
		}
	}
	backendStatuses = append(backendStatuses, BackendStatus{
		Name:        "Supabase",
		Configured:  supabaseConfigured,
		LastSyncOK:  supabaseConfigured && len(st.PendingEvents) == 0,
		PendingSync: len(st.PendingEvents) > 0,
		StatusText:  supabaseStatusText,
	})

	return backupStatus, backendStatuses, nil
}

// SaveR2Credentials almacena credenciales de R2 cifradas con DPAPI.
func (a *App) SaveR2Credentials(endpoint, bucket, accessKeyID, secretAccessKey string) error {
	datPath := a.secretsPath
	if datPath == "" {
		datPath = filepath.Join(filepath.Dir(a.statePath), "config.dat")
	}
	return secrets.Save(datPath, secrets.Credentials{
		Endpoint:        endpoint,
		Bucket:          bucket,
		AccessKeyID:     accessKeyID,
		SecretAccessKey: secretAccessKey,
	})
}

// GetR2Credentials recupera las credenciales de R2 descifradas (si existen).
func (a *App) GetR2Credentials() (*secrets.Credentials, error) {
	datPath := a.secretsPath
	if datPath == "" {
		datPath = filepath.Join(filepath.Dir(a.statePath), "config.dat")
	}
	return secrets.Load(datPath)
}
