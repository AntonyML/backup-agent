package r2

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

type mockS3 struct {
	putObjectFunc    func(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	headObjectFunc   func(ctx context.Context, params *s3.HeadObjectInput, optFns ...func(*s3.Options)) (*s3.HeadObjectOutput, error)
	listObjectsFunc  func(ctx context.Context, params *s3.ListObjectsV2Input, optFns ...func(*s3.Options)) (*s3.ListObjectsV2Output, error)
	deleteObjectFunc func(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
}

func (m *mockS3) PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	if m.putObjectFunc != nil {
		return m.putObjectFunc(ctx, params, optFns...)
	}
	return &s3.PutObjectOutput{}, nil
}

func (m *mockS3) HeadObject(ctx context.Context, params *s3.HeadObjectInput, optFns ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
	if m.headObjectFunc != nil {
		return m.headObjectFunc(ctx, params, optFns...)
	}
	return &s3.HeadObjectOutput{}, nil
}

func (m *mockS3) ListObjectsV2(ctx context.Context, params *s3.ListObjectsV2Input, optFns ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	if m.listObjectsFunc != nil {
		return m.listObjectsFunc(ctx, params, optFns...)
	}
	return &s3.ListObjectsV2Output{}, nil
}

func (m *mockS3) DeleteObject(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	if m.deleteObjectFunc != nil {
		return m.deleteObjectFunc(ctx, params, optFns...)
	}
	return &s3.DeleteObjectOutput{}, nil
}

func TestUpload_Success(t *testing.T) {
	tmpDir := t.TempDir()
	localFile := filepath.Join(tmpDir, "test.bak")
	content := []byte("contenido-de-prueba-para-backup")
	if err := os.WriteFile(localFile, content, 0o644); err != nil {
		t.Fatal(err)
	}

	putCalled := false
	headCalled := false

	mock := &mockS3{
		putObjectFunc: func(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
			putCalled = true
			if *params.Bucket != "my-bucket" || *params.Key != "CONTABILIDAD/test.bak" {
				t.Errorf("parámetros PutObject inválidos: bucket=%s key=%s", *params.Bucket, *params.Key)
			}
			if *params.ContentLength != int64(len(content)) {
				t.Errorf("content length esperado %d, dio %d", len(content), *params.ContentLength)
			}
			return &s3.PutObjectOutput{}, nil
		},
		headObjectFunc: func(ctx context.Context, params *s3.HeadObjectInput, optFns ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
			headCalled = true
			return &s3.HeadObjectOutput{
				ContentLength: aws.Int64(int64(len(content))),
			}, nil
		},
	}

	client := NewWithAPI(mock, "my-bucket")
	err := client.Upload(context.Background(), localFile, "CONTABILIDAD/test.bak")
	if err != nil {
		t.Fatalf("Upload no debería fallar: %v", err)
	}
	if !putCalled || !headCalled {
		t.Fatalf("PutObject o HeadObject no fueron llamados: put=%v head=%v", putCalled, headCalled)
	}
}

func TestUpload_SizeMismatch(t *testing.T) {
	tmpDir := t.TempDir()
	localFile := filepath.Join(tmpDir, "test.bak")
	content := []byte("1234567890")
	if err := os.WriteFile(localFile, content, 0o644); err != nil {
		t.Fatal(err)
	}

	mock := &mockS3{
		putObjectFunc: func(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
			return &s3.PutObjectOutput{}, nil
		},
		headObjectFunc: func(ctx context.Context, params *s3.HeadObjectInput, optFns ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
			return &s3.HeadObjectOutput{
				ContentLength: aws.Int64(999), // discrepancia intencional
			}, nil
		},
	}

	client := NewWithAPI(mock, "my-bucket")
	err := client.Upload(context.Background(), localFile, "CONTABILIDAD/test.bak")
	if err == nil {
		t.Fatal("Upload debería fallar ante discrepancia de tamaño")
	}
}

func TestUpload_PutObjectError(t *testing.T) {
	tmpDir := t.TempDir()
	localFile := filepath.Join(tmpDir, "test.bak")
	if err := os.WriteFile(localFile, []byte("abc"), 0o644); err != nil {
		t.Fatal(err)
	}

	mock := &mockS3{
		putObjectFunc: func(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
			return nil, errors.New("timeout de red simulado")
		},
	}

	client := NewWithAPI(mock, "my-bucket")
	err := client.Upload(context.Background(), localFile, "CONTABILIDAD/test.bak")
	if err == nil {
		t.Fatal("Upload debería propagar error de PutObject")
	}
}

func TestRotate_KeepsOnlyLatest(t *testing.T) {
	mock := &mockS3{
		listObjectsFunc: func(ctx context.Context, params *s3.ListObjectsV2Input, optFns ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
			return &s3.ListObjectsV2Output{
				Contents: []s3types.Object{
					{Key: aws.String("CONTABILIDAD/CONTABILIDAD_20260101_1000.bak")},
					{Key: aws.String("CONTABILIDAD/CONTABILIDAD_20260102_1000.bak")},
					{Key: aws.String("CONTABILIDAD/CONTABILIDAD_20260103_1000.bak")},
				},
			}, nil
		},
		deleteObjectFunc: func(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
			if *params.Key == "CONTABILIDAD/CONTABILIDAD_20260103_1000.bak" {
				t.Errorf("No debería borrarse el archivo que se quiere conservar!")
			}
			return &s3.DeleteObjectOutput{}, nil
		},
	}

	client := NewWithAPI(mock, "my-bucket")
	keep := "CONTABILIDAD/CONTABILIDAD_20260103_1000.bak"
	deleted, err := client.Rotate(context.Background(), "CONTABILIDAD/", keep)
	if err != nil {
		t.Fatalf("Rotate falló: %v", err)
	}

	if len(deleted) != 2 {
		t.Fatalf("esperaba 2 archivos borrados, obtuve %d", len(deleted))
	}
	if deleted[0] != "CONTABILIDAD/CONTABILIDAD_20260101_1000.bak" || deleted[1] != "CONTABILIDAD/CONTABILIDAD_20260102_1000.bak" {
		t.Errorf("archivos borrados no coinciden: %+v", deleted)
	}
}

func TestRotate_DeleteError_Partial(t *testing.T) {
	mock := &mockS3{
		listObjectsFunc: func(ctx context.Context, params *s3.ListObjectsV2Input, optFns ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
			return &s3.ListObjectsV2Output{
				Contents: []s3types.Object{
					{Key: aws.String("CONTABILIDAD/old1.bak")},
					{Key: aws.String("CONTABILIDAD/old2.bak")},
					{Key: aws.String("CONTABILIDAD/new.bak")},
				},
			}, nil
		},
		deleteObjectFunc: func(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
			if *params.Key == "CONTABILIDAD/old2.bak" {
				return nil, errors.New("permiso denegado simulado")
			}
			return &s3.DeleteObjectOutput{}, nil
		},
	}

	client := NewWithAPI(mock, "my-bucket")
	deleted, err := client.Rotate(context.Background(), "CONTABILIDAD/", "CONTABILIDAD/new.bak")

	if err == nil {
		t.Fatal("Rotate debería reportar error si falló un borrado")
	}
	if len(deleted) != 1 || deleted[0] != "CONTABILIDAD/old1.bak" {
		t.Errorf("debería listar los que sí se pudieron borrar: %+v", deleted)
	}
}

func TestKeyForDatabase(t *testing.T) {
	got := KeyForDatabase("CONTABILIDAD", `C:\Backups\CONTABILIDAD_20260115_1200.bak`)
	want := "CONTABILIDAD/CONTABILIDAD_20260115_1200.bak"
	if got != want {
		t.Errorf("KeyForDatabase: obtuve %s, esperaba %s", got, want)
	}
}
