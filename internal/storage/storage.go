package storage

import (
	"context"
	"fmt"
	cfg "go-api-server/internal/config"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"google.golang.org/api/option"

	"cloud.google.com/go/storage"
)

type Storage interface {
	Upload(context.Context, io.Reader, string, string) error
	PresignedURL(context.Context, string) (string, error)
}

type R2Store struct {
	client *s3.Client
	bucket string
}

func NewR2Store(c cfg.Config) (*R2Store, error) {
	accessKeyId := c.AccessKeyID
	accessKeySecret := c.AccessKeySecret
	accountId := c.AccountID

	cfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKeyId, accessKeySecret, "")),
		config.WithRegion("auto"), // Required by SDK but not used by R2
	)
	if err != nil {
		return nil, fmt.Errorf("r2 client: %w", err)
	}

	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(fmt.Sprintf("https://%s.r2.cloudflarestorage.com", accountId))
	})
	return &R2Store{client: client, bucket: c.R2Bucket}, nil
}

func (s *R2Store) Upload(ctx context.Context, file io.Reader, key string, contentType string) error {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		Body:        file,
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return fmt.Errorf("upload object %q: %w", key, err)
	}
	return nil
}

func (s *R2Store) PresignedURL(ctx context.Context, key string) (string, error) {
	presignClient := s3.NewPresignClient(s.client)

	presignResult, err := presignClient.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	},
		func(opts *s3.PresignOptions) {
			opts.Expires = 15 * time.Minute
		},
	)

	if err != nil {
		return "", fmt.Errorf("presign object %q: %w", key, err)
	}

	return presignResult.URL, nil
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
