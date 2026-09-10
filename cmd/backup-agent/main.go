package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"femucaribe-backup-agent/internal/config"
	"femucaribe-backup-agent/internal/hasher"
	"femucaribe-backup-agent/internal/lock"
	"femucaribe-backup-agent/internal/logger"
	"femucaribe-backup-agent/internal/rotation"
	"femucaribe-backup-agent/internal/secrets"
	"femucaribe-backup-agent/internal/sqlbackup"
	"femucaribe-backup-agent/internal/state"
	"femucaribe-backup-agent/internal/storage/r2"
)

const (
	exitOK     = 0
	exitError  = 1
	exitLocked = 2
)

func main() {
	os.Exit(run(os.Args[1:]))
}

// run ejecuta una corrida batch de vida corta o subcomandos de configuración.
// Devuelve el exit code (0 ok o ya-hecho-hoy, 1 error, 2 lock ajeno) sin llamar os.Exit.
func run(args []string) int {
	if len(args) > 0 {
		if args[0] == "configure" || (args[0] == "agent" && len(args) > 1 && args[1] == "configure") {
			return runConfigure(exeDir())
		}
	}

	fs := flag.NewFlagSet("backup-agent", flag.ContinueOnError)
	force := fs.Bool("force", false, "forzar corrida manual aunque ya exista backup de hoy")
	configPath := fs.String("config", "", "ruta a config.json (default: config.json junto al binario)")
	if err := fs.Parse(args); err != nil {
		return exitError
	}

	exeDir := exeDir()
	cfgPath := *configPath
	if cfgPath == "" {
		cfgPath = filepath.Join(exeDir, "config.json")
	}
	statePath := filepath.Join(exeDir, "state.json")
	lockPath := filepath.Join(exeDir, "agent.lock")
	log := logger.New(filepath.Join(exeDir, "logs"))

	cfg, err := config.Load(cfgPath)
	if err != nil {
		log.Errorf("config inválida (%s): %v", cfgPath, err)
		return exitError
	}
	if err := cfg.Validate(); err != nil {
		log.Errorf("config inválida: %v", err)
		return exitError
	}
	log.Infof("backup-agent inicio (db=%s server=%s backup_dir=%s retain=%d force=%v)",
		cfg.Database, cfg.Server, cfg.BackupDir, cfg.Retain, *force)

	// Lock: evita dos disparos el mismo día (startup + trigger) u
	// operaciones manuales superpuestas. Huérfano (PID muerto) se reclama.
	lh, err := lock.Acquire(lockPath)
	if err != nil {
		if errors.Is(err, lock.ErrLocked) {
			log.Errorf("otra instancia está corriendo, salgo sin hacer nada: %v", err)
			return exitLocked
		}
		log.Errorf("no se pudo tomar el lock %s: %v", lockPath, err)
		return exitError
	}
	defer lh.Release()

	if err := os.MkdirAll(cfg.BackupDir, 0o755); err != nil {
		log.Errorf("no se pudo crear backup_dir %s: %v", cfg.BackupDir, err)
		return exitError
	}

	// Arranque: descartar .tmp huérfanos de una corrida interrumpida
	// (kill/apagón). Nunca se intenta "resumir" un backup a medias.
	if n, err := removeTmpOrphans(cfg.BackupDir, log); err != nil {
		log.Errorf("limpieza de .tmp huérfanos: %v", err)
		return exitError
	} else if n > 0 {
		log.Infof("limpieza: %d .tmp huérfano(s) descartados", n)
	}

	st, err := state.Load(statePath)
	if err != nil {
		log.Errorf("state.json corrupto, lo trato como primera corrida: %v", err)
		st = &state.State{}
	}

	// Sincronización pendiente a R2: intentar sincronizar el archivo pendiente ANTES
	// de generar un backup nuevo del día.
	if st.PendingSync.R2 && st.LastBackupFile != "" {
		log.Infof("arranque: detectada sincronización pendiente a R2 para %s", st.LastBackupFile)
		if _, err := os.Stat(st.LastBackupFile); err == nil {
			r2Ctx, r2Cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			r2Client, err := getR2Client(r2Ctx, exeDir)
			if err != nil {
				log.Errorf("sincronización pendiente a R2: no se pudo inicializar cliente: %v", err)
			} else {
				if err := syncToR2(r2Ctx, r2Client, cfg.Database, st.LastBackupFile, log); err != nil {
					log.Errorf("falló sincronización pendiente a R2: %v", err)
				} else {
					st.MarkR2Synced(filepath.Base(st.LastBackupFile))
					if err := state.Save(statePath, st); err != nil {
						log.Errorf("no se pudo guardar state.json tras sync pendiente: %v", err)
					}
				}
			}
			r2Cancel()
		} else {
			log.Warnf("el archivo de backup pendiente %s no existe en disco; se descarta el pendiente", st.LastBackupFile)
			st.SetPendingR2(false)
			_ = state.Save(statePath, st)
		}
	}

	// Idempotencia diaria: una sola copia válida por día.
	if st.RanOn(state.Today()) && !*force {
		log.Infof("ya existe backup de hoy (%s sha256=%s), no repito (usá --force para forzar)",
			st.LastBackupFile, st.SHA256)
		return exitOK
	}
	if *force {
		log.Infof("modo --force: se ignora idempotencia diaria (last_run_date=%s)", st.LastRunDate)
	}

	stamp := time.Now().Format("20060102_1504")
	finalPath := filepath.Join(cfg.BackupDir, fmt.Sprintf("%s_%s.bak", cfg.Database, stamp))
	tmpPath := finalPath + ".tmp"

	// Conexión (falla rápido si SQL detenido/instancia inaccesible,
	// sin tocar state.json ni dejar archivos a medio escribir).
	db, err := sqlbackup.Open(cfg.Server, cfg.LoginTimeoutSec)
	if err != nil {
		log.Errorf("conexión SQL: %v", err)
		return exitError
	}
	defer db.Close()

	// Espacio libre ANTES del BACKUP (falla rápido con mensaje claro).
	sizeCtx, sizeCancel := context.WithTimeout(context.Background(), 60*time.Second)
	needed, err := sqlbackup.DatabaseSizeBytes(sizeCtx, db, cfg.Database)
	sizeCancel()
	if err != nil {
		log.Errorf("%v", err)
		return exitError
	}
	log.Infof("tamaño estimado de %s: %.2f GB", cfg.Database, float64(needed)/(1024*1024*1024))
	if err := sqlbackup.EnsureFreeSpace(cfg.BackupDir, needed); err != nil {
		log.Errorf("%v", err)
		return exitError
	}

	var ctx context.Context
	var cancel context.CancelFunc
	if cfg.BackupTimeoutSec > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), time.Duration(cfg.BackupTimeoutSec)*time.Second)
	} else {
		ctx, cancel = context.WithCancel(context.Background())
	}
	defer cancel()

	log.Infof("BACKUP DATABASE %s -> %s", cfg.Database, tmpPath)
	if err := sqlbackup.BackupDatabase(ctx, db, cfg.Database, tmpPath); err != nil {
		log.Errorf("%v", err)
		removeBestEffort(tmpPath)
		return exitError
	}

	log.Infof("RESTORE VERIFYONLY sobre %s", tmpPath)
	if err := sqlbackup.VerifyBackup(ctx, db, tmpPath); err != nil {
		log.Errorf("%v", err)
		removeBestEffort(tmpPath)
		return exitError
	}

	sum, err := hasher.File(tmpPath)
	if err != nil {
		log.Errorf("SHA-256 del temporal: %v", err)
		removeBestEffort(tmpPath)
		return exitError
	}

	// Rename atómico a nombre final solo si el temporal es válido.
	if err := atomicRename(tmpPath, finalPath); err != nil {
		log.Errorf("rename a nombre final: %v", err)
		removeBestEffort(tmpPath)
		return exitError
	}

	// Estado: solo después de copia válida.
	st.LastRunDate = state.Today()
	st.LastBackupFile = finalPath
	st.SHA256 = sum
	if err := state.Save(statePath, st); err != nil {
		log.Errorf("el backup %s es válido pero no se pudo guardar state.json: %v", finalPath, err)
		return exitError
	}

	// Rotación local solo después de confirmar la copia nueva.
	deleted, err := rotation.Rotate(cfg.BackupDir, cfg.Retain)
	if err != nil {
		log.Errorf("el backup %s es válido pero la rotación falló (revisar a mano): %v", finalPath, err)
		return exitError
	}
	for _, d := range deleted {
		log.Infof("rotación local: borrado %s", d)
	}

	// Sincronización a Cloudflare R2 (Fase 2)
	r2Ctx, r2Cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer r2Cancel()

	r2Client, err := getR2Client(r2Ctx, exeDir)
	if err != nil {
		log.Errorf("Cloudflare R2: credenciales no configuradas o inválidas (%v); el backup local es exitoso, queda pendiente de sync", err)
		st.SetPendingR2(true)
		if err := state.Save(statePath, st); err != nil {
			log.Errorf("no se pudo guardar state.json con pending_sync: %v", err)
		}
	} else {
		if err := syncToR2(r2Ctx, r2Client, cfg.Database, finalPath, log); err != nil {
			log.Errorf("falló subida a R2 (%v); el backup local es exitoso, queda pendiente de sync", err)
			st.SetPendingR2(true)
			if err := state.Save(statePath, st); err != nil {
				log.Errorf("no se pudo guardar state.json con pending_sync: %v", err)
			}
		} else {
			st.MarkR2Synced(filepath.Base(finalPath))
			if err := state.Save(statePath, st); err != nil {
				log.Errorf("no se pudo guardar state.json con sync R2 exitosa: %v", err)
			}
		}
	}

	log.Infof("listo: %s sha256=%s", finalPath, sum)
	return exitOK
}

func runConfigure(dir string) int {
	datPath := filepath.Join(dir, "config.dat")
	if err := secrets.Configure(datPath, os.Stdin, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "Error en configuración: %v\n", err)
		return exitError
	}
	return exitOK
}

func getR2Client(ctx context.Context, dir string) (*r2.Client, error) {
	datPath := filepath.Join(dir, "config.dat")
	creds, err := secrets.Load(datPath)
	if err != nil {
		return nil, err
	}
	return r2.New(ctx, *creds)
}

func syncToR2(ctx context.Context, client *r2.Client, database, localFile string, log *logger.Logger) error {
	remoteKey := r2.KeyForDatabase(database, localFile)
	log.Infof("subiendo a R2: %s -> %s", localFile, remoteKey)
	if err := client.Upload(ctx, localFile, remoteKey); err != nil {
		return err
	}
	log.Infof("subida a R2 confirmada: %s", remoteKey)

	prefix := fmt.Sprintf("%s/", database)
	deleted, err := client.Rotate(ctx, prefix, remoteKey)
	if err != nil {
		log.Warnf("rotación remota R2 con advertencias (no crítico): %v", err)
	}
	for _, d := range deleted {
		log.Infof("rotación remota R2: borrado %s", d)
	}
	return nil
}

// removeTmpOrphans borra *.tmp del directorio y devuelve cuántos borró.
func removeTmpOrphans(dir string, log *logger.Logger) (int, error) {
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
		if log != nil {
			log.Infof("huérfano descartado: %s", e.Name())
		}
	}
	return n, nil
}

// atomicRename mueve src a dst con reemplazo si existe.
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

func removeBestEffort(path string) {
	_ = os.Remove(path)
}

// exeDir es el directorio del binario.
func exeDir() string {
	if exe, err := os.Executable(); err == nil {
		if dir := filepath.Dir(exe); dir != "" {
			return dir
		}
	}
	if cwd, err := os.Getwd(); err == nil {
		return cwd
	}
	return "."
}
