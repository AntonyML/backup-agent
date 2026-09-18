package portability

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"femucaribe-backup-agent/internal/config"
	"femucaribe-backup-agent/internal/secrets"
	"femucaribe-backup-agent/internal/version"
	"golang.org/x/crypto/pbkdf2"
)

const (
	EnvelopeMagic   = "BACFG"
	EnvelopeVersion = 1
	pbkdf2Iters     = 100_000
	keyLen          = 32
	saltLen         = 16
)

var (
	ErrInvalidPassword = errors.New("contraseña incorrecta o archivo dañado")
	ErrEmptyPassword   = errors.New("la contraseña no puede estar vacía")
	ErrShortPassword   = errors.New("la contraseña debe tener al menos 4 caracteres")
	ErrInvalidFormat   = errors.New("formato de archivo de exportación inválido")
)

// Bundle representa el conjunto portable de datos de configuración y secretos.
type Bundle struct {
	Version     string               `json:"version"`
	CreatedAt   string               `json:"created_at"`
	Config      config.Config        `json:"config"`
	Credentials *secrets.Credentials `json:"credentials,omitempty"`
}

// Envelope es el contenedor serializado en disco con cifrado AES-256-GCM.
type Envelope struct {
	Magic      string `json:"magic"`
	Version    int    `json:"version"`
	Salt       string `json:"salt"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

// Export genera un archivo cifrado con AES-256-GCM conteniendo la configuración y secretos.
func Export(configPath, datPath, outputPath, password string) error {
	password = strings.TrimSpace(password)
	if password == "" {
		return ErrEmptyPassword
	}
	if len(password) < 4 {
		return ErrShortPassword
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("cargar configuración (%s): %w", configPath, err)
	}

	bundle := Bundle{
		Version:   version.Current,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		Config:    cfg,
	}

	// Si existen credenciales de R2 en config.dat, descifrarlas en memoria e incluirlas en el bundle
	if datPath != "" {
		creds, err := secrets.Load(datPath)
		if err == nil && creds != nil {
			bundle.Credentials = creds
		}
	}

	plaintext, err := json.Marshal(bundle)
	if err != nil {
		return fmt.Errorf("serializar bundle: %w", err)
	}

	salt := make([]byte, saltLen)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return fmt.Errorf("generar salt: %w", err)
	}

	key := pbkdf2.Key([]byte(password), salt, pbkdf2Iters, keyLen, sha256.New)

	block, err := aes.NewCipher(key)
	if err != nil {
		return fmt.Errorf("crear cifrador AES: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return fmt.Errorf("crear modo GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return fmt.Errorf("generar nonce: %w", err)
	}

	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)

	env := Envelope{
		Magic:      EnvelopeMagic,
		Version:    EnvelopeVersion,
		Salt:       base64.StdEncoding.EncodeToString(salt),
		Nonce:      base64.StdEncoding.EncodeToString(nonce),
		Ciphertext: base64.StdEncoding.EncodeToString(ciphertext),
	}

	outData, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return fmt.Errorf("serializar contenedor: %w", err)
	}
	outData = append(outData, '\n')

	outDir := filepath.Dir(outputPath)
	if outDir != "" && outDir != "." {
		if err := os.MkdirAll(outDir, 0o755); err != nil {
			return fmt.Errorf("crear directorio destino %s: %w", outDir, err)
		}
	}

	tmp := outputPath + ".tmp"
	if err := os.WriteFile(tmp, outData, 0o600); err != nil {
		return fmt.Errorf("escribir temporal %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, outputPath); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("guardar archivo exportado %s: %w", outputPath, err)
	}

	return nil
}

// Import lee un archivo cifrado, valida la contraseña y restaura config.json y config.dat.
func Import(inputPath, password, targetConfigPath, targetDatPath string) (*config.Config, error) {
	password = strings.TrimSpace(password)
	if password == "" {
		return nil, ErrEmptyPassword
	}

	data, err := os.ReadFile(inputPath)
	if err != nil {
		return nil, fmt.Errorf("leer archivo %s: %w", inputPath, err)
	}

	var env Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("%w: json corrupto: %v", ErrInvalidFormat, err)
	}

	if env.Magic != EnvelopeMagic || env.Version != EnvelopeVersion {
		return nil, fmt.Errorf("%w: encabezado no reconocido", ErrInvalidFormat)
	}

	salt, err := base64.StdEncoding.DecodeString(env.Salt)
	if err != nil || len(salt) != saltLen {
		return nil, fmt.Errorf("%w: salt inválido", ErrInvalidFormat)
	}

	nonce, err := base64.StdEncoding.DecodeString(env.Nonce)
	if err != nil {
		return nil, fmt.Errorf("%w: nonce inválido", ErrInvalidFormat)
	}

	ciphertext, err := base64.StdEncoding.DecodeString(env.Ciphertext)
	if err != nil {
		return nil, fmt.Errorf("%w: payload cifrado inválido", ErrInvalidFormat)
	}

	key := pbkdf2.Key([]byte(password), salt, pbkdf2Iters, keyLen, sha256.New)

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("inicializar AES: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("inicializar GCM: %w", err)
	}

	if len(nonce) != gcm.NonceSize() {
		return nil, fmt.Errorf("%w: tamaño de nonce incorrecto", ErrInvalidFormat)
	}

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, ErrInvalidPassword
	}

	var bundle Bundle
	if err := json.Unmarshal(plaintext, &bundle); err != nil {
		return nil, fmt.Errorf("%w: bundle corrupto: %v", ErrInvalidFormat, err)
	}

	// 1. Guardar la configuración en targetConfigPath
	if err := config.Save(targetConfigPath, bundle.Config); err != nil {
		return nil, fmt.Errorf("guardar configuración importada: %w", err)
	}

	// 2. Si venían credenciales de Cloudflare R2, re-cifrarlas con el DPAPI de la máquina actual
	if bundle.Credentials != nil && targetDatPath != "" {
		if err := secrets.Save(targetDatPath, *bundle.Credentials); err != nil {
			return nil, fmt.Errorf("re-cifrar credenciales con DPAPI local: %w", err)
		}
	}

	return &bundle.Config, nil
}
