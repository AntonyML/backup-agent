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
	"femucaribe-backup-agent/internal/hasher"
	"femucaribe-backup-agent/internal/lock"
	"femucaribe-backup-agent/internal/retry"
	"femucaribe-backup-agent/internal/state"
	"femucaribe-backup-agent/internal/storage"
	"femucaribe-backup-agent/internal/version"
)

type BackupOptions struct {
	Force bool
}

func sanitizeError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if len(msg) > 500 {
		msg = msg[:500] + "..."
	}
	return msg
}

// Backup orquesta el ciclo de vida completo de un backup segÃºn las reglas de negocio.
func (a *App) Backup(ctx context.Context, opts BackupOptions) (returnErr error) {
	startTime := time.Now()
	a.recordEvent(ctx, events.NewEvent(events.TypeAgentStarted, events.StatusRunning))
	defer func() {
		status := events.StatusSuccess
		errMsg := ""
		if returnErr != nil && !errors.Is(returnErr, ErrAlreadyRanToday) {
			status = events.StatusFailed
			errMsg = sanitizeError(returnErr)
		}
		a.recordEvent(context.Background(), events.Event{
			EventID:      events.GenerateID(),
			Timestamp:    time.Now().UTC(),
			EventType:    events.TypeAgentFinished,
			Status:       status,
			DurationMs:   time.Since(startTime).Milliseconds(),
			ErrorMessage: errMsg,
			AgentVersion: version.Current,
		})
	}()

	if err := a.cfg.Validate(); err != nil {
		a.logger.Error("configuraciÃ³n invÃ¡lida", "error", err)
		return fmt.Errorf("%w: %v", ErrInvalidConfig, err)
	}

	lh, err := lock.Acquire(a.lockPath)
	if err != nil {
		if errors.Is(err, lock.ErrLocked) {
			a.logger.Error("otra instancia estÃ¡ corriendo", "error", err)
			return ErrLocked
		}
		return fmt.Errorf("adquirir lock %s: %w", a.lockPath, err)
	}
	defer lh.Release()

	if err := os.MkdirAll(a.cfg.BackupDir, 0o755); err != nil {
		return fmt.Errorf("crear backup_dir %s: %w", a.cfg.BackupDir, err)
	}

	// Limpieza de .tmp huÃ©rfanos al arranque
	cleaned, err := a.removeTmpOrphans(a.cfg.BackupDir)
	if err != nil {
		a.logger.Error("limpieza de .tmp huÃ©rfanos", "error", err)
		return fmt.Errorf("limpieza de temporales: %w", err)
	} else if cleaned > 0 {
		a.logger.Info("limpieza de huÃ©rfanos completada", "cantidad", cleaned)
	}

	st, err := state.Load(a.statePath)
	if err != nil {
		a.logger.Warn("state.json corrupto, tratando como primera corrida", "error", err)
		st = &state.State{}
	}

	// SincronizaciÃ³n diferida previa de eventos pendientes
	if len(st.PendingEvents) > 0 {
		a.flushPendingEvents(ctx, st)
	}

	// SincronizaciÃ³n diferida previa: si hay backup pendiente, intentar subirlo antes de hacer el del dÃ­a
	pst := a.profileState(st)
	if pst.HasPending() && pst.LastBackupFile != "" {
		a.syncPendingBackup(ctx, st, pst)
	}

	// Idempotencia diaria POR PERFIL (D3): last_run_date vive en la secciÃ³n del perfil
	if pst.RanOn(state.Today()) && !opts.Force {
		a.logger.Info("ya existe backup de hoy para este perfil, no repito",
			"perfil", a.profileName, "archivo", pst.LastBackupFile, "sha256", pst.SHA256)
		return ErrAlreadyRanToday
	}

	stamp := time.Now().Format("20060102_1504")
	finalPath := filepath.Join(a.cfg.BackupDir, fmt.Sprintf("%s_%s.bak", a.cfg.Database, stamp))
	tmpPath := finalPath + ".tmp"

	// ConexiÃ³n y espacio libre
	db, err := a.sqlEngine.Open(a.cfg.Server, a.cfg.LoginTimeoutSec)
	if err != nil {
		a.logger.Error("conexiÃ³n SQL fallÃ³", "error", err)
		return fmt.Errorf("conexiÃ³n SQL: %w", err)
	}
	defer db.Close()

	sizeCtx, sizeCancel := context.WithTimeout(ctx, 60*time.Second)
	needed, err := a.sqlEngine.DatabaseSizeBytes(sizeCtx, db, a.cfg.Database)
	sizeCancel()
	if err != nil {
		a.logger.Error("estimaciÃ³n de tamaÃ±o fallÃ³", "error", err)
		return fmt.Errorf("estimaciÃ³n de tamaÃ±o: %w", err)
	}
	a.logger.Info("tamaÃ±o estimado de BD", "db", a.cfg.Database, "bytes", needed)

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
	a.recordEvent(ctx, events.Event{
		EventID:      events.GenerateID(),
		Timestamp:    time.Now().UTC(),
		EventType:    events.TypeBackupStarted,
		Status:       events.StatusRunning,
		DatabaseName: a.cfg.Database,
		AgentVersion: version.Current,
	})

	if err := a.sqlEngine.BackupDatabase(bakCtx, db, a.cfg.Database, tmpPath); err != nil {
		_ = os.Remove(tmpPath)
		a.recordEvent(ctx, events.Event{
			EventID:      events.GenerateID(),
			Timestamp:    time.Now().UTC(),
			EventType:    events.TypeBackupFailed,
			Status:       events.StatusFailed,
			DatabaseName: a.cfg.Database,
			ErrorMessage: sanitizeError(err),
			AgentVersion: version.Current,
		})
		return fmt.Errorf("backup database: %w", err)
	}

	if a.failpoint != nil {
		if err := a.failpoint("after_backup_started"); err != nil {
			// Simula corte abrupto/kill dejando .tmp huÃ©rfano en disco
			return fmt.Errorf("failpoint after_backup_started: %w", err)
		}
	}

	a.logger.Info("ejecutando RESTORE VERIFYONLY", "archivo", tmpPath)
	if err := a.sqlEngine.VerifyBackup(bakCtx, db, tmpPath); err != nil {
		_ = os.Remove(tmpPath)
		a.recordEvent(ctx, events.Event{
			EventID:      events.GenerateID(),
			Timestamp:    time.Now().UTC(),
			EventType:    events.TypeBackupFailed,
			Status:       events.StatusFailed,
			DatabaseName: a.cfg.Database,
			ErrorMessage: sanitizeError(err),
			AgentVersion: version.Current,
		})
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

	pst.LastRunDate = state.Today()
	pst.LastBackupFile = finalPath
	pst.SHA256 = sum
	if err := state.Save(a.statePath, st); err != nil {
		a.logger.Error("no se pudo guardar state.json local", "error", err)
		return fmt.Errorf("guardar state.json: %w", err)
	}

	fi, _ := os.Stat(finalPath)
	var backupSize int64
	if fi != nil {
		backupSize = fi.Size()
	}

	a.recordEvent(ctx, events.Event{
		EventID:      events.GenerateID(),
		Timestamp:    time.Now().UTC(),
		EventType:    events.TypeLocalBackupCompleted,
		Status:       events.StatusSuccess,
		Backend:      "local",
		DatabaseName: a.cfg.Database,
		FileName:     filepath.Base(finalPath),
		SizeBytes:    backupSize,
		DurationMs:   time.Since(startTime).Milliseconds(),
		AgentVersion: version.Current,
	})

	// RotaciÃ³n local
	if a.localBackend != nil {
		if err := a.localBackend.Rotate(ctx, a.cfg.Retain); err != nil {
			a.logger.Warn("rotaciÃ³n local con error", "error", err)
			a.recordEvent(ctx, events.Event{
				EventID:      events.GenerateID(),
				Timestamp:    time.Now().UTC(),
				EventType:    events.TypeLocalRotationFailed,
				Status:       events.StatusFailed,
				Backend:      "local",
				ErrorMessage: sanitizeError(err),
				AgentVersion: version.Current,
			})
		} else {
			a.recordEvent(ctx, events.Event{
				EventID:      events.GenerateID(),
				Timestamp:    time.Now().UTC(),
				EventType:    events.TypeLocalRotationCompleted,
				Status:       events.StatusSuccess,
				Backend:      "local",
				AgentVersion: version.Current,
			})
		}
	}

	a.logger.Info("backup local completado exitosamente",
		"archivo", finalPath, "sha256", sum)

	// Pipeline de subida a backends remotos. SyncAfterBackup (D9) decide si el
	// backup dispara la subida tras el Ã©xito; default true = conducta actual.
	if !a.syncAfterBackup() { // schedule efectivo del perfil (propio o heredado)
		a.logger.Info("sync_after_backup desactivado: el backup queda solo local", "perfil", a.profileName)
		return nil
	}
	hadPending := false
	for _, b := range a.backends {
		uploadCtx, uploadCancel := context.WithTimeout(ctx, a.backendTimeout(b))
		uploadErr := a.uploadWithRetries(uploadCtx, b, finalPath)
		uploadCancel()

		isR2 := strings.EqualFold(b.Name(), "r2")
		isServer := strings.EqualFold(b.Name(), "server")

		if uploadErr != nil {
			var retryErr *storage.RetryableError
			if errors.As(uploadErr, &retryErr) {
				a.logger.Warn("falla transitoria en backend remoto; se registra sincronizaciÃ³n pendiente",
					"backend", b.Name(), "error", uploadErr)
				if isR2 {
					pst.SetPending(config.PlatformCloudflare, true)
					a.recordEvent(ctx, events.Event{
						EventID:      events.GenerateID(),
						Timestamp:    time.Now().UTC(),
						EventType:    events.TypeR2SyncFailed,
						Status:       events.StatusFailed,
						Backend:      "r2",
						FileName:     filepath.Base(finalPath),
						ErrorMessage: sanitizeError(uploadErr),
						AgentVersion: version.Current,
					})
				} else if isServer {
					pst.SetPending(config.PlatformServer, true)
					a.recordEvent(ctx, events.Event{
						EventID:      events.GenerateID(),
						Timestamp:    time.Now().UTC(),
						EventType:    events.TypeServerSyncFailed,
						Status:       events.StatusFailed,
						Backend:      "server",
						FileName:     filepath.Base(finalPath),
						ErrorMessage: sanitizeError(uploadErr),
						AgentVersion: version.Current,
					})
				}
				_ = state.Save(a.statePath, st)
				hadPending = true
			} else {
				a.logger.Error("falla fatal en backend remoto",
					"backend", b.Name(), "error", uploadErr)
				failType := events.TypeR2SyncFailed
				if isServer {
					failType = events.TypeServerSyncFailed
				}
				a.recordEvent(ctx, events.Event{
					EventID:      events.GenerateID(),
					Timestamp:    time.Now().UTC(),
					EventType:    failType,
					Status:       events.StatusFailed,
					Backend:      b.Name(),
					FileName:     filepath.Base(finalPath),
					ErrorMessage: sanitizeError(uploadErr),
					AgentVersion: version.Current,
				})
				return fmt.Errorf("backend %s: %w", b.Name(), uploadErr)
			}
		} else {
			a.logger.Info("subida a backend remoto confirmada",
				"backend", b.Name(), "archivo", filepath.Base(finalPath))
			if isR2 {
				pst.MarkSynced(config.PlatformCloudflare, filepath.Base(finalPath))
				a.recordEvent(ctx, events.Event{
					EventID:      events.GenerateID(),
					Timestamp:    time.Now().UTC(),
					EventType:    events.TypeR2SyncCompleted,
					Status:       events.StatusSuccess,
					Backend:      "r2",
					FileName:     filepath.Base(finalPath),
					AgentVersion: version.Current,
				})
				// ReparaciÃ³n D5: keep desde config (default 1) en lugar del literal 1.
				if err := b.Rotate(ctx, a.cloudflareKeep()); err != nil {
					a.logger.Warn("rotaciÃ³n en backend remoto con advertencia",
						"backend", b.Name(), "error", err)
				}
			} else if isServer {
				pst.MarkSynced(config.PlatformServer, filepath.Base(finalPath))
				a.recordEvent(ctx, events.Event{
					EventID:      events.GenerateID(),
					Timestamp:    time.Now().UTC(),
					EventType:    events.TypeServerSyncCompleted,
					Status:       events.StatusSuccess,
					Backend:      "server",
					FileName:     filepath.Base(finalPath),
					AgentVersion: version.Current,
				})
				// ReparaciÃ³n D5: keep desde config (default 10) en lugar del literal 10.
				if err := b.Rotate(ctx, a.serverKeep()); err != nil {
					a.logger.Warn("rotaciÃ³n en backend remoto con advertencia",
						"backend", b.Name(), "error", err)
					a.recordEvent(ctx, events.Event{
						EventID:      events.GenerateID(),
						Timestamp:    time.Now().UTC(),
						EventType:    events.TypeServerRotationFailed,
						Status:       events.StatusFailed,
						Backend:      "server",
						ErrorMessage: sanitizeError(err),
						AgentVersion: version.Current,
					})
				} else {
					a.recordEvent(ctx, events.Event{
						EventID:      events.GenerateID(),
						Timestamp:    time.Now().UTC(),
						EventType:    events.TypeServerRotationCompleted,
						Status:       events.StatusSuccess,
						Backend:      "server",
						AgentVersion: version.Current,
					})
				}
			} else {
				if err := b.Rotate(ctx, 0); err != nil {
					a.logger.Warn("rotaciÃ³n en backend remoto con advertencia",
						"backend", b.Name(), "error", err)
				}
			}
			_ = state.Save(a.statePath, st)
		}
	}

	if hadPending {
		a.recordEvent(ctx, events.Event{
			EventID:      events.GenerateID(),
			Timestamp:    time.Now().UTC(),
			EventType:    events.TypePendingSync,
			Status:       events.StatusPending,
			DatabaseName: a.cfg.Database,
			FileName:     filepath.Base(finalPath),
			AgentVersion: version.Current,
		})
		return ErrPendingSync
	}

	a.recordEvent(ctx, events.Event{
		EventID:      events.GenerateID(),
		Timestamp:    time.Now().UTC(),
		EventType:    events.TypeBackupCompleted,
		Status:       events.StatusSuccess,
		DatabaseName: a.cfg.Database,
		FileName:     filepath.Base(finalPath),
		SizeBytes:    backupSize,
		DurationMs:   time.Since(startTime).Milliseconds(),
		AgentVersion: version.Current,
	})

	return nil
}

func (a *App) syncPendingBackup(ctx context.Context, st *state.State, pst *state.ProfileState) {
	if _, err := os.Stat(pst.LastBackupFile); err != nil {
		a.logger.Warn("el archivo pendiente no existe en disco, descartando pendientes",
			"archivo", pst.LastBackupFile)
		pst.SetPending(config.PlatformCloudflare, false)
		pst.SetPending(config.PlatformServer, false)
		_ = state.Save(a.statePath, st)
		return
	}

	a.logger.Info("iniciando sincronizaciÃ³n de backup pendiente", "archivo", pst.LastBackupFile)
	for _, b := range a.backends {
		isR2 := strings.EqualFold(b.Name(), "r2")
		isServer := strings.EqualFold(b.Name(), "server")

		// Solo intentar sincronizar si este backend tiene pendiente
		if (isR2 && !pst.IsPending(config.PlatformCloudflare)) || (isServer && !pst.IsPending(config.PlatformServer)) {
			continue
		}

		syncCtx, syncCancel := context.WithTimeout(ctx, a.backendTimeout(b))
		err := a.uploadWithRetries(syncCtx, b, pst.LastBackupFile)
		syncCancel()
		if err != nil {
			a.logger.Warn("reintento de sync pendiente fallÃ³", "backend", b.Name(), "error", err)
		} else {
			a.logger.Info("sync pendiente exitosa", "backend", b.Name())
			if isR2 {
				pst.MarkSynced(config.PlatformCloudflare, filepath.Base(pst.LastBackupFile))
				_ = b.Rotate(ctx, a.cloudflareKeep())
			} else if isServer {
				pst.MarkSynced(config.PlatformServer, filepath.Base(pst.LastBackupFile))
				_ = b.Rotate(ctx, a.serverKeep())
			} else {
				_ = b.Rotate(ctx, 0)
			}
			_ = state.Save(a.statePath, st)
		}
	}
}

// uploadWithRetries ejecuta la subida aplicando la polÃ­tica Ãºnica de reintentos
// de internal/retry. ReparaciÃ³n D5: Cloudflare.UploadRetries ahora se conecta
// de verdad (antes existÃ­a en config pero nadie lo usaba). MaxAttempts cuenta
// el intento inicial: UploadRetries=3 â†’ hasta 4 intentos.
func (a *App) uploadWithRetries(ctx context.Context, b storage.Backend, path string) error {
	attempts := 1
	if strings.EqualFold(b.Name(), "r2") && a.cloudflareRetries() > 0 {
		attempts = a.cloudflareRetries() + 1
	}
	return retry.Do(ctx, retry.Policy{
		MaxAttempts: attempts,
		BaseDelay:   2 * time.Second,
		MaxDelay:    30 * time.Second,
	}, func(ctx context.Context) error {
		return b.Upload(ctx, path)
	})
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
			return n, fmt.Errorf("borrar huÃ©rfano %s: %w", full, err)
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



