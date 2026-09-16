package events

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"time"

	"femucaribe-backup-agent/internal/hostinfo"
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
	TypeServerRotationCompleted   = "server_rotation_completed"
	TypeServerRotationFailed      = "server_rotation_failed"
	TypeSupabaseSyncCompleted     = "supabase_sync_completed"
	TypeSupabaseSyncFailed        = "supabase_sync_failed"
	TypeSupabaseRotationCompleted = "supabase_rotation_completed"
	TypeSupabaseRotationFailed    = "supabase_rotation_failed"
	TypePendingSync               = "pending_sync"
)

// Estados de evento operativo.
const (
	StatusSuccess = "SUCCESS"
	StatusFailed  = "FAILED"
	StatusPending = "PENDING"
	StatusRunning = "RUNNING"
	StatusSkipped = "SKIPPED"
	StatusWarning = "WARNING"
)

// HostTelemetry es un alias de hostinfo.HostSpecs para persistencia en backup_hosts.
type HostTelemetry = hostinfo.HostSpecs

// RunTelemetry representa la corrida completa y su telemetría en backup_runs.
type RunTelemetry struct {
	RunID                string         `json:"run_id"`
	HostID               string         `json:"host_id"`
	StartedAt            time.Time      `json:"started_at"`
	FinishedAt           *time.Time     `json:"finished_at,omitempty"`
	DurationMs           int64          `json:"duration_ms,omitempty"`
	Status               string         `json:"status"`
	TriggerMode          string         `json:"trigger_mode"`
	ProfileName          string         `json:"profile_name"`
	DatabaseName         string         `json:"database_name"`
	SQLServerInstance    string         `json:"sql_server_instance"`
	SQLAuthMode          string         `json:"sql_auth_mode"`
	PrimaryIP            string         `json:"primary_ip"`
	LocalIPs             []string       `json:"local_ips"`
	Username             string         `json:"username"`
	UserDomain           string         `json:"user_domain,omitempty"`
	IsElevatedAdmin      bool           `json:"is_elevated_admin"`
	ProcessID            int            `json:"process_id"`
	ProcessPath          string         `json:"process_path"`
	AgentVersion         string         `json:"agent_version"`
	GoVersion            string         `json:"go_version,omitempty"`
	FreeRAMBytes         int64          `json:"free_ram_bytes,omitempty"`
	BackupDiskDrive      string         `json:"backup_disk_drive,omitempty"`
	BackupDiskFreeBytes  int64          `json:"backup_disk_free_bytes,omitempty"`
	BackupDiskTotalBytes int64          `json:"backup_disk_total_bytes,omitempty"`
	SystemUptimeSeconds  int64          `json:"system_uptime_seconds,omitempty"`
	Timezone             string         `json:"timezone,omitempty"`
	ErrorMessage         string         `json:"error_message,omitempty"`
	ErrorStage           string         `json:"error_stage,omitempty"`
	Metadata             map[string]any `json:"metadata,omitempty"`
}

// Event representa un paso o hito individual vinculado a una corrida en backup_events.
type Event struct {
	EventID      string         `json:"event_id"`
	RunID        string         `json:"run_id"`
	Timestamp    time.Time      `json:"timestamp"`
	EventType    string         `json:"event_type"`
	Status       string         `json:"status"`
	Backend      string         `json:"backend,omitempty"`
	Hostname     string         `json:"hostname,omitempty"`
	DatabaseName string         `json:"database_name,omitempty"`
	FileName     string         `json:"filename,omitempty"`
	SizeBytes    int64          `json:"size_bytes,omitempty"`
	DurationMs   int64          `json:"duration_ms,omitempty"`
	ErrorMessage string         `json:"error_message,omitempty"`
	AgentVersion string         `json:"agent_version,omitempty"`
	Details      map[string]any `json:"details,omitempty"`
}

// ArtifactTelemetry representa un archivo .bak generado y su verificación en backup_artifacts.
type ArtifactTelemetry struct {
	ArtifactID  string    `json:"artifact_id"`
	RunID       string    `json:"run_id"`
	Backend     string    `json:"backend"`
	Filename    string    `json:"filename"`
	SizeBytes   int64     `json:"size_bytes"`
	SHA256      string    `json:"sha256"`
	IsVerified  bool      `json:"is_verified"`
	StoragePath string    `json:"storage_path"`
	CreatedAt   time.Time `json:"created_at,omitempty"`
}

// NewEvent crea un Event inicializado con identificador criptográfico único y timestamp UTC actual.
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

// GenerateID genera un identificador único para eventos (hex de 16 bytes con prefijo 'evt_').
func GenerateID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("evt_%s", hex.EncodeToString(b))
}

// GenerateRunID genera un Correlation ID para la corrida (hex de 16 bytes con prefijo 'run_').
func GenerateRunID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("run_%s", hex.EncodeToString(b))
}

// GenerateArtifactID genera un identificador único para un artefacto (hex de 16 bytes con prefijo 'art_').
func GenerateArtifactID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("art_%s", hex.EncodeToString(b))
}

// EventRepository define la abstracción desacoplada para persistir toda la telemetría en Supabase.
type EventRepository interface {
	RegisterHost(ctx context.Context, host HostTelemetry) error
	StartRun(ctx context.Context, run RunTelemetry) error
	FinishRun(ctx context.Context, run RunTelemetry) error
	Append(ctx context.Context, event Event) error
	RecordArtifact(ctx context.Context, artifact ArtifactTelemetry) error
}

