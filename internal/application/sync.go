package application

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"femucaribe-backup-agent/internal/events"
	"femucaribe-backup-agent/internal/lock"
	"femucaribe-backup-agent/internal/state"
	"femucaribe-backup-agent/internal/version"
)

type SyncOptions struct {
	Force bool
}

// Sync procesa las sincronizaciones pendientes hacia los backends remotos configurados.
func (a *App) Sync(ctx context.Context, opts SyncOptions) error {
	lh, err := lock.Acquire(a.lockPath)
	if err != nil {
		if errors.Is(err, lock.ErrLocked) {
			return ErrLocked
		}
		return fmt.Errorf("adquirir lock %s: %w", a.lockPath, err)
	}
	defer lh.Release()

	st, err := state.Load(a.statePath)
	if err != nil {
		return fmt.Errorf("cargar estado: %w", err)
	}

	// Reintentar eventos pendientes hacia Supabase si los hay
	hadPendingEvents := len(st.PendingEvents) > 0
	if hadPendingEvents {
		a.flushPendingEvents(ctx, st)
	}

	hasPending := st.PendingSync.R2 || st.PendingSync.Server
	if (!hasPending && !opts.Force) || st.LastBackupFile == "" {
		if !hadPendingEvents {
			a.logger.Info("no hay sincronizaciones pendientes")
			return ErrNoPendingBackup
		}
		a.logger.Info("eventos pendientes sincronizados, sin backups pendientes")
		return nil
	}

	if _, err := os.Stat(st.LastBackupFile); err != nil {
		a.logger.Warn("el archivo pendiente no existe en disco, descartando pendientes", "archivo", st.LastBackupFile)
		st.SetPendingR2(false)
		st.SetPendingServer(false)
		_ = state.Save(a.statePath, st)
		return fmt.Errorf("archivo pendiente no encontrado en disco: %s", st.LastBackupFile)
	}

	a.logger.Info("sincronizando archivo a backends remotos", "archivo", st.LastBackupFile)
	var hadError bool
	for _, b := range a.backends {
		isR2 := strings.EqualFold(b.Name(), "r2")
		isServer := strings.EqualFold(b.Name(), "server")

		if !opts.Force {
			if (isR2 && !st.PendingSync.R2) || (isServer && !st.PendingSync.Server) {
				continue
			}
		}

		syncCtx, syncCancel := context.WithTimeout(ctx, 10*time.Minute)
		err := b.Upload(syncCtx, st.LastBackupFile)
		syncCancel()
		if err != nil {
			hadError = true
			a.logger.Error("falló sincronización a backend", "backend", b.Name(), "error", err)
			failType := events.TypeR2SyncFailed
			if isServer {
				failType = events.TypeServerSyncFailed
				st.SetPendingServer(true)
			} else if isR2 {
				st.SetPendingR2(true)
			}
			a.recordEvent(ctx, events.Event{
				EventID:      events.GenerateID(),
				Timestamp:    time.Now().UTC(),
				EventType:    failType,
				Status:       events.StatusFailed,
				Backend:      b.Name(),
				FileName:     filepath.Base(st.LastBackupFile),
				ErrorMessage: sanitizeError(err),
				AgentVersion: version.Current,
			})
		} else {
			a.logger.Info("sincronización exitosa", "backend", b.Name())
			completedType := events.TypeR2SyncCompleted
			if isServer {
				completedType = events.TypeServerSyncCompleted
				st.MarkServerSynced(filepath.Base(st.LastBackupFile))
				_ = b.Rotate(ctx, 10)
			} else if isR2 {
				st.MarkR2Synced(filepath.Base(st.LastBackupFile))
				_ = b.Rotate(ctx, 1)
			} else {
				_ = b.Rotate(ctx, 0)
			}
			a.recordEvent(ctx, events.Event{
				EventID:      events.GenerateID(),
				Timestamp:    time.Now().UTC(),
				EventType:    completedType,
				Status:       events.StatusSuccess,
				Backend:      b.Name(),
				FileName:     filepath.Base(st.LastBackupFile),
				AgentVersion: version.Current,
			})
		}
		_ = state.Save(a.statePath, st)
	}

	if hadError {
		return ErrPendingSync
	}

	return nil
}
