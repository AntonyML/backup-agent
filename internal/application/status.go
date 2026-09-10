package application

import (
	"context"
	"fmt"
	"os"

	"femucaribe-backup-agent/internal/state"
)

type StatusReport struct {
	LastRunDate          string `json:"last_run_date"`
	LastBackupFile       string `json:"last_backup_file"`
	SHA256               string `json:"sha256"`
	PendingSyncR2        bool   `json:"pending_sync_r2"`
	R2LastSyncedFile     string `json:"r2_last_synced_file"`
	PendingSyncServer    bool   `json:"pending_sync_server"`
	ServerLastSyncedFile string `json:"server_last_synced_file"`
	LockActive           bool   `json:"lock_active"`
	Database             string `json:"database"`
	Server               string `json:"server"`
	BackupDir            string `json:"backup_dir"`
	Retain               int    `json:"retain"`
}

// Status consulta el estado operativo y persistente del agente.
func (a *App) Status(ctx context.Context) (*StatusReport, error) {
	st, err := state.Load(a.statePath)
	if err != nil {
		return nil, fmt.Errorf("cargar estado: %w", err)
	}

	lockActive := false
	if _, err := os.Stat(a.lockPath); err == nil {
		lockActive = true
	}

	return &StatusReport{
		LastRunDate:          st.LastRunDate,
		LastBackupFile:       st.LastBackupFile,
		SHA256:               st.SHA256,
		PendingSyncR2:        st.PendingSync.R2,
		R2LastSyncedFile:     st.R2LastSyncedFile,
		PendingSyncServer:    st.PendingSync.Server,
		ServerLastSyncedFile: st.ServerLastSyncedFile,
		LockActive:           lockActive,
		Database:             a.cfg.Database,
		Server:               a.cfg.Server,
		BackupDir:            a.cfg.BackupDir,
		Retain:               a.cfg.Retain,
	}, nil
}
