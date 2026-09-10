package application

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"femucaribe-backup-agent/internal/hasher"
	"femucaribe-backup-agent/internal/lock"
	"femucaribe-backup-agent/internal/state"
	"femucaribe-backup-agent/internal/storage"
)

type BackupOptions struct {
	Force bool
}

// Backup orquesta el ciclo de vida completo de un backup según las reglas de negocio.
func (a *App) Backup(ctx context.Context, opts BackupOptions) error {
	if err := a.cfg.Validate(); err != nil {
		a.logger.Error("configuración inválida", "error", err)
		return fmt.Errorf("%w: %v", ErrInvalidConfig, err)
	}

	lh, err := lock.Acquire(a.lockPath)
	if err != nil {
		if errors.Is(err, lock.ErrLocked) {
			a.logger.Error("otra instancia está corriendo", "error", err)
			return ErrLocked
		}
		return fmt.Errorf("adquirir lock %s: %w", a.lockPath, err)
	}
	defer lh.Release()

	if err := os.MkdirAll(a.cfg.BackupDir, 0o755); err != nil {
		return fmt.Errorf("crear backup_dir %s: %w", a.cfg.BackupDir, err)
	}

	// Limpieza de .tmp huérfanos al arranque
	cleaned, err := a.removeTmpOrphans(a.cfg.BackupDir)
	if err != nil {
		a.logger.Error("limpieza de .tmp huérfanos", "error", err)
		return fmt.Errorf("limpieza de temporales: %w", err)
	} else if cleaned > 0 {
		a.logger.Info("limpieza de huérfanos completada", "cantidad", cleaned)
	}

	st, err := state.Load(a.statePath)
	if err != nil {
		a.logger.Warn("state.json corrupto, tratando como primera corrida", "error", err)
		st = &state.State{}
	}

	// Sincronización diferida previa: si hay backup pendiente, intentar subirlo antes de hacer el del día
	if (st.PendingSync.R2 || st.PendingSync.Server) && st.LastBackupFile != "" {
		a.syncPendingBackup(ctx, st)
	}

	// Idempotencia diaria
	if st.RanOn(state.Today()) && !opts.Force {
		a.logger.Info("ya existe backup de hoy, no repito",
			"archivo", st.LastBackupFile, "sha256", st.SHA256)
		return ErrAlreadyRanToday
	}

	stamp := time.Now().Format("20060102_1504")
	finalPath := filepath.Join(a.cfg.BackupDir, fmt.Sprintf("%s_%s.bak", a.cfg.Database, stamp))
	tmpPath := finalPath + ".tmp"

	// Conexión y espacio libre
	db, err := a.sqlEngine.Open(a.cfg.Server, a.cfg.LoginTimeoutSec)
	if err != nil {
		a.logger.Error("conexión SQL falló", "error", err)
		return fmt.Errorf("conexión SQL: %w", err)
	}
	defer db.Close()

	sizeCtx, sizeCancel := context.WithTimeout(ctx, 60*time.Second)
	needed, err := a.sqlEngine.DatabaseSizeBytes(sizeCtx, db, a.cfg.Database)
	sizeCancel()
	if err != nil {
		a.logger.Error("estimación de tamaño falló", "error", err)
		return fmt.Errorf("estimación de tamaño: %w", err)
	}
	a.logger.Info("tamaño estimado de BD", "db", a.cfg.Database, "bytes", needed)

	if err := a.sqlEngine.EnsureFreeSpace(a.cfg.BackupDir, needed); err != nil {
		a.logger.Error("espacio insuficiente en disco", "error", err)
		return fmt.Errorf("espacio en disco: %w", err)
	}

	var bakCtx context.Context
	var bakCancel context.CancelFunc
	if a.cfg.BackupTimeoutSec > 0 {
		bakCtx, bakCancel = context.WithTimeout(ctx, time.Duration(a.cfg.BackupTimeoutSec)*time.Second)
	} else {
		bakCtx, bakCancel = context.WithCancel(ctx)
	}
	defer bakCancel()

	a.logger.Info("ejecutando BACKUP DATABASE", "db", a.cfg.Database, "destino", tmpPath)
	if err := a.sqlEngine.BackupDatabase(bakCtx, db, a.cfg.Database, tmpPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("backup database: %w", err)
	}

	if a.failpoint != nil {
		if err := a.failpoint("after_backup_started"); err != nil {
			// Simula corte abrupto/kill dejando .tmp huérfano en disco
			return fmt.Errorf("failpoint after_backup_started: %w", err)
		}
	}

	a.logger.Info("ejecutando RESTORE VERIFYONLY", "archivo", tmpPath)
	if err := a.sqlEngine.VerifyBackup(bakCtx, db, tmpPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("verify backup: %w", err)
	}

	if a.failpoint != nil {
		if err := a.failpoint("after_verify"); err != nil {
			_ = os.Remove(tmpPath)
			return fmt.Errorf("failpoint after_verify: %w", err)
		}
	}

	sum, err := hasher.File(tmpPath)
	if err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("calcular sha256: %w", err)
	}

	if err := atomicRename(tmpPath, finalPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename a destino final: %w", err)
	}

	if a.failpoint != nil {
		if err := a.failpoint("before_state_save"); err != nil {
			return fmt.Errorf("failpoint before_state_save: %w", err)
		}
	}

	st.LastRunDate = state.Today()
	st.LastBackupFile = finalPath
	st.SHA256 = sum
	if err := state.Save(a.statePath, st); err != nil {
		a.logger.Error("no se pudo guardar state.json local", "error", err)
		return fmt.Errorf("guardar state.json: %w", err)
	}


	// Rotación local
	if a.localBackend != nil {
		if err := a.localBackend.Rotate(ctx, a.cfg.Retain); err != nil {
			a.logger.Warn("rotación local con error", "error", err)
		}
	}

	a.logger.Info("backup local completado exitosamente",
		"archivo", finalPath, "sha256", sum)

	// Pipeline de subida a backends remotos
	hadPending := false
	for _, b := range a.backends {
		uploadCtx, uploadCancel := context.WithTimeout(ctx, 10*time.Minute)
		uploadErr := b.Upload(uploadCtx, finalPath)
		uploadCancel()

		isR2 := strings.EqualFold(b.Name(), "r2")
		isServer := strings.EqualFold(b.Name(), "server")

		if uploadErr != nil {
			var retryErr *storage.RetryableError
			if errors.As(uploadErr, &retryErr) {
				a.logger.Warn("falla transitoria en backend remoto; se registra sincronización pendiente",
					"backend", b.Name(), "error", uploadErr)
				if isR2 {
					st.SetPendingR2(true)
				} else if isServer {
					st.SetPendingServer(true)
				}
				_ = state.Save(a.statePath, st)
				hadPending = true
			} else {
				a.logger.Error("falla fatal en backend remoto",
					"backend", b.Name(), "error", uploadErr)
				return fmt.Errorf("backend %s: %w", b.Name(), uploadErr)
			}
		} else {
			a.logger.Info("subida a backend remoto confirmada",
				"backend", b.Name(), "archivo", filepath.Base(finalPath))
			if isR2 {
				st.MarkR2Synced(filepath.Base(finalPath))
				if err := b.Rotate(ctx, 1); err != nil {
					a.logger.Warn("rotación en backend remoto con advertencia",
						"backend", b.Name(), "error", err)
				}
			} else if isServer {
				st.MarkServerSynced(filepath.Base(finalPath))
				if err := b.Rotate(ctx, 10); err != nil {
					a.logger.Warn("rotación en backend remoto con advertencia",
						"backend", b.Name(), "error", err)
				}
			} else {
				if err := b.Rotate(ctx, 0); err != nil {
					a.logger.Warn("rotación en backend remoto con advertencia",
						"backend", b.Name(), "error", err)
				}
			}
			_ = state.Save(a.statePath, st)
		}
	}

	if hadPending {
		return ErrPendingSync
	}

	return nil
}

func (a *App) syncPendingBackup(ctx context.Context, st *state.State) {
	if _, err := os.Stat(st.LastBackupFile); err != nil {
		a.logger.Warn("el archivo pendiente no existe en disco, descartando pendientes",
			"archivo", st.LastBackupFile)
		st.SetPendingR2(false)
		st.SetPendingServer(false)
		_ = state.Save(a.statePath, st)
		return
	}

	a.logger.Info("iniciando sincronización de backup pendiente", "archivo", st.LastBackupFile)
	for _, b := range a.backends {
		isR2 := strings.EqualFold(b.Name(), "r2")
		isServer := strings.EqualFold(b.Name(), "server")

		// Solo intentar sincronizar si este backend tiene pendiente
		if (isR2 && !st.PendingSync.R2) || (isServer && !st.PendingSync.Server) {
			continue
		}

		syncCtx, syncCancel := context.WithTimeout(ctx, 10*time.Minute)
		err := b.Upload(syncCtx, st.LastBackupFile)
		syncCancel()
		if err != nil {
			a.logger.Warn("reintento de sync pendiente falló", "backend", b.Name(), "error", err)
		} else {
			a.logger.Info("sync pendiente exitosa", "backend", b.Name())
			if isR2 {
				st.MarkR2Synced(filepath.Base(st.LastBackupFile))
				_ = b.Rotate(ctx, 1)
			} else if isServer {
				st.MarkServerSynced(filepath.Base(st.LastBackupFile))
				_ = b.Rotate(ctx, 10)
			} else {
				_ = b.Rotate(ctx, 0)
			}
			_ = state.Save(a.statePath, st)
		}
	}
}

func (a *App) removeTmpOrphans(dir string) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, fmt.Errorf("listar %s: %w", dir, err)
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasSuffix(strings.ToLower(e.Name()), ".tmp") {
			continue
		}
		full := filepath.Join(dir, e.Name())
		if err := os.Remove(full); err != nil {
			return n, fmt.Errorf("borrar huérfano %s: %w", full, err)
		}
		n++
	}
	return n, nil
}

func atomicRename(src, dst string) error {
	if _, err := os.Stat(dst); err == nil {
		if err := os.Remove(dst); err != nil {
			return fmt.Errorf("reemplazar %s: %w", dst, err)
		}
	}
	if err := os.Rename(src, dst); err != nil {
		return fmt.Errorf("renombrar %s a %s: %w", src, dst, err)
	}
	return nil
}
