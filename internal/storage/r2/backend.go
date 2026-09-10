package r2

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"

	"femucaribe-backup-agent/internal/storage"

	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// Backend implementa storage.Backend para Cloudflare R2 sin acoplamiento a estado ni CLI.
type Backend struct {
	client   *Client
	database string
}

func NewBackend(client *Client, database string) *Backend {
	return &Backend{
		client:   client,
		database: database,
	}
}

func (b *Backend) Name() string {
	return "r2"
}

func (b *Backend) Upload(ctx context.Context, localPath string) error {
	remoteKey := KeyForDatabase(b.database, localPath)
	if err := b.client.Upload(ctx, localPath, remoteKey); err != nil {
		return storage.NewRetryableError(fmt.Errorf("r2: subida fallida: %w", err))
	}
	return nil
}

func (b *Backend) Rotate(ctx context.Context, keep int) error {
	prefix := fmt.Sprintf("%s/", b.database)
	latest, err := b.LatestRemote(ctx)
	if err != nil {
		return storage.NewRetryableError(fmt.Errorf("r2: consultar más reciente para rotación: %w", err))
	}
	if latest == "" {
		return nil
	}
	keepKey := fmt.Sprintf("%s/%s", b.database, latest)
	if _, err := b.client.Rotate(ctx, prefix, keepKey); err != nil {
		return fmt.Errorf("r2: rotación: %w", err)
	}
	return nil
}

func (b *Backend) LatestRemote(ctx context.Context) (string, error) {
	prefix := fmt.Sprintf("%s/", b.database)
	out, err := b.client.api.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket: &b.client.bucket,
		Prefix: &prefix,
	})
	if err != nil {
		return "", storage.NewRetryableError(fmt.Errorf("r2: listar objetos: %w", err))
	}

	var names []string
	for _, item := range out.Contents {
		if item.Key != nil {
			names = append(names, filepath.Base(*item.Key))
		}
	}
	if len(names) == 0 {
		return "", nil
	}
	sort.Strings(names)
	return names[len(names)-1], nil
}
