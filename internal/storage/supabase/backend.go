package supabase

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"femucaribe-backup-agent/internal/storage"
)

// Backend implementa storage.Backend para Supabase Storage.
type Backend struct {
	client   *Client
	bucket   string
	database string
}

// NewBackend instancia un Backend de Supabase Storage.
func NewBackend(client *Client, bucket, database string) *Backend {
	return &Backend{
		client:   client,
		bucket:   bucket,
		database: database,
	}
}

// Name retorna el identificador canónico del backend.
func (b *Backend) Name() string {
	return "supabase"
}

// Bucket retorna el nombre del bucket configurado.
func (b *Backend) Bucket() string {
	return b.bucket
}

// Upload sube el archivo local al bucket bajo el prefijo {database}/{filename}.
func (b *Backend) Upload(ctx context.Context, localPath string) error {
	filename := filepath.Base(localPath)
	objectPath := fmt.Sprintf("%s/%s", b.database, filename)

	if err := b.client.EnsureBucket(ctx, b.bucket); err != nil {
		return err
	}

	return b.client.Upload(ctx, b.bucket, objectPath, localPath)
}

// Rotate elimina los backups remotos excedentes conservando los últimos `keep`.
func (b *Backend) Rotate(ctx context.Context, keep int) error {
	if keep <= 0 {
		return nil
	}

	items, err := b.client.List(ctx, b.bucket, b.database)
	if err != nil {
		return fmt.Errorf("supabase storage: rotación: %w", err)
	}

	var bakNames []string
	for _, item := range items {
		cleanName := filepath.Base(item.Name)
		if strings.HasSuffix(strings.ToLower(cleanName), ".bak") {
			bakNames = append(bakNames, cleanName)
		}
	}

	if len(bakNames) <= keep {
		return nil
	}

	sort.Strings(bakNames)
	excessCount := len(bakNames) - keep
	toDelete := make([]string, 0, excessCount)
	for i := 0; i < excessCount; i++ {
		toDelete = append(toDelete, fmt.Sprintf("%s/%s", b.database, bakNames[i]))
	}

	if err := b.client.Delete(ctx, b.bucket, toDelete); err != nil {
		return fmt.Errorf("supabase storage: rotación eliminando excedentes: %w", err)
	}

	return nil
}

// LatestRemote localiza el archivo .bak más reciente en el bucket para esta base de datos.
func (b *Backend) LatestRemote(ctx context.Context) (string, error) {
	items, err := b.client.List(ctx, b.bucket, b.database)
	if err != nil {
		return "", storage.NewRetryableError(fmt.Errorf("supabase storage: consultar más reciente: %w", err))
	}

	var bakNames []string
	for _, item := range items {
		cleanName := filepath.Base(item.Name)
		if strings.HasSuffix(strings.ToLower(cleanName), ".bak") {
			bakNames = append(bakNames, cleanName)
		}
	}

	if len(bakNames) == 0 {
		return "", nil
	}

	sort.Strings(bakNames)
	return bakNames[len(bakNames)-1], nil
}
