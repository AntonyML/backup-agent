package application

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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

	if (!st.PendingSync.R2 && !opts.Force) || st.LastBackupFile == "" {
		a.logger.Info("no hay sincronizaciones pendientes")
		return ErrNoPendingBackup
	}

	if _, err := os.Stat(st.LastBackupFile); err != nil {
		a.logger.Warn("el archivo pendiente no existe en disco", "archivo", st.LastBackupFile)
		st.SetPendingR2(false)
		_ = state.Save(a.statePath, st)
		return fmt.Errorf("archivo pendiente no encontrado en disco: %s", st.LastBackupFile)
	}

	a.logger.Info("sincronizando archivo a backends remotos", "archivo", st.LastBackupFile)
	var hadError bool
	for _, b := range a.backends {
		syncCtx, syncCancel := context.WithTimeout(ctx, 10*time.Minute)
		err := b.Upload(syncCtx, st.LastBackupFile)
		syncCancel()
		if err != nil {
			hadError = true
			a.logger.Error("falló sincronización a backend", "backend", b.Name(), "error", err)
		} else {
			a.logger.Info("sincronización exitosa", "backend", b.Name())
			_ = b.Rotate(ctx, 1)
		}
	}

	if hadError {
		st.SetPendingR2(true)
		_ = state.Save(a.statePath, st)
		return ErrPendingSync
	}

	st.MarkR2Synced(filepath.Base(st.LastBackupFile))
	if err := state.Save(a.statePath, st); err != nil {
		return fmt.Errorf("guardar estado: %w", err)
	}

	return nil
}
