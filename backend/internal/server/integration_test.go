package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"

	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/auth"
	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/camera"
	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/config"
	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/db"
	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/detection"
	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/eventbus"
	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/notification"
	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/recording"
	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/storage"
	"github.com/LstDtchMn/Sentinel-NVR/backend/pkg/go2rtc"
	"github.com/LstDtchMn/Sentinel-NVR/backend/pkg/models"
)

// testEnv bundles the components created by setupTestServer so tests can access them.
type testEnv struct {
	server      *httptest.Server
	authService *auth.Service
	bus         *eventbus.Bus
}

// setupTestServer creates a fully-wired Server backed by an in-memory SQLite database,
// a mock go2rtc HTTP server, and real repositories. It returns an httptest.Server ready
// for integration testing and a cleanup function registered via t.Cleanup.
func setupTestServer(t *testing.T) *testEnv {
	t.Helper()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// In-memory SQLite with migrations applied
	sqlDB, err := db.Open(":memory:", false, logger)
	if err != nil {
		t.Fatalf("opening in-memory db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	// Mock go2rtc server — returns canned responses for Health, Streams, AddStream, RemoveStream
	mockG2R := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/streams" && r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, "{}")
		case r.URL.Path == "/api/streams" && r.Method == http.MethodPut:
			w.WriteHeader(http.StatusOK)
		case r.URL.Path == "/api/streams" && r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(func() { mockG2R.Close() })

	hotPath := t.TempDir()
	snapshotPath := t.TempDir()
	modelsDir := t.TempDir()

	cfg := &config.Config{
		Server: config.ServerConfig{
			Host:     "127.0.0.1",
			Port:     0, // not used — httptest picks a random port
			LogLevel: "info",
		},
		Auth: config.AuthConfig{
			Enabled:         true,
			AccessTokenTTL:  900,
			RefreshTokenTTL: 604800,
			SecureCookie:    false,
			AllowedOrigins:  []string{"http://localhost:5173"},
		},
		Storage: config.StorageConfig{
			HotPath:                hotPath,
			HotRetentionDays:       3,
			SegmentDuration:        10,
			SegmentFormat:          "mp4",
			MigrationIntervalHours: 1,
			CleanupIntervalHours:   6,
		},
		Detection: config.DetectionConfig{
			SnapshotPath: snapshotPath,
		},
		Go2RTC: config.Go2RTCConfig{
			APIURL:  mockG2R.URL,
			RTSPURL: "rtsp://127.0.0.1:8554",
		},
	}

	// Repositories
	authRepo := auth.NewRepository(sqlDB)
	ctx := context.Background()

	authSvc, err := auth.New(ctx, authRepo, cfg.Auth.AccessTokenTTL, cfg.Auth.RefreshTokenTTL)
	if err != nil {
		t.Fatalf("creating auth service: %v", err)
	}

	// Create admin user
	hash, err := auth.HashPassword("testpass")
	if err != nil {
		t.Fatalf("hashing password: %v", err)
	}
	_, err = authRepo.CreateUser(ctx, "admin", hash, "admin")
	if err != nil {
		t.Fatalf("creating admin user: %v", err)
	}

	camRepo := camera.NewRepository(sqlDB, authSvc)
	recRepo := recording.NewRepository(sqlDB)
	detRepo := detection.NewRepository(sqlDB, snapshotPath)
	faceRepo := detection.NewFaceRepository(sqlDB)
	retentionRepo := storage.NewRetentionRepository(sqlDB)
	notifRepo := notification.NewRepository(sqlDB)
	bus := eventbus.New(64, logger)
	t.Cleanup(func() { bus.Close() })

	g2rClient := go2rtc.NewClient(mockG2R.URL)

	camManager := camera.NewManager(
		camRepo,
		g2rClient,
		bus,
		cfg.Storage,
		cfg.Go2RTC.RTSPURL,
		recRepo,
		nil, // detector
		cfg.Detection,
		logger,
		nil, // PipelineDeps
	)

	modelManager := models.NewManager(modelsDir, "", logger)
	logLevel := &slog.LevelVar{}

	configPath := t.TempDir() + "/sentinel.yml"

	s := New(
		cfg,
		configPath,
		"test-version",
		sqlDB,
		authSvc,
		nil, // oidcProvider
		logLevel,
		camManager,
		camRepo,
		recRepo,
		detRepo,
		faceRepo,
		nil, // faceRecognizer
		retentionRepo,
		modelManager,
		g2rClient,
		bus,
		notifRepo,
		nil, // notifSenders
		nil, // backupMgr
		nil, // exportService
		logger,
	)

	ts := httptest.NewServer(s.router)
	t.Cleanup(func() { ts.Close() })

	return &testEnv{
		server:      ts,
		authService: authSvc,
		bus:         bus,
	}
}

// loginAndGetCookies POSTs to /api/v1/auth/login and returns the response cookies.
func loginAndGetCookies(t *testing.T, serverURL, username, password string) []*http.Cookie {
	t.Helper()

	body, err := json.Marshal(map[string]string{
		"username": username,
		"password": password,
	})
	if err != nil {
		t.Fatalf("marshaling login body: %v", err)
	}

	resp, err := http.Post(serverURL+"/api/v1/auth/login", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("login request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("login returned %d: %s", resp.StatusCode, b)
	}

	cookies := resp.Cookies()
	if len(cookies) == 0 {
		t.Fatal("login returned no cookies")
	}
	return cookies
}

// authenticatedClient logs in as admin and returns an http.Client with a cookie jar pre-loaded.
func authenticatedClient(t *testing.T, serverURL string) *http.Client {
	t.Helper()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("creating cookie jar: %v", err)
	}

	client := &http.Client{Jar: jar}

	body, err := json.Marshal(map[string]string{
		"username": "admin",
		"password": "testpass",
	})
	if err != nil {
		t.Fatalf("marshaling login body: %v", err)
	}

	resp, err := client.Post(serverURL+"/api/v1/auth/login", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("login request failed: %v", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login returned %d", resp.StatusCode)
	}

	return client
}

// ─── Auth Route Tests ────────────────────────────────────────────────────────

func TestAuth_LoginSuccess(t *testing.T) {
	env := setupTestServer(t)

	body, _ := json.Marshal(map[string]string{
		"username": "admin",
		"password": "testpass",
	})
	resp, err := http.Post(env.server.URL+"/api/v1/auth/login", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, b)
	}

	// Verify cookies are set
	var hasAccess, hasRefresh bool
	for _, c := range resp.Cookies() {
		if c.Name == "sentinel_access" && c.Value != "" {
			hasAccess = true
		}
		if c.Name == "sentinel_refresh" && c.Value != "" {
			hasRefresh = true
		}
	}
	if !hasAccess {
		t.Error("expected sentinel_access cookie to be set")
	}
	if !hasRefresh {
		t.Error("expected sentinel_refresh cookie to be set")
	}

	// Verify response body
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if msg, ok := result["message"].(string); !ok || msg != "logged in" {
		t.Errorf("expected message 'logged in', got %v", result["message"])
	}
}

func TestAuth_LoginWrongPassword(t *testing.T) {
	env := setupTestServer(t)

	body, _ := json.Marshal(map[string]string{
		"username": "admin",
		"password": "wrongpassword",
	})
	resp, err := http.Post(env.server.URL+"/api/v1/auth/login", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if _, ok := result["error"]; !ok {
		t.Error("expected error field in response")
	}
}

func TestAuth_Me_Authenticated(t *testing.T) {
	env := setupTestServer(t)
	client := authenticatedClient(t, env.server.URL)

	resp, err := client.Get(env.server.URL + "/api/v1/auth/me")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, b)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if result["username"] != "admin" {
		t.Errorf("expected username 'admin', got %v", result["username"])
	}
	if result["role"] != "admin" {
		t.Errorf("expected role 'admin', got %v", result["role"])
	}
}

func TestAuth_Me_Unauthenticated(t *testing.T) {
	env := setupTestServer(t)

	resp, err := http.Get(env.server.URL + "/api/v1/auth/me")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestAuth_Logout(t *testing.T) {
	env := setupTestServer(t)
	client := authenticatedClient(t, env.server.URL)

	resp, err := client.Post(env.server.URL+"/api/v1/auth/logout", "application/json", nil)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, b)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if result["message"] != "logged out" {
		t.Errorf("expected message 'logged out', got %v", result["message"])
	}
}

func TestAuth_Refresh(t *testing.T) {
	env := setupTestServer(t)

	// First login to get cookies
	cookies := loginAndGetCookies(t, env.server.URL, "admin", "testpass")

	// Build a request with only the refresh cookie
	req, err := http.NewRequest(http.MethodPost, env.server.URL+"/api/v1/auth/refresh", nil)
	if err != nil {
		t.Fatalf("creating request: %v", err)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, b)
	}

	// Verify new cookies were issued
	var hasNewAccess bool
	for _, c := range resp.Cookies() {
		if c.Name == "sentinel_access" && c.Value != "" {
			hasNewAccess = true
		}
	}
	if !hasNewAccess {
		t.Error("expected new sentinel_access cookie after refresh")
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if result["message"] != "refreshed" {
		t.Errorf("expected message 'refreshed', got %v", result["message"])
	}
}

// ─── Camera CRUD Tests ───────────────────────────────────────────────────────

func TestCamera_Create(t *testing.T) {
	env := setupTestServer(t)
	client := authenticatedClient(t, env.server.URL)

	body, _ := json.Marshal(map[string]interface{}{
		"name":        "TestCam",
		"main_stream": "rtsp://localhost:8554/test",
		"enabled":     true,
		"record":      false,
		"detect":      false,
	})
	resp, err := client.Post(env.server.URL+"/api/v1/cameras", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 201, got %d: %s", resp.StatusCode, b)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if result["name"] != "TestCam" {
		t.Errorf("expected name 'TestCam', got %v", result["name"])
	}
	if result["main_stream"] != "rtsp://localhost:8554/test" {
		t.Errorf("expected main_stream 'rtsp://localhost:8554/test', got %v", result["main_stream"])
	}
}

func TestCamera_CreateDuplicate(t *testing.T) {
	env := setupTestServer(t)
	client := authenticatedClient(t, env.server.URL)

	body, _ := json.Marshal(map[string]interface{}{
		"name":        "DupCam",
		"main_stream": "rtsp://localhost:8554/test",
		"enabled":     true,
		"record":      false,
		"detect":      false,
	})

	// Create first
	resp, err := client.Post(env.server.URL+"/api/v1/cameras", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("first create failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("first create: expected 201, got %d", resp.StatusCode)
	}

	// Create duplicate
	resp, err = client.Post(env.server.URL+"/api/v1/cameras", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("second create failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 409, got %d: %s", resp.StatusCode, b)
	}
}

func TestCamera_CreateInvalidURL(t *testing.T) {
	env := setupTestServer(t)
	client := authenticatedClient(t, env.server.URL)

	body, _ := json.Marshal(map[string]interface{}{
		"name":        "BadCam",
		"main_stream": "ftp://invalid-scheme/stream",
		"enabled":     true,
		"record":      false,
		"detect":      false,
	})
	resp, err := client.Post(env.server.URL+"/api/v1/cameras", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 400, got %d: %s", resp.StatusCode, b)
	}
}

func TestCamera_List(t *testing.T) {
	env := setupTestServer(t)
	client := authenticatedClient(t, env.server.URL)

	// Create a camera first
	body, _ := json.Marshal(map[string]interface{}{
		"name":        "ListCam",
		"main_stream": "rtsp://localhost:8554/test",
		"enabled":     true,
		"record":      false,
		"detect":      false,
	})
	resp, err := client.Post(env.server.URL+"/api/v1/cameras", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	resp.Body.Close()

	// List cameras
	resp, err = client.Get(env.server.URL + "/api/v1/cameras")
	if err != nil {
		t.Fatalf("list request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, b)
	}

	var cameras []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&cameras); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if len(cameras) < 1 {
		t.Error("expected at least 1 camera in list")
	}

	found := false
	for _, cam := range cameras {
		if cam["name"] == "ListCam" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected to find 'ListCam' in camera list")
	}
}

func TestCamera_GetByName(t *testing.T) {
	env := setupTestServer(t)
	client := authenticatedClient(t, env.server.URL)

	// Create a camera
	body, _ := json.Marshal(map[string]interface{}{
		"name":        "GetCam",
		"main_stream": "rtsp://localhost:8554/test",
		"enabled":     true,
		"record":      false,
		"detect":      false,
	})
	resp, err := client.Post(env.server.URL+"/api/v1/cameras", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	resp.Body.Close()

	// Get by name
	resp, err = client.Get(env.server.URL + "/api/v1/cameras/GetCam")
	if err != nil {
		t.Fatalf("get request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, b)
	}

	var cam map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&cam); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if cam["name"] != "GetCam" {
		t.Errorf("expected name 'GetCam', got %v", cam["name"])
	}
}

func TestCamera_GetNotFound(t *testing.T) {
	env := setupTestServer(t)
	client := authenticatedClient(t, env.server.URL)

	resp, err := client.Get(env.server.URL + "/api/v1/cameras/nonexistent")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestCamera_Update(t *testing.T) {
	env := setupTestServer(t)
	client := authenticatedClient(t, env.server.URL)

	// Create a camera
	createBody, _ := json.Marshal(map[string]interface{}{
		"name":        "UpdCam",
		"main_stream": "rtsp://localhost:8554/test",
		"enabled":     true,
		"record":      false,
		"detect":      false,
	})
	resp, err := client.Post(env.server.URL+"/api/v1/cameras", "application/json", bytes.NewReader(createBody))
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	resp.Body.Close()

	// Update the camera
	updateBody, _ := json.Marshal(map[string]interface{}{
		"main_stream": "rtsp://localhost:8554/updated",
		"enabled":     true,
		"record":      true,
		"detect":      false,
	})
	req, err := http.NewRequest(http.MethodPut, env.server.URL+"/api/v1/cameras/UpdCam", bytes.NewReader(updateBody))
	if err != nil {
		t.Fatalf("creating request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("update request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, b)
	}

	var cam map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&cam); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if cam["main_stream"] != "rtsp://localhost:8554/updated" {
		t.Errorf("expected updated main_stream, got %v", cam["main_stream"])
	}
	if cam["record"] != true {
		t.Errorf("expected record=true after update, got %v", cam["record"])
	}
}

func TestCamera_Delete(t *testing.T) {
	env := setupTestServer(t)
	client := authenticatedClient(t, env.server.URL)

	// Create a camera
	body, _ := json.Marshal(map[string]interface{}{
		"name":        "DelCam",
		"main_stream": "rtsp://localhost:8554/test",
		"enabled":     true,
		"record":      false,
		"detect":      false,
	})
	resp, err := client.Post(env.server.URL+"/api/v1/cameras", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	resp.Body.Close()

	// Delete the camera
	req, err := http.NewRequest(http.MethodDelete, env.server.URL+"/api/v1/cameras/DelCam", nil)
	if err != nil {
		t.Fatalf("creating delete request: %v", err)
	}
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("delete request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 204, got %d: %s", resp.StatusCode, b)
	}

	// Verify it's gone
	resp, err = client.Get(env.server.URL + "/api/v1/cameras/DelCam")
	if err != nil {
		t.Fatalf("get after delete failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 after delete, got %d", resp.StatusCode)
	}
}
