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
	"femucaribe-backup-agent/internal/sqlbackup"
	"femucaribe-backup-agent/internal/state"
)

const (
	exitOK     = 0
	exitError  = 1
	exitLocked = 2
)

func main() {
	os.Exit(run())
}

// run ejecuta una corrida batch de vida corta. Devuelve el exit code
// (0 ok o ya-hecho-hoy, 1 error, 2 lock ajeno) sin llamar os.Exit,
// para que sea testeable.
func run() int {
	force := flag.Bool("force", false, "forzar corrida manual aunque ya exista backup de hoy")
	configPath := flag.String("config", "", "ruta a config.json (default: config.json junto al binario)")
	flag.Parse()

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

	// Estado: solo después de copia válida. La fecha se toma al completar
	// (una corrida que cruza medianoche cuenta para el día nuevo).
	st.LastRunDate = state.Today()
	st.LastBackupFile = finalPath
	st.SHA256 = sum
	if err := state.Save(statePath, st); err != nil {
		log.Errorf("el backup %s es válido pero no se pudo guardar state.json: %v", finalPath, err)
		return exitError
	}

	// Rotación solo después de confirmar la copia nueva.
	deleted, err := rotation.Rotate(cfg.BackupDir, cfg.Retain)
	if err != nil {
		log.Errorf("el backup %s es válido pero la rotación falló (revisar a mano): %v", finalPath, err)
		return exitError
	}
	for _, d := range deleted {
		log.Infof("rotación: borrado %s", d)
	}

	log.Infof("listo: %s sha256=%s", finalPath, sum)
	return exitOK
}

// removeTmpOrphans borra *.tmp del directorio y devuelve cuántos borró.
// No toca .bak finales, state.json ni nada más.
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

// atomicRename mueve src a dst. En Windows os.Rename no pisa el destino,
// por eso se borra primero si existe (el nombre lleva timestamp de minuto,
// colisiona solo si se fuerza dos corridas el mismo minuto).
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

// exeDir es el directorio del binario (ahí viven config.json,
// state.json, agent.lock y logs/). Si no se puede determinar,
// cae al directorio actual.
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
