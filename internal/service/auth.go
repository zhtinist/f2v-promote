package service

import (
	"fmt"
	"strconv"

	"github.com/gorilla/securecookie"
	"golang.org/x/crypto/bcrypt"
)

// AuthService handles password hashing and session token management.
type AuthService struct {
	sc *securecookie.SecureCookie
}

// NewAuthService creates an AuthService with the given session secret.
func NewAuthService(sessionSecret string) *AuthService {
	hashKey := []byte(sessionSecret)
	// Use first 32 bytes of secret as block key (AES-256); pad if needed.
	blockKey := make([]byte, 32)
	copy(blockKey, []byte(sessionSecret))

	sc := securecookie.New(hashKey, blockKey)
	sc.MaxAge(24 * 60 * 60) // 24 hours
	return &AuthService{sc: sc}
}

// HashPassword hashes a plaintext password with bcrypt cost 10.
func HashPassword(plain string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(plain), 10)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(hash), nil
}

// VerifyPassword checks a plaintext password against a bcrypt hash.
func VerifyPassword(plain, hashed string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hashed), []byte(plain)) == nil
}

// CreateSessionToken encodes the user ID into a signed, encrypted cookie value.
func (a *AuthService) CreateSessionToken(userID int64) (string, error) {
	value := map[string]string{
		"user_id": strconv.FormatInt(userID, 10),
	}
	encoded, err := a.sc.Encode("session", value)
	if err != nil {
		return "", fmt.Errorf("create session token: %w", err)
	}
	return encoded, nil
}

// DecodeSessionToken decodes a session token and returns the user ID.
// Returns an error if the token is invalid or older than 24 hours.
func (a *AuthService) DecodeSessionToken(token string) (string, error) {
	value := make(map[string]string)
	if err := a.sc.Decode("session", token, &value); err != nil {
		return "", fmt.Errorf("decode session token: %w", err)
	}
	userID, ok := value["user_id"]
	if !ok || userID == "" {
		return "", fmt.Errorf("decode session token: missing user_id")
	}
	return userID, nil
}
