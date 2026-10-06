package security

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"

	"golang.org/x/crypto/pbkdf2"
)

const (
	passwordSaltBytes  = 16
	passwordKeyBytes   = 32
	passwordIterations = 600_000
)

var ErrEmptyPassword = errors.New("password cannot be empty")

// HashPassword creates a salted PBKDF2-HMAC-SHA256 password verifier.
func HashPassword(password string) (saltHex, hashHex string, err error) {
	if password == "" {
		return "", "", ErrEmptyPassword
	}
	salt := make([]byte, passwordSaltBytes)
	if _, err := rand.Read(salt); err != nil {
		return "", "", err
	}
	hash := pbkdf2.Key([]byte(password), salt, passwordIterations, passwordKeyBytes, sha256.New)
	return hex.EncodeToString(salt), hex.EncodeToString(hash), nil
}

// VerifyPassword checks a password against a hex-encoded salt and verifier.
func VerifyPassword(password, saltHex, hashHex string) bool {
	if password == "" {
		return false
	}
	salt, err := hex.DecodeString(saltHex)
	if err != nil || len(salt) != passwordSaltBytes {
		return false
	}
	want, err := hex.DecodeString(hashHex)
	if err != nil || len(want) != passwordKeyBytes {
		return false
	}
	got := pbkdf2.Key([]byte(password), salt, passwordIterations, passwordKeyBytes, sha256.New)
	return subtle.ConstantTimeCompare(got, want) == 1
}
