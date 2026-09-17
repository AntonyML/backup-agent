package events

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"femucaribe-backup-agent/internal/config"
	"femucaribe-backup-agent/internal/version"
)

// SupabaseRepository implementa EventRepository consumiendo la API REST de Supabase (PostgREST).
type SupabaseRepository struct {
	cfg        config.SupabaseConfig
	apiKey     string
	httpClient *http.Client
}

// NewSupabaseRepository instancia un repositorio Supabase encapsulando el cliente HTTP.
func NewSupabaseRepository(cfg config.SupabaseConfig, apiKey string, client *http.Client) *SupabaseRepository {
	if client == nil {
		timeout := time.Duration(cfg.TimeoutSec) * time.Second
		if timeout <= 0 {
			timeout = 10 * time.Second
		}
		client = &http.Client{Timeout: timeout}
	}
	return &SupabaseRepository{
		cfg:        cfg,
		apiKey:     apiKey,
		httpClient: client,
	}
}

// sendRequest ejecuta peticiones HTTP autenticadas hacia PostgREST con manejo de errores y códigos de estado.
func (r *SupabaseRepository) sendRequest(ctx context.Context, method, endpoint string, payload []byte, prefer string) error {
	if !r.cfg.Enabled {
		return ErrDisabled
	}
	if strings.TrimSpace(r.apiKey) == "" {
		return fmt.Errorf("%w: api_key no proporcionada", ErrAuthFailed)
	}

	baseURL := strings.TrimRight(r.cfg.URL, "/")
	baseURL = strings.TrimSuffix(baseURL, "/rest/v1")
	fullURL := baseURL + "/rest/v1" + endpoint

	var bodyReader io.Reader
	if len(payload) > 0 {
		bodyReader = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
	if err != nil {
		return fmt.Errorf("crear request: %w", err)
	}

	req.Header.Set("User-Agent", "backup-agent/"+version.Current)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("apikey", r.apiKey)
	req.Header.Set("Authorization", "Bearer "+r.apiKey)
	if prefer != "" {
		req.Header.Set("Prefer", prefer)
	}

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("enviar petición a Supabase (%s): %w", endpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	respBody := strings.TrimSpace(string(bodyBytes))

	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("%w (HTTP %d): %s", ErrAuthFailed, resp.StatusCode, respBody)
	case http.StatusTooManyRequests:
		return fmt.Errorf("%w (HTTP 429): %s", ErrRateLimited, respBody)
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return fmt.Errorf("%w (HTTP %d): %s", ErrSchemaInvalid, resp.StatusCode, respBody)
	default:
		return fmt.Errorf("supabase HTTP %d: %s", resp.StatusCode, respBody)
	}
}

// RegisterHost registra o actualiza las especificaciones de hardware y SO del equipo en backup_hosts.
func (r *SupabaseRepository) RegisterHost(ctx context.Context, host HostTelemetry) error {
	payload, err := json.Marshal(host)
	if err != nil {
		return fmt.Errorf("serializar host: %w", err)
	}
	return r.sendRequest(ctx, http.MethodPost, "/backup_hosts", payload, "return=minimal,resolution=merge-duplicates")
}

// StartRun crea la sesión inicial de la corrida en backup_runs con su Correlation ID (run_id).
func (r *SupabaseRepository) StartRun(ctx context.Context, run RunTelemetry) error {
	if run.StartedAt.IsZero() {
		run.StartedAt = time.Now().UTC()
	}
	if run.Status == "" {
		run.Status = StatusRunning
	}
	payload, err := json.Marshal(run)
	if err != nil {
		return fmt.Errorf("serializar run: %w", err)
	}
	return r.sendRequest(ctx, http.MethodPost, "/backup_runs", payload, "return=minimal,resolution=merge-duplicates")
}

// FinishRun actualiza el estado final, duración y posibles errores de la corrida en backup_runs.
func (r *SupabaseRepository) FinishRun(ctx context.Context, run RunTelemetry) error {
	updateData := map[string]any{
		"status":      run.Status,
		"finished_at": run.FinishedAt,
		"duration_ms": run.DurationMs,
	}
	if run.ErrorMessage != "" {
		updateData["error_message"] = run.ErrorMessage
	}
	if run.ErrorStage != "" {
		updateData["error_stage"] = run.ErrorStage
	}

	payload, err := json.Marshal(updateData)
	if err != nil {
		return fmt.Errorf("serializar actualización de corrida: %w", err)
	}
	endpoint := fmt.Sprintf("/backup_runs?run_id=eq.%s", run.RunID)
	return r.sendRequest(ctx, http.MethodPatch, endpoint, payload, "return=minimal")
}

// Append envía el evento a Supabase vía POST /rest/v1/backup_events con idempotencia garantizada.
func (r *SupabaseRepository) Append(ctx context.Context, event Event) error {
	if event.EventID == "" {
		event.EventID = GenerateID()
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}

	details := make(map[string]any)
	for k, v := range event.Details {
		details[k] = v
	}
	if event.Hostname != "" {
		details["hostname"] = event.Hostname
	}
	if event.DatabaseName != "" {
		details["database_name"] = event.DatabaseName
	}
	if event.FileName != "" {
		details["filename"] = event.FileName
	}
	if event.SizeBytes > 0 {
		details["size_bytes"] = event.SizeBytes
	}
	if event.AgentVersion != "" {
		details["agent_version"] = event.AgentVersion
	}

	dto := struct {
		EventID      string         `json:"event_id"`
		RunID        *string        `json:"run_id,omitempty"`
		Timestamp    time.Time      `json:"timestamp"`
		EventType    string         `json:"event_type"`
		Status       string         `json:"status"`
		Backend      *string        `json:"backend,omitempty"`
		DurationMs   *int64         `json:"duration_ms,omitempty"`
		ErrorMessage *string        `json:"error_message,omitempty"`
		Details      map[string]any `json:"details"`
	}{
		EventID:   event.EventID,
		Timestamp: event.Timestamp,
		EventType: event.EventType,
		Status:    event.Status,
		Details:   details,
	}

	if event.RunID != "" {
		dto.RunID = &event.RunID
	}
	if event.Backend != "" {
		dto.Backend = &event.Backend
	}
	if event.DurationMs > 0 {
		dto.DurationMs = &event.DurationMs
	}
	if event.ErrorMessage != "" {
		dto.ErrorMessage = &event.ErrorMessage
	}

	payload, err := json.Marshal(dto)
	if err != nil {
		return fmt.Errorf("serializar evento: %w", err)
	}

	return r.sendRequest(ctx, http.MethodPost, "/backup_events", payload, "return=minimal,resolution=ignore-duplicates")
}

// RecordArtifact registra un archivo .bak generado y su verificación en backup_artifacts.
func (r *SupabaseRepository) RecordArtifact(ctx context.Context, artifact ArtifactTelemetry) error {
	if artifact.ArtifactID == "" {
		artifact.ArtifactID = GenerateArtifactID()
	}
	if artifact.CreatedAt.IsZero() {
		artifact.CreatedAt = time.Now().UTC()
	}

	payload, err := json.Marshal(artifact)
	if err != nil {
		return fmt.Errorf("serializar artefacto: %w", err)
	}

	return r.sendRequest(ctx, http.MethodPost, "/backup_artifacts", payload, "return=minimal,resolution=merge-duplicates")
}

// IsRetryable evalúa si el error devuelto por el repositorio amerita reintento (timeout, 429, error de red, 5xx).
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrAuthFailed) || errors.Is(err, ErrSchemaInvalid) || errors.Is(err, ErrDisabled) {
		return false
	}
	return true
}

