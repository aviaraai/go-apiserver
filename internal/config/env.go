package config

import (
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	// Application port
	Port int

	// Postgres database credentials
	DBHost, DBPort, DBName, DBUser, DBPassword, DBSchema, SSLMode string

	// GCS Object Storage credentials
	GCSBucket, GCSCredsPath string

	// R2 Object Storage credentials
	AccessKeyID, AccessKeySecret, AccountID, R2Bucket string

	// Supabase JWT Key
	JWTSecret []byte
	// Admin Key for admin access
	AdminAPIKey []byte

	// Inference Server URL
	InferenceServerURL string

	// QR code encryption key
	QREncryptionKey []byte
}

func LoadEnvFile(path string) error {
	if path == "" {
		path = filepath.Join("secrets", ".env")
	}
	if err := godotenv.Load(path); err != nil {
		return err
	}
	return nil
}

func Load() (*Config, error) {
	cfg := &Config{}
	var errs []string

	cfg.Port = mustAtoi("PORT", &errs)

	cfg.DBHost = mustGet("DB_HOST", &errs)
	cfg.DBPort = mustGet("DB_PORT", &errs)
	cfg.DBName = mustGet("DB_DATABASE", &errs)
	cfg.DBUser = mustGet("DB_USER", &errs)
	cfg.DBPassword = mustGet("DB_PASSWORD", &errs)
	cfg.DBSchema = mustGet("DB_SCHEMA", &errs)
	cfg.SSLMode = mustGet("SSL_MODE", &errs)

	cfg.JWTSecret = mustGetBytes("SUPABASE_JWT_SECRET", 32, &errs)
	cfg.AdminAPIKey = mustGetBytes("ADMIN_API_KEY", 32, &errs)

	cfg.GCSBucket = mustGet("GCS_BUCKET", &errs)
	cfg.GCSCredsPath = mustGetFile("GCS_CREDENTIALS_PATH", &errs)

	cfg.InferenceServerURL = mustGet("INFERENCE_SERVER_URL", &errs)

	cfg.QREncryptionKey = mustGetHexBytes("QR_ENCRYPTION_KEY", 32, &errs)

	if len(errs) > 0 {
		return nil, fmt.Errorf("invalid configuration:\n%s", strings.Join(errs, "\n"))
	}
	return cfg, nil
}

func mustGet(key string, errs *[]string) string {
	v := os.Getenv(key)
	if v == "" {
		*errs = append(*errs, fmt.Sprintf("%s is required", key))
	}
	return v
}

func mustGetBytes(key string, minBytes int, errs *[]string) []byte {
	v := os.Getenv(key)
	if v == "" {
		*errs = append(*errs, fmt.Sprintf("%s is required", key))
		return nil
	}
	b := []byte(v)
	if len(b) < minBytes {
		*errs = append(*errs, fmt.Sprintf("%s must be at least %d bytes, got %d", key, minBytes, len(b)))
		return nil
	}
	return b
}

func mustGetHexBytes(key string, length int, errs *[]string) []byte {
	v := os.Getenv(key)
	if v == "" {
		*errs = append(*errs, fmt.Sprintf("%s is required", key))
		return nil
	}
	qrKey, err := hex.DecodeString(v)
	if err != nil {
		*errs = append(*errs, fmt.Sprintf("%s must be a valid hex string", key))
		return nil
	}
	if len(qrKey) != length {
		*errs = append(*errs, fmt.Sprintf("%s must be %d bytes, got %d", key, length, len(qrKey)))
		return nil
	}
	return qrKey
}

func mustGetFile(key string, errs *[]string) string {
	v := os.Getenv(key)
	if v == "" {
		*errs = append(*errs, fmt.Sprintf("%s is required", key))
		return v
	}
	_, err := os.Stat(v)
	if errors.Is(err, os.ErrNotExist) {
		*errs = append(*errs, fmt.Sprintf("%s json file does not exist", key))
	} else if err != nil {
		*errs = append(*errs, fmt.Sprintf("error accessing %s file: %v", key, err))
	}
	return v
}

func mustAtoi(key string, errs *[]string) int {
	s := os.Getenv(key)
	if s == "" {
		*errs = append(*errs, fmt.Sprintf("%s is required", key))
		return 0
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		*errs = append(*errs, fmt.Sprintf("%s must be an integer", key))
		return 0
	}
	return n
}
