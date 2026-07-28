package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"io"
)

func Encrypt(key []byte, plaintext string) (string, error) {
	block, err := aes.NewCipher(key) // key must be 16, 24, or 32 bytes (AES-128/192/256)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize()) // 12 bytes, standard for GCM
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	// Seal appends the ciphertext+auth-tag to nonce, giving us one blob
	// that carries everything needed to decrypt and verify later.
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)

	return base64.RawURLEncoding.EncodeToString(ciphertext), nil
}
