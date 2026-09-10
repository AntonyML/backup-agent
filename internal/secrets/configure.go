package secrets

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Configure solicita interactivamente los datos de R2 por `in`,
// los valida, los cifra con DPAPI y los guarda en `path`.
func Configure(path string, in io.Reader, out io.Writer) error {
	reader := bufio.NewReader(in)

	fmt.Fprintln(out, "=== Configuración de Cloudflare R2 (Cifrado DPAPI) ===")
	fmt.Fprintln(out, "Las credenciales serán cifradas con DPAPI para el usuario actual y guardadas en config.dat.")
	fmt.Fprintln(out)

	prompt := func(label string) (string, error) {
		fmt.Fprintf(out, "%s: ", label)
		line, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", err
		}
		return strings.TrimSpace(line), nil
	}

	endpoint, err := prompt("Endpoint de R2 (ej: https://<account_id>.r2.cloudflarestorage.com)")
	if err != nil {
		return fmt.Errorf("lectura de endpoint: %w", err)
	}
	bucket, err := prompt("Nombre del Bucket")
	if err != nil {
		return fmt.Errorf("lectura de bucket: %w", err)
	}
	accessKey, err := prompt("R2 Access Key ID")
	if err != nil {
		return fmt.Errorf("lectura de access key: %w", err)
	}
	secretKey, err := prompt("R2 Secret Access Key")
	if err != nil {
		return fmt.Errorf("lectura de secret key: %w", err)
	}

	creds := Credentials{
		Endpoint:        endpoint,
		Bucket:          bucket,
		AccessKeyID:     accessKey,
		SecretAccessKey: secretKey,
	}

	if err := creds.Validate(); err != nil {
		return fmt.Errorf("validación de credenciales: %w", err)
	}

	if err := Save(path, creds); err != nil {
		return fmt.Errorf("guardar config.dat: %w", err)
	}

	fmt.Fprintln(out)
	fmt.Fprintf(out, "Configuración guardada exitosamente en %s\n", path)
	return nil
}
