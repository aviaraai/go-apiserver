package storage

import (
	"context"
	"fmt"
	cfg "go-api-server/internal/config"
	"io"
	"time"

	"google.golang.org/api/option"

	"cloud.google.com/go/storage"
)

type Storage interface {
	Upload(context.Context, io.Reader, string, string) error
	PresignedURL(context.Context, string) (string, error)
}

type GCSStore struct {
	client *storage.Client
	bucket string
}

func NewGCSStore(c *cfg.Config) (*GCSStore, error) {
	client, err := storage.NewClient(context.Background(), option.WithAuthCredentialsFile(option.ServiceAccount, c.GCSCredsPath))
	if err != nil {
		return nil, fmt.Errorf("gcs client: %w", err)
	}
	return &GCSStore{client: client, bucket: c.GCSBucket}, nil
}

func (s *GCSStore) Upload(ctx context.Context, file io.Reader, key string, contentType string) error {
	w := s.client.Bucket(s.bucket).Object(key).NewWriter(ctx)
	w.ContentType = contentType

	if _, err := io.Copy(w, file); err != nil {
		_ = w.Close()
		return fmt.Errorf("upload object %q: %w", key, err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("upload object %q close: %w", key, err)
	}
	return nil
}

func (s *GCSStore) PresignedURL(ctx context.Context, key string) (string, error) {
	opts := &storage.SignedURLOptions{
		Method:  "GET",
		Expires: time.Now().Add(15 * time.Minute),
		Scheme:  storage.SigningSchemeV4,
	}

	url, err := s.client.Bucket(s.bucket).SignedURL(key, opts)
	if err != nil {
		return "", fmt.Errorf("presign object %q: %w", key, err)
	}
	return url, nil
}
