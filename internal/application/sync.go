package application

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"femucaribe-backup-agent/internal/config"
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
	pst := a.profileState(st)

	// Reintentar eventos pendientes hacia Supabase si los hay
	hadPendingEvents := len(st.PendingEvents) > 0
	if hadPendingEvents {
		a.flushPendingEvents(ctx, st)
	}

	hasPending := pst.HasPending()
	if (!hasPending && !opts.Force) || pst.LastBackupFile == "" {
		if !hadPendingEvents {
			a.logger.Info("no hay sincronizaciones pendientes")
			return ErrNoPendingBackup
		}
		a.logger.Info("eventos pendientes sincronizados, sin backups pendientes")
		return nil
	}

	if _, err := os.Stat(pst.LastBackupFile); err != nil {
		a.logger.Warn("el archivo pendiente no existe en disco, descartando pendientes", "archivo", pst.LastBackupFile)
		pst.SetPending(config.PlatformCloudflare, false)
		pst.SetPending(config.PlatformServer, false)
		_ = state.Save(a.statePath, st)
		return fmt.Errorf("archivo pendiente no encontrado en disco: %s", pst.LastBackupFile)
	}

	a.logger.Info("sincronizando archivo a backends remotos", "archivo", pst.LastBackupFile, "perfil", a.profileName)
	var hadError bool
	for _, b := range a.backends {
		isR2 := strings.EqualFold(b.Name(), "r2")
		isServer := strings.EqualFold(b.Name(), "server")

		if !opts.Force {
			if (isR2 && !pst.IsPending(config.PlatformCloudflare)) || (isServer && !pst.IsPending(config.PlatformServer)) {
				continue
			}
		}

		syncCtx, syncCancel := context.WithTimeout(ctx, a.backendTimeout(b))
		err := a.uploadWithRetries(syncCtx, b, pst.LastBackupFile)
		syncCancel()
		if err != nil {
			hadError = true
			a.logger.Error("fallÃ³ sincronizaciÃ³n a backend", "backend", b.Name(), "error", err)
			failType := events.TypeR2SyncFailed
			if isServer {
				failType = events.TypeServerSyncFailed
				pst.SetPending(config.PlatformServer, true)
			} else if isR2 {
				pst.SetPending(config.PlatformCloudflare, true)
			}
			a.recordEvent(ctx, events.Event{
				EventID:      events.GenerateID(),
				Timestamp:    time.Now().UTC(),
				EventType:    failType,
				Status:       events.StatusFailed,
				Backend:      b.Name(),
				FileName:     filepath.Base(pst.LastBackupFile),
				ErrorMessage: sanitizeError(err),
				AgentVersion: version.Current,
			})
		} else {
			a.logger.Info("sincronizaciÃ³n exitosa", "backend", b.Name())
			completedType := events.TypeR2SyncCompleted
			if isServer {
				completedType = events.TypeServerSyncCompleted
				pst.MarkSynced(config.PlatformServer, filepath.Base(pst.LastBackupFile))
				_ = b.Rotate(ctx, a.serverKeep())
			} else if isR2 {
				pst.MarkSynced(config.PlatformCloudflare, filepath.Base(pst.LastBackupFile))
				_ = b.Rotate(ctx, a.cloudflareKeep())
			} else {
				_ = b.Rotate(ctx, 0)
			}
			a.recordEvent(ctx, events.Event{
				EventID:      events.GenerateID(),
				Timestamp:    time.Now().UTC(),
				EventType:    completedType,
				Status:       events.StatusSuccess,
				Backend:      b.Name(),
				FileName:     filepath.Base(pst.LastBackupFile),
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

