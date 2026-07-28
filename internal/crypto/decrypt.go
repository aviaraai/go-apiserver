package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"errors"
)

var ErrInvalidToken = errors.New("invalid or tampered token")

func Decrypt(key []byte, token string) (string, error) {
	data, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return "", ErrInvalidToken
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", ErrInvalidToken
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", ErrInvalidToken // authentication failed — tampered or wrong key
	}

	return string(plaintext), nil
}
