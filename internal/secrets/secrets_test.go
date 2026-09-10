package secrets

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDPAPI_RoundTrip(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("DPAPI solo corre en Windows")
	}

	original := []byte("mi-super-secreto-12345!@#$%^&*()")
	enc, err := Protect(original)
	if err != nil {
		t.Fatalf("Protect falló: %v", err)
	}
	if bytes.Equal(enc, original) {
		t.Fatalf("El texto cifrado no debería ser igual al original")
	}

	dec, err := Unprotect(enc)
	if err != nil {
		t.Fatalf("Unprotect falló: %v", err)
	}
	if !bytes.Equal(dec, original) {
		t.Fatalf("Descifrado incorrecto: obtuve %s, esperaba %s", string(dec), string(original))
	}
}

func TestSaveLoad_RoundTrip(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("DPAPI solo corre en Windows")
	}

	tmpDir := t.TempDir()
	datPath := filepath.Join(tmpDir, "config.dat")

	creds := Credentials{
		Endpoint:        "https://test-account.r2.cloudflarestorage.com",
		Bucket:          "femucaribe-backups",
		AccessKeyID:     "AKIAIOSFODNN7EXAMPLE",
		SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
	}

	if err := Save(datPath, creds); err != nil {
		t.Fatalf("Save falló: %v", err)
	}

	rawDisk, err := os.ReadFile(datPath)
	if err != nil {
		t.Fatalf("leer archivo guardado: %v", err)
	}

	// Verificar que NO haya texto plano en disco
	if strings.Contains(string(rawDisk), creds.SecretAccessKey) {
		t.Fatalf("El archivo en disco contiene la secret key en texto plano!")
	}
	if strings.Contains(string(rawDisk), creds.AccessKeyID) {
		t.Fatalf("El archivo en disco contiene la access key en texto plano!")
	}

	loaded, err := Load(datPath)
	if err != nil {
		t.Fatalf("Load falló: %v", err)
	}

	if *loaded != creds {
		t.Fatalf("Credenciales cargadas difieren:\nobtuve: %+v\nesperaba: %+v", loaded, creds)
	}
}

func TestLoad_MissingFile(t *testing.T) {
	tmpDir := t.TempDir()
	datPath := filepath.Join(tmpDir, "inexistente.dat")

	_, err := Load(datPath)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("esperaba ErrNotFound, obtuve: %v", err)
	}
}

func TestLoad_CorruptFile(t *testing.T) {
	tmpDir := t.TempDir()
	datPath := filepath.Join(tmpDir, "config.dat")

	if err := os.WriteFile(datPath, []byte("no es un json valido"), 0o600); err != nil {
		t.Fatalf("escribir corrupto: %v", err)
	}

	_, err := Load(datPath)
	if !errors.Is(err, ErrCorrupt) {
		t.Fatalf("esperaba ErrCorrupt, obtuve: %v", err)
	}
}

func TestConfigure_Interactive(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("DPAPI solo corre en Windows")
	}

	tmpDir := t.TempDir()
	datPath := filepath.Join(tmpDir, "config.dat")

	input := "https://cf.r2.cloudflarestorage.com\nmi-bucket\nACCESSO123\nSECRETO456\n"
	in := strings.NewReader(input)
	out := &bytes.Buffer{}

	if err := Configure(datPath, in, out); err != nil {
		t.Fatalf("Configure falló: %v", err)
	}

	loaded, err := Load(datPath)
	if err != nil {
		t.Fatalf("Load tras Configure falló: %v", err)
	}

	if loaded.Endpoint != "https://cf.r2.cloudflarestorage.com" {
		t.Errorf("endpoint incorrecto: %s", loaded.Endpoint)
	}
	if loaded.Bucket != "mi-bucket" {
		t.Errorf("bucket incorrecto: %s", loaded.Bucket)
	}
	if loaded.AccessKeyID != "ACCESSO123" {
		t.Errorf("access key incorrecta: %s", loaded.AccessKeyID)
	}
	if loaded.SecretAccessKey != "SECRETO456" {
		t.Errorf("secret key incorrecta: %s", loaded.SecretAccessKey)
	}
}
