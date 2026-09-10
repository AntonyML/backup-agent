package secrets

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var (
	ErrNotFound = errors.New("config.dat no encontrado")
	ErrCorrupt  = errors.New("config.dat corrupto o no se puede descifrar")
)

type Credentials struct {
	Endpoint        string `json:"endpoint"`
	Bucket          string `json:"bucket"`
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
}

func (c Credentials) Validate() error {
	if strings.TrimSpace(c.Endpoint) == "" {
		return errors.New("endpoint de R2 no puede estar vacío")
	}
	if strings.TrimSpace(c.Bucket) == "" {
		return errors.New("bucket de R2 no puede estar vacío")
	}
	if strings.TrimSpace(c.AccessKeyID) == "" {
		return errors.New("access key ID de R2 no puede estar vacío")
	}
	if strings.TrimSpace(c.SecretAccessKey) == "" {
		return errors.New("secret access key de R2 no puede estar vacío")
	}
	return nil
}

type encryptedConfig struct {
	Endpoint        string `json:"endpoint"`
	Bucket          string `json:"bucket"`
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
}

// Save cifra cada campo con DPAPI, serializa en JSON con blobs codificados en base64
// y lo escribe en path con rename atómico.
func Save(path string, creds Credentials) error {
	if err := creds.Validate(); err != nil {
		return fmt.Errorf("validar credenciales: %w", err)
	}

	encEndpoint, err := Protect([]byte(creds.Endpoint))
	if err != nil {
		return fmt.Errorf("cifrar endpoint: %w", err)
	}
	encBucket, err := Protect([]byte(creds.Bucket))
	if err != nil {
		return fmt.Errorf("cifrar bucket: %w", err)
	}
	encAccessKey, err := Protect([]byte(creds.AccessKeyID))
	if err != nil {
		return fmt.Errorf("cifrar access key: %w", err)
	}
	encSecretKey, err := Protect([]byte(creds.SecretAccessKey))
	if err != nil {
		return fmt.Errorf("cifrar secret key: %w", err)
	}

	raw := encryptedConfig{
		Endpoint:        base64.StdEncoding.EncodeToString(encEndpoint),
		Bucket:          base64.StdEncoding.EncodeToString(encBucket),
		AccessKeyID:     base64.StdEncoding.EncodeToString(encAccessKey),
		SecretAccessKey: base64.StdEncoding.EncodeToString(encSecretKey),
	}

	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return fmt.Errorf("serializar config cifrada: %w", err)
	}
	data = append(data, '\n')

	dir := filepath.Dir(path)
	if dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("crear directorio %s: %w", dir, err)
		}
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("escribir temporal %s: %w", tmp, err)
	}

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		_ = os.Remove(tmp)
		return fmt.Errorf("reemplazar %s: %w", path, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("renombrar %s a %s: %w", tmp, path, err)
	}

	return nil
}

// Load lee y descifra config.dat en memoria.
func Load(path string) (*Credentials, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("leer %s: %w", path, err)
	}

	var raw encryptedConfig
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("%w: json inválido: %v", ErrCorrupt, err)
	}

	decEndpoint, err := decodeAndUnprotect(raw.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("%w: endpoint: %v", ErrCorrupt, err)
	}
	decBucket, err := decodeAndUnprotect(raw.Bucket)
	if err != nil {
		return nil, fmt.Errorf("%w: bucket: %v", ErrCorrupt, err)
	}
	decAccessKey, err := decodeAndUnprotect(raw.AccessKeyID)
	if err != nil {
		return nil, fmt.Errorf("%w: access_key_id: %v", ErrCorrupt, err)
	}
	decSecretKey, err := decodeAndUnprotect(raw.SecretAccessKey)
	if err != nil {
		return nil, fmt.Errorf("%w: secret_access_key: %v", ErrCorrupt, err)
	}

	creds := &Credentials{
		Endpoint:        string(decEndpoint),
		Bucket:          string(decBucket),
		AccessKeyID:     string(decAccessKey),
		SecretAccessKey: string(decSecretKey),
	}

	if err := creds.Validate(); err != nil {
		return nil, fmt.Errorf("%w: datos descifrados inválidos: %v", ErrCorrupt, err)
	}

	return creds, nil
}

func decodeAndUnprotect(encoded string) ([]byte, error) {
	b, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("base64 decode: %w", err)
	}
	return Unprotect(b)
}
