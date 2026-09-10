package r2

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"femucaribe-backup-agent/internal/storage"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

func TestR2Backend_Upload_RetryableError(t *testing.T) {
	tmpDir := t.TempDir()
	localFile := filepath.Join(tmpDir, "test.bak")
	if err := os.WriteFile(localFile, []byte("contenido"), 0o644); err != nil {
		t.Fatal(err)
	}

	mock := &mockS3{
		putObjectFunc: func(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
			return nil, errors.New("falla de conexión R2")
		},
	}

	client := NewWithAPI(mock, "test-bucket")
	backend := NewBackend(client, "CONTABILIDAD")

	if backend.Name() != "r2" {
		t.Errorf("esperaba 'r2', dio '%s'", backend.Name())
	}

	err := backend.Upload(context.Background(), localFile)
	if err == nil {
		t.Fatal("Upload debería fallar")
	}

	var retryErr *storage.RetryableError
	if !errors.As(err, &retryErr) {
		t.Errorf("esperaba un RetryableError, dio %T: %v", err, err)
	}
}

func TestR2Backend_LatestRemote(t *testing.T) {
	mock := &mockS3{
		listObjectsFunc: func(ctx context.Context, params *s3.ListObjectsV2Input, optFns ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
			return &s3.ListObjectsV2Output{
				Contents: []s3types.Object{
					{Key: aws.String("CONTABILIDAD/CONTABILIDAD_20260101_1000.bak")},
					{Key: aws.String("CONTABILIDAD/CONTABILIDAD_20260103_1000.bak")},
					{Key: aws.String("CONTABILIDAD/CONTABILIDAD_20260102_1000.bak")},
				},
			}, nil
		},
	}

	client := NewWithAPI(mock, "test-bucket")
	backend := NewBackend(client, "CONTABILIDAD")

	latest, err := backend.LatestRemote(context.Background())
	if err != nil {
		t.Fatalf("LatestRemote falló: %v", err)
	}
	if latest != "CONTABILIDAD_20260103_1000.bak" {
		t.Errorf("esperaba CONTABILIDAD_20260103_1000.bak, dio %s", latest)
	}
}
