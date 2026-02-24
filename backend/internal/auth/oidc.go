package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/config"
)

// stateTTL is how long a generated OIDC state token is considered valid.
// The browser must complete the authorization redirect within this window.
const stateTTL = 5 * time.Minute

// stateCleanupInterval controls how often expired states are pruned.
const stateCleanupInterval = time.Minute

// OIDCProvider wraps the go-oidc + oauth2 flow with in-memory state management.
// State tokens are generated on /auth/oidc/login and validated on /auth/oidc/callback.
// A cleanup goroutine removes expired states every minute.
//
// Thread-safe: all state map accesses are guarded by mu.
type OIDCProvider struct {
	provider   *oidc.Provider
	oauth2     oauth2.Config
	verifier   *oidc.IDTokenVerifier
	stopCancel context.CancelFunc // cancels the cleanup goroutine

	mu     sync.Mutex
	states map[string]time.Time // state -> expiry
}

// NewOIDCProvider fetches the OIDC discovery document from cfg.ProviderURL and
// initialises the oauth2 config and ID token verifier.
func NewOIDCProvider(ctx context.Context, cfg config.OIDCConfig) (*OIDCProvider, error) {
	provider, err := oidc.NewProvider(ctx, cfg.ProviderURL)
	if err != nil {
		return nil, fmt.Errorf("fetching OIDC discovery document from %q: %w", cfg.ProviderURL, err)
	}

	oauth2Cfg := oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  cfg.RedirectURL,
		Endpoint:     provider.Endpoint(),
		// openid is required; profile and email give us preferred_username and email claims.
		Scopes: []string{oidc.ScopeOpenID, "profile", "email"},
	}

	verifier := provider.Verifier(&oidc.Config{ClientID: cfg.ClientID})

	p := &OIDCProvider{
		provider: provider,
		oauth2:   oauth2Cfg,
		verifier: verifier,
		states:   make(map[string]time.Time),
	}

	// Start background cleanup goroutine.
	// Store the cancel so Stop() can terminate the goroutine during shutdown.
	cleanupCtx, cleanupCancel := context.WithCancel(context.Background())
	p.stopCancel = cleanupCancel
	p.startCleanup(cleanupCtx)

	return p, nil
}

// AuthURL generates a new random state token, stores it with a 5-minute TTL,
// and returns the provider authorization URL to redirect the browser to.
func (p *OIDCProvider) AuthURL() (string, error) {
	state, err := generateState()
	if err != nil {
		return "", fmt.Errorf("generating OIDC state: %w", err)
	}

	p.mu.Lock()
	p.states[state] = time.Now().Add(stateTTL)
	p.mu.Unlock()

	return p.oauth2.AuthCodeURL(state), nil
}

// Exchange validates the state token, exchanges the authorization code for tokens,
// verifies the ID token, and returns the subject claim plus user identity hints.
func (p *OIDCProvider) Exchange(ctx context.Context, code, state string) (sub, username, email string, err error) {
	// Validate and consume the state token (one-time use).
	p.mu.Lock()
	expiry, ok := p.states[state]
	if ok {
		delete(p.states, state)
	}
	p.mu.Unlock()

	if !ok {
		return "", "", "", fmt.Errorf("unknown OIDC state token (expired or never issued)")
	}
	if time.Now().After(expiry) {
		return "", "", "", fmt.Errorf("OIDC state token expired")
	}

	// Exchange authorization code for an OAuth2 token set.
	oauth2Token, err := p.oauth2.Exchange(ctx, code)
	if err != nil {
		return "", "", "", fmt.Errorf("exchanging OIDC authorization code: %w", err)
	}

	// Extract and verify the ID token embedded in the OAuth2 response.
	rawIDToken, ok := oauth2Token.Extra("id_token").(string)
	if !ok {
		return "", "", "", fmt.Errorf("OIDC token response missing id_token")
	}

	idToken, err := p.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return "", "", "", fmt.Errorf("verifying OIDC id_token: %w", err)
	}

	// Parse standard claims.
	var claims struct {
		Sub               string `json:"sub"`
		PreferredUsername string `json:"preferred_username"`
		Email             string `json:"email"`
	}
	if err := idToken.Claims(&claims); err != nil {
		return "", "", "", fmt.Errorf("parsing OIDC id_token claims: %w", err)
	}
	if claims.Sub == "" {
		return "", "", "", fmt.Errorf("OIDC id_token missing sub claim")
	}

	return claims.Sub, claims.PreferredUsername, claims.Email, nil
}

// startCleanup runs a background goroutine that removes expired state tokens every minute.
func (p *OIDCProvider) startCleanup(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(stateCleanupInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				p.mu.Lock()
				for state, expiry := range p.states {
					if now.After(expiry) {
						delete(p.states, state)
					}
				}
				p.mu.Unlock()
			}
		}
	}()
}

// Stop cancels the cleanup goroutine. Call when the OIDCProvider is no longer needed
// (e.g. during graceful shutdown) to prevent goroutine leaks in tests and long-lived processes.
func (p *OIDCProvider) Stop() {
	if p.stopCancel != nil {
		p.stopCancel()
	}
}

// generateState returns a cryptographically random 16-byte hex string.
func generateState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
