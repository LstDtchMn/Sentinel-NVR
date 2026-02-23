package auth

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

const (
	// AccessCookieName is the httpOnly cookie that carries the JWT access token.
	AccessCookieName = "sentinel_access"
	// RefreshCookieName is the httpOnly cookie that carries the refresh token.
	RefreshCookieName = "sentinel_refresh"

	// CtxKeyUserID is the Gin context key for the authenticated user's integer ID.
	CtxKeyUserID = "user_id"
	// CtxKeyUsername is the Gin context key for the authenticated user's name.
	CtxKeyUsername = "username"
	// CtxKeyRole is the Gin context key for the authenticated user's role.
	CtxKeyRole = "role"
)

// Middleware returns a Gin handler that validates the JWT access token from
// the sentinel_access httpOnly cookie. On success it sets user_id, username,
// and role in the Gin context for downstream handlers. On failure it returns
// 401 Unauthorized and aborts the request chain.
func (s *Service) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		tokenStr, err := c.Cookie(AccessCookieName)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
			c.Abort()
			return
		}

		claims, err := s.ValidateAccessToken(tokenStr)
		if err != nil {
			if errors.Is(err, ErrTokenExpired) {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "access token expired"})
			} else {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			}
			c.Abort()
			return
		}

		c.Set(CtxKeyUserID, claims.UserID)
		c.Set(CtxKeyUsername, claims.Username)
		c.Set(CtxKeyRole, claims.Role)
		c.Next()
	}
}

// SetTokenCookies writes the access and refresh tokens as httpOnly, SameSite=Lax cookies.
// Gin's SetCookie does not expose the SameSite attribute, so we write Set-Cookie headers
// directly via http.SetCookie which supports http.Cookie.SameSite (Phase 7, CG6).
// SameSite=Lax prevents cookies from being sent on cross-site POST/DELETE requests,
// which eliminates CSRF for state-changing endpoints. GET requests still carry the cookie
// (needed for SSE and streaming endpoints).
func SetTokenCookies(c *gin.Context, pair *TokenPair, secureCookie bool) {
	writeAuthCookie(c, AccessCookieName, pair.AccessToken, int(pair.AccessTTL.Seconds()), secureCookie)
	writeAuthCookie(c, RefreshCookieName, pair.RefreshToken, int(pair.RefreshTTL.Seconds()), secureCookie)
}

// ClearTokenCookies deletes the access and refresh cookies (logout).
// secureCookie must match the value used when the cookies were set — some
// browsers refuse to clear a Secure cookie via a non-Secure deletion response.
func ClearTokenCookies(c *gin.Context, secureCookie bool) {
	writeAuthCookie(c, AccessCookieName, "", -1, secureCookie)
	writeAuthCookie(c, RefreshCookieName, "", -1, secureCookie)
}

// writeAuthCookie writes a single httpOnly, SameSite=Lax cookie via http.SetCookie.
func writeAuthCookie(c *gin.Context, name, value string, maxAge int, secure bool) {
	// Set empty Value with MaxAge=-1 for deletions (Max-Age=0 means "expire immediately"
	// but some older browsers treat it differently; -1 forces "Max-Age=-1" which is
	// universally interpreted as "delete this cookie").
	cookie := &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode, // protects POST/DELETE from CSRF while allowing GET SSE
	}
	// Validate before writing — SameSite=Lax + empty domain = host-only cookie.
	if err := cookie.Valid(); err != nil {
		// Defensive: fall back to Gin's SetCookie (no SameSite), but log so the
		// operator can see that the SameSite guard was bypassed.
		fmt.Printf("auth: cookie validation failed, falling back to SetCookie: %v\n", err)
		c.SetCookie(name, value, maxAge, "/", "", secure, true)
		return
	}
	http.SetCookie(c.Writer, cookie)
}
