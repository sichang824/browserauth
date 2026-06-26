package cookie

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
)

const (
	saltSize     = 16
	gcmNonceSize = 12
)

// Encrypt encrypts plaintext with passphrase and app-specific magic header.
// Format: magic + salt(16) + nonce(12) + ciphertext.
func Encrypt(plaintext, magic, passphrase string) ([]byte, error) {
	if passphrase == "" {
		return nil, errors.New("cookie encryption passphrase must not be empty")
	}
	if len(magic) == 0 {
		return nil, errors.New("cookie magic must not be empty")
	}

	salt := make([]byte, saltSize)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, fmt.Errorf("random salt: %w", err)
	}

	key := deriveKey([]byte(passphrase), salt)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcmNonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("random nonce: %w", err)
	}

	ciphertext := aead.Seal(nil, nonce, []byte(plaintext), nil)
	out := make([]byte, 0, len(magic)+saltSize+gcmNonceSize+len(ciphertext))
	out = append(out, magic...)
	out = append(out, salt...)
	out = append(out, nonce...)
	out = append(out, ciphertext...)
	return out, nil
}

// Decrypt decrypts a blob produced by Encrypt.
func Decrypt(data []byte, magic, passphrase string) (string, error) {
	if passphrase == "" {
		return "", errors.New("cookie decryption passphrase must not be empty")
	}
	if len(data) < len(magic)+saltSize+gcmNonceSize {
		return "", errors.New("cookie file too short")
	}
	if string(data[:len(magic)]) != magic {
		return "", errors.New("cookie file has wrong magic header")
	}

	salt := data[len(magic) : len(magic)+saltSize]
	key := deriveKey([]byte(passphrase), salt)
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := data[len(magic)+saltSize : len(magic)+saltSize+gcmNonceSize]
	ciphertext := data[len(magic)+saltSize+gcmNonceSize:]
	plain, err := aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}
	return string(plain), nil
}

// IsEncrypted reports whether data starts with the expected magic header.
func IsEncrypted(data []byte, magic string) bool {
	return len(data) >= len(magic) && string(data[:len(magic)]) == magic
}

func deriveKey(passphrase, salt []byte) []byte {
	h := sha256.New()
	h.Write(salt)
	h.Write(passphrase)
	return h.Sum(nil)
}
