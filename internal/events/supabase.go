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

// Append envía el evento a Supabase vía POST /rest/v1/backup_events con idempotencia garantizada.
func (r *SupabaseRepository) Append(ctx context.Context, event Event) error {
	if !r.cfg.Enabled {
		return ErrDisabled
	}
	if strings.TrimSpace(r.apiKey) == "" {
		return fmt.Errorf("%w: api_key no proporcionada", ErrAuthFailed)
	}

	// Completar campos si vienen vacíos
	if event.EventID == "" {
		event.EventID = GenerateID()
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	if event.AgentVersion == "" {
		event.AgentVersion = version.Current
	}

	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("serializar evento: %w", err)
	}

	url := strings.TrimRight(r.cfg.URL, "/") + "/rest/v1/backup_events"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("crear request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("apikey", r.apiKey)
	req.Header.Set("Authorization", "Bearer "+r.apiKey)
	// Idempotencia: resolution=ignore-duplicates ante reintentos de red
	req.Header.Set("Prefer", "return=minimal,resolution=ignore-duplicates")

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("enviar evento a Supabase: %w", err)
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
