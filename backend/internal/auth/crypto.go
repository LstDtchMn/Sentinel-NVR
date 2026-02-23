package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

const (
	// encPrefix identifies a ciphertext value stored with AES-256-GCM encryption.
	// On read, values starting with this prefix are decrypted; others are returned as-is.
	encPrefix = "enc:"

	// bcryptCost is the bcrypt work factor. 12 is a reasonable default for 2024+
	// hardware — takes ~300ms on a modern CPU (adequate brute-force resistance).
	bcryptCost = 12
)

// GenerateKey generates a cryptographically random 32-byte key suitable for
// AES-256 encryption or HS256 JWT signing.
func GenerateKey() ([]byte, error) {
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("generating key: %w", err)
	}
	return key, nil
}

// GenerateToken generates a cryptographically random 32-byte token (64-char hex).
// Used for refresh tokens stored in the database and sent as httpOnly cookies.
func GenerateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", fmt.Errorf("generating token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// EncryptCredential encrypts plaintext using AES-256-GCM and returns a string
// prefixed with "enc:" followed by the base64-encoded nonce+ciphertext.
// Returns the plaintext unchanged if key is nil (graceful degradation when
// encryption has not yet been initialized).
func EncryptCredential(plaintext string, key []byte) (string, error) {
	if len(key) == 0 || plaintext == "" {
		return plaintext, nil
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("creating AES cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("creating GCM: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generating nonce: %w", err)
	}
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return encPrefix + base64.StdEncoding.EncodeToString(ciphertext), nil
}

// DecryptCredential decrypts a value encrypted by EncryptCredential.
// If the value does not start with "enc:", it is returned unchanged — this
// allows transparent reads of legacy unencrypted credentials.
func DecryptCredential(value string, key []byte) (string, error) {
	if !strings.HasPrefix(value, encPrefix) {
		return value, nil // plaintext (pre-Phase-7 or encryption not set)
	}
	if len(key) == 0 {
		return "", fmt.Errorf("credential is encrypted but no key is available")
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(value, encPrefix))
	if err != nil {
		return "", fmt.Errorf("decoding credential: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("creating AES cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("creating GCM: %w", err)
	}
	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}
	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decrypting credential: %w", err)
	}
	return string(plaintext), nil
}

// HashPassword returns a bcrypt hash of password using bcryptCost work factor.
func HashPassword(password string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return "", fmt.Errorf("hashing password: %w", err)
	}
	return string(h), nil
}

// VerifyPassword checks whether password matches the bcrypt hash.
// Returns nil on match, ErrInvalidCredentials otherwise.
func VerifyPassword(password, hash string) error {
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		return ErrInvalidCredentials
	}
	return nil
}
