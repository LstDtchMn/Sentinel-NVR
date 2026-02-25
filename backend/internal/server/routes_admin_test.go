package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/auth"
	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/detection"
)

func init() { gin.SetMode(gin.TestMode) }

// newTestServer returns a minimal Server wired for route-level tests.
// When authEnabled is true, the authService is non-nil and requireAdmin
// will inspect the Gin context role key.
func newTestServer(authEnabled bool) *Server {
	s := &Server{
		logger: testLogger(),
	}
	if authEnabled {
		// A non-nil authService causes requireAdmin to check the role.
		// We don't need a real one — just a non-nil pointer.
		s.authService = &auth.Service{}
	}
	return s
}

// ─── requireAdmin ────────────────────────────────────────────────────────────

func TestRequireAdmin_AuthDisabled_AlwaysAllows(t *testing.T) {
	s := newTestServer(false)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	if !s.requireAdmin(c) {
		t.Error("requireAdmin should return true when auth is disabled")
	}
	if w.Code == http.StatusForbidden {
		t.Error("should not return 403 when auth is disabled")
	}
}

func TestRequireAdmin_AdminRole_Allows(t *testing.T) {
	s := newTestServer(true)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	c.Set(auth.CtxKeyRole, "admin")

	if !s.requireAdmin(c) {
		t.Error("requireAdmin should return true for admin role")
	}
}

func TestRequireAdmin_ViewerRole_Returns403(t *testing.T) {
	s := newTestServer(true)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	c.Set(auth.CtxKeyRole, "viewer")

	if s.requireAdmin(c) {
		t.Error("requireAdmin should return false for viewer role")
	}
	if w.Code != http.StatusForbidden {
		t.Errorf("expected HTTP 403, got %d", w.Code)
	}
}

func TestRequireAdmin_NoRole_Returns403(t *testing.T) {
	s := newTestServer(true)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	// no role set in context

	if s.requireAdmin(c) {
		t.Error("requireAdmin should return false when no role is set")
	}
	if w.Code != http.StatusForbidden {
		t.Errorf("expected HTTP 403, got %d", w.Code)
	}
}

// ─── sanitizeEventThumbnail ──────────────────────────────────────────────────

func TestSanitizeEventThumbnail_ReplacesFilesystemPath(t *testing.T) {
	ev := &detection.EventRecord{
		ID:        42,
		Thumbnail: "/data/snapshots/detection_42.jpg",
	}
	sanitizeEventThumbnail(ev)

	want := "/api/v1/events/42/thumbnail"
	if ev.Thumbnail != want {
		t.Errorf("Thumbnail = %q, want %q", ev.Thumbnail, want)
	}
}

func TestSanitizeEventThumbnail_EmptyStaysEmpty(t *testing.T) {
	ev := &detection.EventRecord{ID: 99, Thumbnail: ""}
	sanitizeEventThumbnail(ev)

	if ev.Thumbnail != "" {
		t.Errorf("expected empty thumbnail to remain empty, got %q", ev.Thumbnail)
	}
}

func TestSanitizeEventThumbnail_DifferentIDs(t *testing.T) {
	for _, id := range []int{1, 100, 999999} {
		ev := &detection.EventRecord{ID: id, Thumbnail: "/some/path.jpg"}
		sanitizeEventThumbnail(ev)

		want := fmt.Sprintf("/api/v1/events/%d/thumbnail", id)
		if ev.Thumbnail != want {
			t.Errorf("ID %d: Thumbnail = %q, want %q", id, ev.Thumbnail, want)
		}
	}
}
