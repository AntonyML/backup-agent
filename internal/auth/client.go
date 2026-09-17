package auth

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

	"femucaribe-backup-agent/internal/version"
)

const (
	// DefaultSupabaseURL es la URL base del proyecto en Supabase.
	DefaultSupabaseURL = "https://oxpxyiucnzpedawwkosy.supabase.co"
	// DefaultPublishableKey es la clave publishable/anon utilizada para autenticación en clientes.
	DefaultPublishableKey = "sb_publishable_1vsQ8WgX-FrzPwdwV9fgHg_TJt3cLcn"
)

// Client gestiona las solicitudes HTTP contra la API REST de GoTrue (Supabase Auth).
type Client struct {
	authURL    string
	apiKey     string
	httpClient *http.Client
}

// NewClient inicializa un cliente para Supabase Auth.
func NewClient(rawURL, apiKey string, client *http.Client) *Client {
	baseURL := strings.TrimSpace(rawURL)
	if baseURL == "" {
		baseURL = DefaultSupabaseURL
	}
	baseURL = strings.TrimRight(baseURL, "/")
	baseURL = strings.TrimSuffix(baseURL, "/rest/v1")
	baseURL = strings.TrimSuffix(baseURL, "/auth/v1")
	authURL := baseURL + "/auth/v1"

	key := strings.TrimSpace(apiKey)
	if key == "" {
		key = DefaultPublishableKey
	}

	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}

	return &Client{
		authURL:    authURL,
		apiKey:     key,
		httpClient: client,
	}
}

// Login autentica a un operador con su correo y contraseña contra Supabase GoTrue.
func (c *Client) Login(ctx context.Context, email, password string) (*Session, error) {
	email = strings.TrimSpace(email)
	if email == "" {
		return nil, errors.New("el correo electrónico es obligatorio")
	}
	if password == "" {
		return nil, errors.New("la contraseña es obligatoria")
	}

	loginURL := fmt.Sprintf("%s/token?grant_type=password", c.authURL)
	payload := map[string]string{
		"email":    email,
		"password": password,
	}
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("serializar payload de login: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, loginURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("crear request de login: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error de conexión con Supabase Auth: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.parseError(resp)
	}

	var sess Session
	if err := json.NewDecoder(resp.Body).Decode(&sess); err != nil {
		return nil, fmt.Errorf("decodificar respuesta de sesión: %w", err)
	}

	if sess.ExpiresIn > 0 {
		sess.ExpiresAt = time.Now().Add(time.Duration(sess.ExpiresIn) * time.Second)
	} else {
		// Por defecto 1 hora si no viene especificado
		sess.ExpiresAt = time.Now().Add(1 * time.Hour)
	}

	return &sess, nil
}

// RefreshToken renueva una sesión utilizando el token de actualización (refresh_token).
func (c *Client) RefreshToken(ctx context.Context, refreshToken string) (*Session, error) {
	refreshToken = strings.TrimSpace(refreshToken)
	if refreshToken == "" {
		return nil, errors.New("refresh token vacío")
	}

	refreshURL := fmt.Sprintf("%s/token?grant_type=refresh_token", c.authURL)
	payload := map[string]string{
		"refresh_token": refreshToken,
	}
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("serializar payload de refresh: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, refreshURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("crear request de refresh: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error de conexión renovando sesión: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.parseError(resp)
	}

	var sess Session
	if err := json.NewDecoder(resp.Body).Decode(&sess); err != nil {
		return nil, fmt.Errorf("decodificar sesión renovada: %w", err)
	}

	if sess.ExpiresIn > 0 {
		sess.ExpiresAt = time.Now().Add(time.Duration(sess.ExpiresIn) * time.Second)
	} else {
		sess.ExpiresAt = time.Now().Add(1 * time.Hour)
	}

	return &sess, nil
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("User-Agent", "femucaribe-backup-agent/"+version.Current)
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("apikey", c.apiKey)
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
}

func (c *Client) parseError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	var errData struct {
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
		Message          string `json:"message"`
		Msg              string `json:"msg"`
	}
	_ = json.Unmarshal(body, &errData)

	desc := errData.ErrorDescription
	if desc == "" {
		desc = errData.Message
	}
	if desc == "" {
		desc = errData.Msg
	}

	switch resp.StatusCode {
	case http.StatusBadRequest, http.StatusUnauthorized:
		if strings.Contains(strings.ToLower(desc), "invalid login credentials") {
			return errors.New("credenciales inválidas: verifique correo y contraseña")
		}
		if desc != "" {
			return fmt.Errorf("autenticación rechazada: %s", desc)
		}
		return errors.New("credenciales inválidas o usuario no autorizado")
	case http.StatusTooManyRequests:
		return errors.New("demasiados intentos de inicio de sesión: espere un momento")
	default:
		if desc != "" {
			return fmt.Errorf("error de autenticación (HTTP %d): %s", resp.StatusCode, desc)
		}
		return fmt.Errorf("error del servidor de autenticación (HTTP %d)", resp.StatusCode)
	}
}
