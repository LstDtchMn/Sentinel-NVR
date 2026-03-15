package auth

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Claims are the JWT payload fields embedded in every access token.
type Claims struct {
	jwt.RegisteredClaims
	UserID   int    `json:"uid"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

// TokenPair holds a freshly issued access/refresh token pair together with
// their TTLs so the handler can set cookie MaxAge correctly.
type TokenPair struct {
	AccessToken  string
	RefreshToken string
	AccessTTL    time.Duration
	RefreshTTL   time.Duration
}

// Service handles authentication logic: login, refresh, logout, and token validation.
// It also exposes EncryptCredential/DecryptCredential so callers (camera repo) can
// encrypt ONVIF passwords using the same AES-256 key managed here.
type Service struct {
	repo       *Repository
	jwtSecret  []byte
	encKey     []byte
	accessTTL  time.Duration
	refreshTTL time.Duration
}

// New initialises the Service by loading (or generating on first run) the JWT
// secret and credential encryption key from the system_settings table.
func New(ctx context.Context, repo *Repository, accessTTLSeconds, refreshTTLSeconds int) (*Service, error) {
	jwtSecret, err := repo.GetOrGenerateKey(ctx, "jwt_secret")
	if err != nil {
		return nil, fmt.Errorf("auth: loading jwt_secret: %w", err)
	}
	encKey, err := repo.GetOrGenerateKey(ctx, "credential_key")
	if err != nil {
		return nil, fmt.Errorf("auth: loading credential_key: %w", err)
	}

	if accessTTLSeconds <= 0 {
		accessTTLSeconds = 900 // 15 minutes
	}
	if refreshTTLSeconds <= 0 {
		refreshTTLSeconds = 604800 // 7 days
	}

	return &Service{
		repo:       repo,
		jwtSecret:  jwtSecret,
		encKey:     encKey,
		accessTTL:  time.Duration(accessTTLSeconds) * time.Second,
		refreshTTL: time.Duration(refreshTTLSeconds) * time.Second,
	}, nil
}

// Login verifies credentials and returns a new token pair on success.
// Returns ErrInvalidCredentials if the username or password is wrong.
func (s *Service) Login(ctx context.Context, username, password string) (*TokenPair, error) {
	user, err := s.repo.GetUserByUsername(ctx, username)
	if errors.Is(err, ErrNotFound) {
		// Return generic error to prevent username enumeration.
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, fmt.Errorf("login: %w", err)
	}
	if err := VerifyPassword(password, user.PasswordHash); err != nil {
		return nil, ErrInvalidCredentials
	}
	return s.issueTokenPair(ctx, user)
}

// Refresh validates the refresh token, rotates it (issues a new pair, deletes the old one),
// and returns the new token pair. Rotation invalidates stolen tokens immediately.
// ClaimRefreshToken is used to atomically delete-and-read, preventing concurrent
// requests from replaying the same token to mint multiple sessions.
func (s *Service) Refresh(ctx context.Context, refreshToken string) (*TokenPair, error) {
	rt, err := s.repo.ClaimRefreshToken(ctx, refreshToken)
	if err != nil {
		return nil, err // ErrNotFound or ErrTokenExpired
	}
	user, err := s.repo.GetUserByID(ctx, rt.UserID)
	if err != nil {
		return nil, fmt.Errorf("refresh: loading user: %w", err)
	}
	return s.issueTokenPair(ctx, user)
}

// Logout revokes the refresh token. The access token expires naturally via TTL.
// Returns nil even if the token was not found (idempotent — double-logout is safe).
// DB errors are propagated so callers can log them; the handler always clears cookies.
func (s *Service) Logout(ctx context.Context, refreshToken string) error {
	if err := s.repo.DeleteRefreshToken(ctx, refreshToken); err != nil {
		return fmt.Errorf("logout: revoking refresh token: %w", err)
	}
	return nil
}

// ValidateAccessToken parses and validates a JWT string.
// Returns the claims on success, ErrTokenInvalid or ErrTokenExpired on failure.
func (s *Service) ValidateAccessToken(tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return s.jwtSecret, nil
	})
	if err != nil {
		// jwt/v5 wraps expiry in jwt.ErrTokenExpired; use errors.Is for unwrapping.
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrTokenExpired
		}
		return nil, ErrTokenInvalid
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, ErrTokenInvalid
	}
	// Cross-validate: Subject must equal the UserID custom claim.
	// Prevents a crafted token from smuggling a different user's ID.
	if strconv.Itoa(claims.UserID) != claims.Subject {
		return nil, ErrTokenInvalid
	}
	return claims, nil
}

// EncryptCredential encrypts a plaintext camera credential using AES-256-GCM.
// Delegates to crypto.go; exposed here so callers do not need to import crypto
// functions directly or manage the key themselves.
func (s *Service) EncryptCredential(plaintext string) (string, error) {
	return EncryptCredential(plaintext, s.encKey)
}

// DecryptCredential decrypts a credential value.
// Values not prefixed with "enc:" are returned unchanged (plaintext compat).
func (s *Service) DecryptCredential(ciphertext string) (string, error) {
	return DecryptCredential(ciphertext, s.encKey)
}

// NeedsSetup reports whether first-run setup is required (no user accounts exist yet).
func (s *Service) NeedsSetup(ctx context.Context) (bool, error) {
	count, err := s.repo.CountUsers(ctx)
	if err != nil {
		return false, err
	}
	return count == 0, nil
}

// Setup creates the first admin account and issues a session token pair.
// Returns ErrSetupAlreadyDone if any user accounts already exist.
func (s *Service) Setup(ctx context.Context, username, password string) (*User, *TokenPair, error) {
	count, err := s.repo.CountUsers(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("setup: %w", err)
	}
	if count > 0 {
		return nil, nil, ErrSetupAlreadyDone
	}

	hash, err := HashPassword(password)
	if err != nil {
		return nil, nil, fmt.Errorf("setup: %w", err)
	}
	user, err := s.repo.CreateUser(ctx, username, hash, "admin")
	if err != nil {
		return nil, nil, fmt.Errorf("setup: creating user: %w", err)
	}
	pair, err := s.issueTokenPair(ctx, user)
	if err != nil {
		return nil, nil, fmt.Errorf("setup: issuing tokens: %w", err)
	}
	return user, pair, nil
}

// OIDCLoginOrCreate looks up the user by OIDC subject claim.
// If no matching user exists, a new viewer account is created using the preferred
// username or email local-part as the display name.
// Returns a fresh token pair ready to be set as cookies.
func (s *Service) OIDCLoginOrCreate(ctx context.Context, sub, username, email string) (*TokenPair, error) {
	displayName := username
	if displayName == "" && email != "" {
		if at := strings.Index(email, "@"); at > 0 {
			displayName = email[:at]
		}
	}
	if displayName == "" {
		displayName = "oidc_user"
	}

	user, err := s.repo.GetUserByOIDCSub(ctx, sub)
	if errors.Is(err, ErrNotFound) {
		user, err = s.repo.CreateOIDCUser(ctx, sub, displayName, "viewer")
		if err != nil {
			return nil, fmt.Errorf("OIDC: creating user for sub: %w", err)
		}
	} else if err != nil {
		return nil, fmt.Errorf("OIDC: looking up user: %w", err)
	}

	return s.issueTokenPair(ctx, user)
}

// IssueTokenPairForUserID creates a session token pair for the given user ID
// without requiring password verification. Used by QR pairing (Phase 12, CG11)
// where the pairing code itself serves as the authentication factor.
func (s *Service) IssueTokenPairForUserID(ctx context.Context, userID int) (*TokenPair, error) {
	user, err := s.repo.GetUserByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("issue tokens for user %d: %w", userID, err)
	}
	return s.issueTokenPair(ctx, user)
}

// ListUsers returns all user accounts. Delegates to Repository.ListUsers.
func (s *Service) ListUsers(ctx context.Context) ([]User, error) {
	return s.repo.ListUsers(ctx)
}

// CreateUser creates a new user with the given username, plaintext password, and role.
// The password is hashed with bcrypt before storage.
func (s *Service) CreateUser(ctx context.Context, username, password, role string) (*User, error) {
	hash, err := HashPassword(password)
	if err != nil {
		return nil, fmt.Errorf("hashing password: %w", err)
	}
	return s.repo.CreateUser(ctx, username, hash, role)
}

// DeleteUser removes a user by ID. Delegates to Repository.DeleteUser.
func (s *Service) DeleteUser(ctx context.Context, id int) error {
	return s.repo.DeleteUser(ctx, id)
}

// UpdateUserRole changes a user's role. Delegates to Repository.UpdateUserRole.
func (s *Service) UpdateUserRole(ctx context.Context, id int, role string) (*User, error) {
	return s.repo.UpdateUserRole(ctx, id, role)
}

// UpdateUserPassword changes a user's password. The plaintext password is hashed before storage.
func (s *Service) UpdateUserPassword(ctx context.Context, id int, password string) error {
	hash, err := HashPassword(password)
	if err != nil {
		return fmt.Errorf("hashing password: %w", err)
	}
	return s.repo.UpdateUserPassword(ctx, id, hash)
}

// issueTokenPair creates a new access JWT and a random refresh token for user.
func (s *Service) issueTokenPair(ctx context.Context, user *User) (*TokenPair, error) {
	now := time.Now()

	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   fmt.Sprintf("%d", user.ID),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.accessTTL)),
		},
		UserID:   user.ID,
		Username: user.Username,
		Role:     user.Role,
	}
	accessToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.jwtSecret)
	if err != nil {
		return nil, fmt.Errorf("signing access token: %w", err)
	}

	refreshToken, err := GenerateToken()
	if err != nil {
		return nil, err
	}
	expiresAt := now.Add(s.refreshTTL)
	if err := s.repo.CreateRefreshToken(ctx, user.ID, refreshToken, expiresAt); err != nil {
		return nil, fmt.Errorf("storing refresh token: %w", err)
	}

	return &TokenPair{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		AccessTTL:    s.accessTTL,
		RefreshTTL:   s.refreshTTL,
	}, nil
}
