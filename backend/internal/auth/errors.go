// Package auth implements local authentication for Sentinel NVR (Phase 7, CG6).
// It provides bcrypt password hashing, AES-256-GCM credential encryption,
// HS256 JWT access tokens, and server-side refresh token management.
package auth

import "errors"

var (
	// ErrNotFound is returned when a requested user or token does not exist.
	ErrNotFound = errors.New("not found")

	// ErrInvalidCredentials is returned when username/password are incorrect.
	ErrInvalidCredentials = errors.New("invalid credentials")

	// ErrTokenExpired is returned when a refresh token is past its expiry time.
	ErrTokenExpired = errors.New("token expired")

	// ErrTokenInvalid is returned when a JWT or refresh token fails validation.
	ErrTokenInvalid = errors.New("token invalid")

	// ErrSetupAlreadyDone is returned when POST /setup is called but users already exist.
	ErrSetupAlreadyDone = errors.New("setup already completed")
)
