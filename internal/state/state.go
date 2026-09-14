package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"femucaribe-backup-agent/internal/config"
	"femucaribe-backup-agent/internal/events"
)

// ProfileState es el estado persistente de UN perfil: idempotencia diaria,
// último backup y sincronizaciones remotas pendientes, con namespacing por
// perfil dentro de un único state.json (D3).
type ProfileState struct {
	LastRunDate     string            `json:"last_run_date,omitempty"`
	LastBackupFile  string            `json:"last_backup_file,omitempty"`
	SHA256          string            `json:"sha256,omitempty"`
	PendingSync     map[string]bool   `json:"pending_sync,omitempty"`
	LastSyncedFiles map[string]string `json:"last_synced_files,omitempty"`
}

// IsPending reporta si la plataforma tiene una subida pendiente.
func (p *ProfileState) IsPending(platform string) bool {
	if p == nil {
		return false
	}
	return p.PendingSync[platform]
}

// SetPending marca/desmarca una plataforma como pendiente de sincronización.
func (p *ProfileState) SetPending(platform string, pending bool) {
	if p == nil {
		return
	}
	if p.PendingSync == nil {
		p.PendingSync = map[string]bool{}
	}
	if pending {
		p.PendingSync[platform] = true
	} else {
		delete(p.PendingSync, platform)
	}
}

// MarkSynced desmarca la plataforma y registra el último archivo sincronizado.
func (p *ProfileState) MarkSynced(platform, filename string) {
	if p == nil {
		return
	}
	p.SetPending(platform, false)
	if p.LastSyncedFiles == nil {
		p.LastSyncedFiles = map[string]string{}
	}
	p.LastSyncedFiles[platform] = filename
}

// HasPending reporta si hay alguna plataforma con subida pendiente.
func (p *ProfileState) HasPending() bool {
	if p == nil {
		return false
	}
	for _, v := range p.PendingSync {
		if v {
			return true
		}
	}
	return false
}

// RanOn reporta si este perfil corrió en la fecha dada (YYYY-MM-DD).
func (p *ProfileState) RanOn(date string) bool {
	if p == nil {
		return false
	}
	return p.LastRunDate != "" && p.LastRunDate == date
}

// State es el estado persistente del agente (state.json junto al binario).
// Desde el rediseño de perfiles, el estado operativo vive namespaced por
// perfil en Profiles; los campos top-level del esquema viejo se aceptan al
// leer (migración) y nunca se escriben.
type State struct {
	Profiles      map[string]*ProfileState `json:"profiles,omitempty"`
	PendingEvents []events.Event           `json:"pending_events,omitempty"`
}

// Profile devuelve el estado del perfil indicado, creándolo si no existe.
func (s *State) Profile(name string) *ProfileState {
	if s == nil {
		return nil
	}
	if s.Profiles == nil {
		s.Profiles = map[string]*ProfileState{}
	}
	p, ok := s.Profiles[name]
	if !ok || p == nil {
		p = &ProfileState{}
		s.Profiles[name] = p
	}
	return p
}

// ProfileIfExists devuelve el estado del perfil sin crearlo.
func (s *State) ProfileIfExists(name string) (*ProfileState, bool) {
	if s == nil || s.Profiles == nil {
		return nil, false
	}
	p, ok := s.Profiles[name]
	return p, ok && p != nil
}

// legacyShim representa el esquema viejo de state.json (pre-perfiles) para la
// migración de lectura. PendingSync viejo era un struct fijo {r2, server}; el
// nuevo es map[string]bool con claves "cloudflare"/"remote_server" (D4).
type legacyShim struct {
	LastRunDate          string                   `json:"last_run_date"`
	LastBackupFile       string                   `json:"last_backup_file"`
	SHA256               string                   `json:"sha256"`
	PendingSync          json.RawMessage          `json:"pending_sync"`
	R2LastSyncedFile     string                   `json:"r2_last_synced_file"`
	ServerLastSyncedFile string                   `json:"server_last_synced_file"`
	Profiles             map[string]*ProfileState `json:"profiles"`
	PendingEvents        []events.Event           `json:"pending_events"`
}

// UnmarshalJSON implementa la migración idempotente de lectura: acepta el
// esquema viejo, mapea r2->cloudflare y server->remote_server, y mueve los
// datos top-level a la sección del perfil inicial (D3/D11). Si ya viene
// namespaced, no toca nada.
func (s *State) UnmarshalJSON(data []byte) error {
	var shim legacyShim
	if err := json.Unmarshal(data, &shim); err != nil {
		return err
	}
	s.Profiles = shim.Profiles
	s.PendingEvents = shim.PendingEvents

	hasLegacy := shim.LastRunDate != "" || shim.LastBackupFile != "" || shim.SHA256 != "" ||
		shim.R2LastSyncedFile != "" || shim.ServerLastSyncedFile != "" ||
		(len(shim.PendingSync) > 0 && string(shim.PendingSync) != "null")
	if hasLegacy {
		p := s.Profile(config.InitialProfileName)
		if p.LastRunDate == "" {
			p.LastRunDate = shim.LastRunDate
		}
		if p.LastBackupFile == "" {
			p.LastBackupFile = shim.LastBackupFile
		}
		if p.SHA256 == "" {
			p.SHA256 = shim.SHA256
		}
		if len(shim.PendingSync) > 0 && string(shim.PendingSync) != "null" {
			var old struct {
				R2     bool `json:"r2"`
				Server bool `json:"server"`
			}
			if err := json.Unmarshal(shim.PendingSync, &old); err == nil && (old.R2 || old.Server) {
				if old.R2 && !p.IsPending(config.PlatformCloudflare) {
					p.SetPending(config.PlatformCloudflare, true)
				}
				if old.Server && !p.IsPending(config.PlatformServer) {
					p.SetPending(config.PlatformServer, true)
				}
			} else {
				var m map[string]bool
				if err := json.Unmarshal(shim.PendingSync, &m); err == nil {
					for k, v := range m {
						if v && !p.IsPending(k) {
							p.SetPending(k, true)
						}
					}
				}
			}
		}
		if p.LastSyncedFiles == nil {
			p.LastSyncedFiles = map[string]string{}
		}
		if shim.R2LastSyncedFile != "" && p.LastSyncedFiles[config.PlatformCloudflare] == "" {
			p.LastSyncedFiles[config.PlatformCloudflare] = shim.R2LastSyncedFile
		}
		if shim.ServerLastSyncedFile != "" && p.LastSyncedFiles[config.PlatformServer] == "" {
			p.LastSyncedFiles[config.PlatformServer] = shim.ServerLastSyncedFile
		}
	}
	return nil
}

// AddPendingEvent registra un evento pendiente evitando duplicados por EventID y limitando el buffer.
func (s *State) AddPendingEvent(evt events.Event) {
	if s == nil {
		return
	}
	for _, e := range s.PendingEvents {
		if e.EventID == evt.EventID {
			return
		}
	}
	if len(s.PendingEvents) >= 100 {
		s.PendingEvents = s.PendingEvents[1:]
	}
	s.PendingEvents = append(s.PendingEvents, evt)
}

// ClearPendingEvents vacía la cola de eventos pendientes.
func (s *State) ClearPendingEvents() {
	if s != nil {
		s.PendingEvents = nil
	}
}

// dateLayout es el formato canónico de LastRunDate.
const dateLayout = "2006-01-02"

// Today devuelve la fecha de hoy en formato YYYY-MM-DD (hora local,
// que es lo que usa Task Scheduler).
func Today() string {
	return time.Now().Format(dateLayout)
}

// RanOn reporta si el perfil indicado corrió en la fecha dada (YYYY-MM-DD).
// Compatibilidad con el esquema previo: delega en la sección del perfil.
func (s *State) RanOn(profile, date string) bool {
	p, ok := s.ProfileIfExists(profile)
	if !ok {
		return false
	}
	return p.RanOn(date)
}

// Load lee state.json. Si no existe, devuelve un State vacío y nil
// (primer arranque: "nunca se corrió", no es error).
// Si está corrupto, devuelve State vacío + error (el llamador debe
// loguearlo y tratarlo como "nunca se corrió", nunca crashear).
func Load(path string) (*State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &State{}, nil
		}
		return &State{}, fmt.Errorf("state: leer %s: %w", path, err)
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return &State{}, fmt.Errorf("state: %s corrupto: %w", path, err)
	}
	return &s, nil
}

// Save escribe state.json de forma atómica: escribe a un temporal en el
// mismo directorio y luego renombra. En Windows os.Rename no pisa el
// destino, por eso se borra primero el anterior (ventana mínima, proceso
// batch de vida corta con lock file).
func Save(path string, s *State) error {
	if s == nil {
		return fmt.Errorf("state: no se puede guardar un State nil")
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("state: serializar: %w", err)
	}
	data = append(data, '\n')

	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("state: crear dir %s: %w", dir, err)
		}
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("state: escribir temporal: %w", err)
	}
	// os.Rename en Windows falla si el destino existe.
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		_ = os.Remove(tmp)
		return fmt.Errorf("state: reemplazar %s: %w", path, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("state: renombrar a %s: %w", path, err)
	}
	return nil
}
