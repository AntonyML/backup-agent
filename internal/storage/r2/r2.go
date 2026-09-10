package r2

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"femucaribe-backup-agent/internal/secrets"

	"github.com/aws/aws-sdk-go-v2/aws"
	configaws "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// S3API abstrae los métodos de S3 usados para permitir mocks en tests unitarios.
type S3API interface {
	PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	HeadObject(ctx context.Context, params *s3.HeadObjectInput, optFns ...func(*s3.Options)) (*s3.HeadObjectOutput, error)
	ListObjectsV2(ctx context.Context, params *s3.ListObjectsV2Input, optFns ...func(*s3.Options)) (*s3.ListObjectsV2Output, error)
	DeleteObject(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
}

// Client gestiona las operaciones contra Cloudflare R2.
type Client struct {
	api    S3API
	bucket string
}

// New crea un nuevo cliente configurado con credenciales R2.
func New(ctx context.Context, creds secrets.Credentials) (*Client, error) {
	if err := creds.Validate(); err != nil {
		return nil, fmt.Errorf("r2: credenciales inválidas: %w", err)
	}

	cfg, err := configaws.LoadDefaultConfig(ctx,
		configaws.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			creds.AccessKeyID,
			creds.SecretAccessKey,
			"",
		)),
		configaws.WithRegion("auto"),
	)
	if err != nil {
		return nil, fmt.Errorf("r2: inicializar aws config: %w", err)
	}

	s3Client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(creds.Endpoint)
		o.Region = "auto"
	})

	return &Client{
		api:    s3Client,
		bucket: creds.Bucket,
	}, nil
}

// NewWithAPI permite inyectar un S3API (útil para tests unitarios).
func NewWithAPI(api S3API, bucket string) *Client {
	return &Client{
		api:    api,
		bucket: bucket,
	}
}

// KeyForDatabase arma la key del objeto: <database>/<filename>.
func KeyForDatabase(database, filename string) string {
	return fmt.Sprintf("%s/%s", database, filepath.Base(filename))
}

// Upload sube el archivo local al bucket con la key remota especificada y
// verifica inmediatamente que el tamaño del objeto en R2 coincida con el tamaño local.
func (c *Client) Upload(ctx context.Context, localPath, remoteKey string) error {
	fi, err := os.Stat(localPath)
	if err != nil {
		return fmt.Errorf("r2: inspeccionar archivo local %s: %w", localPath, err)
	}
	localSize := fi.Size()

	f, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("r2: abrir archivo local %s: %w", localPath, err)
	}
	defer f.Close()

	putInput := &s3.PutObjectInput{
		Bucket:        aws.String(c.bucket),
		Key:           aws.String(remoteKey),
		Body:          f,
		ContentLength: aws.Int64(localSize),
	}

	if _, err := c.api.PutObject(ctx, putInput); err != nil {
		return fmt.Errorf("r2: subir objeto %s: %w", remoteKey, err)
	}

	// Verificación de integridad por tamaño
	head, err := c.api.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(remoteKey),
	})
	if err != nil {
		return fmt.Errorf("r2: verificar objeto subido %s con HeadObject: %w", remoteKey, err)
	}

	if head.ContentLength == nil {
		return fmt.Errorf("r2: HeadObject devolvió ContentLength nil para %s", remoteKey)
	}

	if *head.ContentLength != localSize {
		return fmt.Errorf("r2: discrepancia de tamaño en %s: local=%d bytes, remoto=%d bytes",
			remoteKey, localSize, *head.ContentLength)
	}

	return nil
}

// Rotate lista todos los objetos bajo el prefijo dado (ej: "CONTABILIDAD/") y borra
// todos salvo keepKey (la copia recién confirmada). Devuelve los eliminados y cualquier error.
func (c *Client) Rotate(ctx context.Context, prefix, keepKey string) ([]string, error) {
	listInput := &s3.ListObjectsV2Input{
		Bucket: aws.String(c.bucket),
		Prefix: aws.String(prefix),
	}

	output, err := c.api.ListObjectsV2(ctx, listInput)
	if err != nil {
		return nil, fmt.Errorf("r2: listar objetos con prefijo %s: %w", prefix, err)
	}

	var deleted []string
	var deleteErrs []error

	for _, item := range output.Contents {
		if item.Key == nil {
			continue
		}
		key := *item.Key
		if key == keepKey {
			continue
		}

		_, err := c.api.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket: aws.String(c.bucket),
			Key:    aws.String(key),
		})
		if err != nil {
			deleteErrs = append(deleteErrs, fmt.Errorf("borrar %s: %w", key, err))
		} else {
			deleted = append(deleted, key)
		}
	}

	if len(deleteErrs) > 0 {
		return deleted, errors.Join(deleteErrs...)
	}

	return deleted, nil
}
