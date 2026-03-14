package auth

import (
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestValidateAccessToken_Success(t *testing.T) {
	secret := []byte("test-jwt-secret")
	svc := &Service{jwtSecret: secret}

	now := time.Now()
	tokenStr, err := jwt.NewWithClaims(jwt.SigningMethodHS256, &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "7",
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
		},
		UserID:   7,
		Username: "alice",
		Role:     "admin",
	}).SignedString(secret)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	got, err := svc.ValidateAccessToken(tokenStr)
	if err != nil {
		t.Fatalf("ValidateAccessToken: %v", err)
	}
	if got.UserID != 7 || got.Username != "alice" || got.Role != "admin" {
		t.Fatalf("unexpected claims: %+v", got)
	}
}

func TestValidateAccessToken_Expired(t *testing.T) {
	secret := []byte("test-jwt-secret")
	svc := &Service{jwtSecret: secret}

	now := time.Now()
	tokenStr, err := jwt.NewWithClaims(jwt.SigningMethodHS256, &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "9",
			IssuedAt:  jwt.NewNumericDate(now.Add(-2 * time.Hour)),
			ExpiresAt: jwt.NewNumericDate(now.Add(-time.Hour)),
		},
		UserID: 9,
	}).SignedString(secret)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	_, err = svc.ValidateAccessToken(tokenStr)
	if !errors.Is(err, ErrTokenExpired) {
		t.Fatalf("expected ErrTokenExpired, got %v", err)
	}
}

func TestValidateAccessToken_SubjectMismatch(t *testing.T) {
	secret := []byte("test-jwt-secret")
	svc := &Service{jwtSecret: secret}

	now := time.Now()
	tokenStr, err := jwt.NewWithClaims(jwt.SigningMethodHS256, &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "8", // mismatch with UserID below
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
		},
		UserID: 7,
	}).SignedString(secret)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	_, err = svc.ValidateAccessToken(tokenStr)
	if !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("expected ErrTokenInvalid, got %v", err)
	}
}

func TestValidateAccessToken_Malformed(t *testing.T) {
	svc := &Service{jwtSecret: []byte("test-jwt-secret")}

	_, err := svc.ValidateAccessToken("not-a-jwt")
	if !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("expected ErrTokenInvalid, got %v", err)
	}
}

