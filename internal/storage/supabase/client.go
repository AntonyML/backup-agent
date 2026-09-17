package supabase

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"femucaribe-backup-agent/internal/storage"
	"femucaribe-backup-agent/internal/version"
)

// ObjectItem representa un objeto devuelto por el endpoint de listado de Supabase Storage.
type ObjectItem struct {
	Name      string    `json:"name"`
	ID        string    `json:"id"`
	UpdatedAt time.Time `json:"updated_at"`
	CreatedAt time.Time `json:"created_at"`
}

// Client gestiona la interacción HTTP con la API REST de Supabase Storage (/storage/v1).
type Client struct {
	storageURL string
	apiKey     string
	httpClient *http.Client
}

// NewClient inicializa un cliente para Supabase Storage.
func NewClient(rawURL, apiKey string, timeout time.Duration, client *http.Client) *Client {
	baseURL := strings.TrimRight(rawURL, "/")
	baseURL = strings.TrimSuffix(baseURL, "/rest/v1")
	storageURL := baseURL + "/storage/v1"

	if client == nil {
		if timeout <= 0 {
			timeout = 600 * time.Second
		}
		client = &http.Client{Timeout: timeout}
	}

	return &Client{
		storageURL: storageURL,
		apiKey:     apiKey,
		httpClient: client,
	}
}

// EnsureBucket comprueba que el bucket exista o lo crea como privado en caso de no existir.
func (c *Client) EnsureBucket(ctx context.Context, bucket string) error {
	getURL := fmt.Sprintf("%s/bucket/%s", c.storageURL, bucket)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, getURL, nil)
	if err != nil {
		return fmt.Errorf("crear request get bucket: %w", err)
	}
	c.setHeaders(req, "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return storage.NewRetryableError(fmt.Errorf("supabase storage: consultar bucket: %w", err))
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return nil
	}

	// Si no existe (404), intentamos crearlo
	if resp.StatusCode == http.StatusNotFound {
		createURL := fmt.Sprintf("%s/bucket", c.storageURL)
		payload := map[string]any{
			"id":     bucket,
			"name":   bucket,
			"public": false,
		}
		bodyBytes, _ := json.Marshal(payload)
		postReq, err := http.NewRequestWithContext(ctx, http.MethodPost, createURL, bytes.NewReader(bodyBytes))
		if err != nil {
			return fmt.Errorf("crear request create bucket: %w", err)
		}
		c.setHeaders(postReq, "application/json")

		postResp, err := c.httpClient.Do(postReq)
		if err != nil {
			return storage.NewRetryableError(fmt.Errorf("supabase storage: crear bucket: %w", err))
		}
		defer postResp.Body.Close()

		if postResp.StatusCode >= 200 && postResp.StatusCode < 300 {
			return nil
		}
		// Si devuelve 400/409 porque ya existe de forma concurrente, se tolera
		if postResp.StatusCode == http.StatusBadRequest || postResp.StatusCode == http.StatusConflict {
			return nil
		}
		respBody, _ := io.ReadAll(io.LimitReader(postResp.Body, 512))
		return fmt.Errorf("supabase storage: crear bucket %s falló (HTTP %d): %s", bucket, postResp.StatusCode, string(respBody))
	}

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	if resp.StatusCode >= 500 {
		return storage.NewRetryableError(fmt.Errorf("supabase storage: error de servidor consultando bucket (HTTP %d): %s", resp.StatusCode, string(respBody)))
	}
	return fmt.Errorf("supabase storage: error consultando bucket %s (HTTP %d): %s", bucket, resp.StatusCode, string(respBody))
}

// Upload transmite el archivo local directamente hacia /storage/v1/object/{bucket}/{objectPath}.
func (c *Client) Upload(ctx context.Context, bucket, objectPath, localPath string) error {
	f, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("abrir archivo local: %w", err)
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return fmt.Errorf("obtener tamaño de archivo: %w", err)
	}

	cleanPath := strings.TrimPrefix(objectPath, "/")
	uploadURL := fmt.Sprintf("%s/object/%s/%s", c.storageURL, bucket, cleanPath)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadURL, f)
	if err != nil {
		return fmt.Errorf("crear request upload: %w", err)
	}
	c.setHeaders(req, "application/octet-stream")
	req.Header.Set("x-upsert", "true")
	req.ContentLength = fi.Size()

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return storage.NewRetryableError(fmt.Errorf("supabase storage: subida de %s: %w", cleanPath, err))
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests {
		return storage.NewRetryableError(fmt.Errorf("supabase storage: error transitorio subiendo %s (HTTP %d): %s", cleanPath, resp.StatusCode, string(respBody)))
	}
	return fmt.Errorf("supabase storage: subida de %s falló (HTTP %d): %s", cleanPath, resp.StatusCode, string(respBody))
}

// List obtiene los objetos contenidos en un prefijo del bucket.
func (c *Client) List(ctx context.Context, bucket, prefix string) ([]ObjectItem, error) {
	listURL := fmt.Sprintf("%s/object/list/%s", c.storageURL, bucket)
	cleanPrefix := strings.Trim(prefix, "/")
	payload := map[string]any{
		"prefix": cleanPrefix,
		"limit":  1000,
		"sortBy": map[string]string{
			"column": "name",
			"order":  "asc",
		},
	}
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("serializar list payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, listURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("crear request list: %w", err)
	}
	c.setHeaders(req, "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, storage.NewRetryableError(fmt.Errorf("supabase storage: listar objetos: %w", err))
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests {
			return nil, storage.NewRetryableError(fmt.Errorf("supabase storage: error transitorio listando objetos (HTTP %d): %s", resp.StatusCode, string(respBody)))
		}
		return nil, fmt.Errorf("supabase storage: listar objetos falló (HTTP %d): %s", resp.StatusCode, string(respBody))
	}

	var items []ObjectItem
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		return nil, fmt.Errorf("decodificar listado de objetos: %w", err)
	}
	return items, nil
}

// Delete elimina una lista de objetos en el bucket.
func (c *Client) Delete(ctx context.Context, bucket string, prefixes []string) error {
	if len(prefixes) == 0 {
		return nil
	}

	delURL := fmt.Sprintf("%s/object/%s", c.storageURL, bucket)
	payload := map[string]any{
		"prefixes": prefixes,
	}
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("serializar delete payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, delURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("crear request delete: %w", err)
	}
	c.setHeaders(req, "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return storage.NewRetryableError(fmt.Errorf("supabase storage: borrar objetos: %w", err))
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests {
			return storage.NewRetryableError(fmt.Errorf("supabase storage: error transitorio borrando objetos (HTTP %d): %s", resp.StatusCode, string(respBody)))
		}
		return fmt.Errorf("supabase storage: borrar objetos falló (HTTP %d): %s", resp.StatusCode, string(respBody))
	}
	return nil
}

func (c *Client) setHeaders(req *http.Request, contentType string) {
	req.Header.Set("User-Agent", "backup-agent/"+version.Current)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if c.apiKey != "" {
		req.Header.Set("apikey", c.apiKey)
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
}
