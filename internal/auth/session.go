package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	// ErrNoSession indica que no hay ninguna sesión guardada o disponible.
	ErrNoSession = errors.New("no hay sesión de usuario activa")
	// ErrSessionExpired indica que la sesión ha expirado y no se pudo renovar.
	ErrSessionExpired = errors.New("la sesión ha expirado")
)

// User representa el perfil básico del usuario autenticado en Supabase Auth.
type User struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Phone string `json:"phone,omitempty"`
	Role  string `json:"role,omitempty"`
}

// Session almacena los tokens y la metadata de autenticación del operador.
type Session struct {
	AccessToken  string    `json:"access_token"`
	TokenType    string    `json:"token_type"`
	ExpiresIn    int       `json:"expires_in"`
	RefreshToken string    `json:"refresh_token"`
	User         User      `json:"user"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// UnmarshalJSON decodifica la sesión soportando expires_at como número unix (Supabase GoTrue)
// o como string RFC3339 (sesiones guardadas en disco).
func (s *Session) UnmarshalJSON(data []byte) error {
	type Alias Session
	aux := struct {
		RawExpiresAt json.RawMessage `json:"expires_at"`
		*Alias
	}{
		Alias: (*Alias)(s),
	}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	if len(aux.RawExpiresAt) > 0 && string(aux.RawExpiresAt) != "null" {
		// 1. Caso numérico: timestamp unix en segundos (Supabase Auth GoTrue API)
		var num int64
		if err := json.Unmarshal(aux.RawExpiresAt, &num); err == nil && num > 0 {
			s.ExpiresAt = time.Unix(num, 0)
			return nil
		}
		var fnum float64
		if err := json.Unmarshal(aux.RawExpiresAt, &fnum); err == nil && fnum > 0 {
			s.ExpiresAt = time.Unix(int64(fnum), 0)
			return nil
		}
		// 2. Caso string: formato RFC3339 (guardado en disco)
		var t time.Time
		if err := json.Unmarshal(aux.RawExpiresAt, &t); err == nil {
			s.ExpiresAt = t
			return nil
		}
	}

	// 3. Fallback: calcular a partir de expires_in
	if s.ExpiresIn > 0 {
		s.ExpiresAt = time.Now().Add(time.Duration(s.ExpiresIn) * time.Second)
	}

	return nil
}

// IsValid verifica si el token de acceso sigue siendo válido (con 2 minutos de margen de seguridad).
func (s *Session) IsValid() bool {
	if s == nil || s.AccessToken == "" {
		return false
	}
	if s.ExpiresAt.IsZero() {
		return false
	}
	return time.Now().Add(2 * time.Minute).Before(s.ExpiresAt)
}

// sessionMu sincroniza el acceso concorrente a disco para guardar/leer la sesión.
var sessionMu sync.Mutex

// defaultSessionPath devuelve la ruta donde se persiste la sesión (%APPDATA%\backup-agent\session.json en Windows).
func defaultSessionPath() string {
	configDir, err := os.UserConfigDir()
	if err != nil {
		home, errH := os.UserHomeDir()
		if errH != nil {
			return ".backup-agent-session.json"
		}
		configDir = home
	}
	return filepath.Join(configDir, "backup-agent", "session.json")
}

// SaveSession guarda la sesión de forma atómica con permisos restringidos (0600).
func SaveSession(filePath string, s *Session) error {
	if s == nil {
		return errors.New("sesión nula")
	}

	sessionMu.Lock()
	defer sessionMu.Unlock()

	if filePath == "" {
		filePath = defaultSessionPath()
	}

	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("crear directorio de sesión %s: %w", dir, err)
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("serializar sesión: %w", err)
	}

	tmpFile := filePath + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0o600); err != nil {
		return fmt.Errorf("escribir temporal de sesión: %w", err)
	}

	if err := os.Rename(tmpFile, filePath); err != nil {
		_ = os.Remove(tmpFile)
		return fmt.Errorf("reemplazar archivo de sesión: %w", err)
	}

	return nil
}

// LoadSession lee y deserializa la sesión guardada en disco.
func LoadSession(filePath string) (*Session, error) {
	sessionMu.Lock()
	defer sessionMu.Unlock()

	if filePath == "" {
		filePath = defaultSessionPath()
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNoSession
		}
		return nil, fmt.Errorf("leer sesión: %w", err)
	}

	var s Session
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("deserializar sesión: %w", err)
	}

	return &s, nil
}

// ClearSession elimina el archivo de sesión guardado.
func ClearSession(filePath string) error {
	sessionMu.Lock()
	defer sessionMu.Unlock()

	if filePath == "" {
		filePath = defaultSessionPath()
	}

	if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("eliminar sesión: %w", err)
	}
	return nil
}
