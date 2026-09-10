package events

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"time"

	"femucaribe-backup-agent/internal/version"
)

// Errores centinela para clasificación semántica de fallos en Supabase.
var (
	ErrAuthFailed    = errors.New("supabase: autenticación o permisos inválidos (401/403)")
	ErrRateLimited   = errors.New("supabase: tasa de peticiones excedida (429)")
	ErrSchemaInvalid = errors.New("supabase: esquema de datos o payload inválido (400/422)")
	ErrDisabled      = errors.New("supabase: servicio deshabilitado en configuración")
)

// Nombres canónicos de tipos de evento operativo según especificación de Fase 4.
const (
	TypeAgentStarted            = "agent_started"
	TypeAgentFinished           = "agent_finished"
	TypeBackupStarted           = "backup_started"
	TypeBackupCompleted         = "backup_completed"
	TypeBackupFailed            = "backup_failed"
	TypeLocalBackupCompleted    = "local_backup_completed"
	TypeLocalRotationCompleted  = "local_rotation_completed"
	TypeLocalRotationFailed     = "local_rotation_failed"
	TypeR2SyncCompleted         = "r2_sync_completed"
	TypeR2SyncFailed            = "r2_sync_failed"
	TypeServerSyncCompleted     = "server_sync_completed"
	TypeServerSyncFailed        = "server_sync_failed"
	TypeServerRotationCompleted = "server_rotation_completed"
	TypeServerRotationFailed    = "server_rotation_failed"
	TypePendingSync             = "pending_sync"
)

// Estados de evento operativo.
const (
	StatusSuccess = "SUCCESS"
	StatusFailed  = "FAILED"
	StatusPending = "PENDING"
	StatusRunning = "RUNNING"
)

// Event representa un evento operativo del Backup Agent.
type Event struct {
	EventID      string    `json:"event_id"`
	Timestamp    time.Time `json:"timestamp"`
	EventType    string    `json:"event_type"`
	Status       string    `json:"status"`
	Backend      string    `json:"backend,omitempty"`
	Hostname     string    `json:"hostname,omitempty"`
	DatabaseName string    `json:"database_name,omitempty"`
	FileName     string    `json:"filename,omitempty"`
	SizeBytes    int64     `json:"size_bytes,omitempty"`
	DurationMs   int64     `json:"duration_ms,omitempty"`
	ErrorMessage string    `json:"error_message,omitempty"`
	AgentVersion string    `json:"agent_version,omitempty"`
}

// NewEvent crea un Event inicializado con identificador criptográfico único,
// timestamp UTC actual, nombre de host y versión del agente.
func NewEvent(eventType, status string) Event {
	h, _ := os.Hostname()
	return Event{
		EventID:      GenerateID(),
		Timestamp:    time.Now().UTC(),
		EventType:    eventType,
		Status:       status,
		Hostname:     h,
		AgentVersion: version.Current,
	}
}

// GenerateID genera un identificador único seguro (hex de 16 bytes con prefijo 'evt_').
func GenerateID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("evt_%s", hex.EncodeToString(b))
}

// EventRepository define la abstracción desacoplada para persistir eventos.
type EventRepository interface {
	Append(ctx context.Context, event Event) error
}
