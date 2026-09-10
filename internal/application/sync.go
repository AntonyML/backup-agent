package application

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"femucaribe-backup-agent/internal/lock"
	"femucaribe-backup-agent/internal/state"
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

	hasPending := st.PendingSync.R2 || st.PendingSync.Server
	if (!hasPending && !opts.Force) || st.LastBackupFile == "" {
		a.logger.Info("no hay sincronizaciones pendientes")
		return ErrNoPendingBackup
	}

	if _, err := os.Stat(st.LastBackupFile); err != nil {
		a.logger.Warn("el archivo pendiente no existe en disco", "archivo", st.LastBackupFile)
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
			if isR2 {
				st.SetPendingR2(true)
			} else if isServer {
				st.SetPendingServer(true)
			}
		} else {
			a.logger.Info("sincronización exitosa", "backend", b.Name())
			if isR2 {
				st.MarkR2Synced(filepath.Base(st.LastBackupFile))
				_ = b.Rotate(ctx, 1)
			} else if isServer {
				st.MarkServerSynced(filepath.Base(st.LastBackupFile))
				_ = b.Rotate(ctx, 10)
			} else {
				_ = b.Rotate(ctx, 0)
			}
		}
		_ = state.Save(a.statePath, st)
	}

	if hadError {
		return ErrPendingSync
	}

	return nil
}
