package auth

import (
	"context"
	"errors"
	"net/http"
	"sync"
)

// Manager coordina la validación de sesión, renovación de tokens y autenticación.
type Manager struct {
	client      *Client
	sessionPath string
	mu          sync.RWMutex
	cachedSess  *Session
}

// NewManager inicializa un Manager de autenticación.
func NewManager(baseURL, apiKey, sessionFilePath string, httpClient *http.Client) *Manager {
	return &Manager{
		client:      NewClient(baseURL, apiKey, httpClient),
		sessionPath: sessionFilePath,
	}
}

// GetSession recupera la sesión activa. Si está próxima a expirar o expiró, intenta renovarla con refresh_token.
func (m *Manager) GetSession(ctx context.Context) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 1. Intentar con la sesión en memoria si está vigente
	if m.cachedSess != nil && m.cachedSess.IsValid() {
		return m.cachedSess, nil
	}

	// 2. Cargar desde disco
	sess, err := LoadSession(m.sessionPath)
	if err != nil {
		m.cachedSess = nil
		return nil, err
	}

	// 3. Si sigue siendo válida, cachear y devolver
	if sess.IsValid() {
		m.cachedSess = sess
		return sess, nil
	}

	// 4. Si expiró pero tiene refresh_token, intentar renovar silenciosamente
	if sess.RefreshToken != "" {
		newSess, refErr := m.client.RefreshToken(ctx, sess.RefreshToken)
		if refErr == nil && newSess != nil {
			_ = SaveSession(m.sessionPath, newSess)
			m.cachedSess = newSess
			return newSess, nil
		}
	}

	// Si no se pudo renovar, se invalida
	m.cachedSess = nil
	return nil, ErrSessionExpired
}

// IsAuthenticated devuelve true si hay una sesión válida o renovable en este momento.
func (m *Manager) IsAuthenticated(ctx context.Context) bool {
	sess, err := m.GetSession(ctx)
	return err == nil && sess != nil
}

// CurrentUser devuelve el correo del operador autenticado o cadena vacía si no hay sesión.
func (m *Manager) CurrentUser(ctx context.Context) string {
	sess, err := m.GetSession(ctx)
	if err == nil && sess != nil {
		return sess.User.Email
	}
	return ""
}

// Login autentica contra Supabase y persiste la sesión resultante en disco.
func (m *Manager) Login(ctx context.Context, email, password string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	sess, err := m.client.Login(ctx, email, password)
	if err != nil {
		return nil, err
	}
	if sess == nil {
		return nil, errors.New("respuesta de autenticación vacía")
	}

	if err := SaveSession(m.sessionPath, sess); err != nil {
		return nil, err
	}

	m.cachedSess = sess
	return sess, nil
}

// Logout elimina la sesión persistida y limpia la caché en memoria.
func (m *Manager) Logout() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.cachedSess = nil
	return ClearSession(m.sessionPath)
}
