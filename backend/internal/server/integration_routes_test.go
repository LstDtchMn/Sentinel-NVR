package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"
	"time"

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

// routesTestEnv holds all state returned by the setup helper.
type routesTestEnv struct {
	url         string              // base URL of the httptest server
	adminClient *http.Client        // client with admin cookies
	adminUserID int                 // DB ID of the admin user
	cameraID    int                 // DB ID of the seeded camera
	eventIDs    []int               // IDs of seeded events
	db          interface{ Close() error } // for deferred cleanup
	ts          *httptest.Server    // for deferred cleanup
	authSvc     *auth.Service       // for creating additional users
	authRepo    *auth.Repository    // for direct user creation
}

// setupRoutesTestServer creates an in-memory test server with real repos and seeded data.
// It returns a routesTestEnv with an authenticated admin http.Client.
func setupRoutesTestServer(t *testing.T) *routesTestEnv {
	t.Helper()

	logger := testLogger()

	// Open in-memory SQLite (runs migrations).
	sqlDB, err := db.Open(":memory:", false, logger)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}

	// Config with required fields.
	cfg := &config.Config{
		Server: config.ServerConfig{
			Host:     "127.0.0.1",
			Port:     8099,
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
			HotPath:                "/media/hot",
			HotRetentionDays:       3,
			SegmentDuration:        10,
			SegmentFormat:          "mp4",
			MigrationIntervalHours: 1,
			CleanupIntervalHours:   6,
		},
		Detection: config.DetectionConfig{
			Enabled:      false,
			Backend:      "remote",
			RemoteURL:    "http://localhost:32168",
			SnapshotPath: "/data/snapshots",
		},
		Go2RTC: config.Go2RTCConfig{
			APIURL:  "http://placeholder:1984", // replaced below by mock
			RTSPURL: "rtsp://placeholder:8554",
		},
		Notifications: config.NotificationConfig{
			Enabled:       true,
			RetryInterval: 60,
		},
	}

	// Use platform-appropriate absolute paths for config validation.
	tmpDir := t.TempDir()
	hotPath := tmpDir + "/hot"
	snapshotPath := tmpDir + "/snapshots"
	cfg.Storage.HotPath = hotPath
	cfg.Detection.SnapshotPath = snapshotPath

	ctx := context.Background()

	// Auth repo + service.
	authRepo := auth.NewRepository(sqlDB)
	authSvc, err := auth.New(ctx, authRepo, 900, 604800)
	if err != nil {
		t.Fatalf("auth.New: %v", err)
	}

	// Create admin user directly via repo.
	hash, err := auth.HashPassword("testpass")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	adminUser, err := authRepo.CreateUser(ctx, "admin", hash, "admin")
	if err != nil {
		t.Fatalf("CreateUser admin: %v", err)
	}

	// Repos.
	camRepo := camera.NewRepository(sqlDB, authSvc)
	recRepo := recording.NewRepository(sqlDB)
	detRepo := detection.NewRepository(sqlDB, snapshotPath)
	faceRepo := detection.NewFaceRepository(sqlDB)
	notifRepo := notification.NewRepository(sqlDB)
	retentionRepo := storage.NewRetentionRepository(sqlDB)
	modelMgr := models.NewManager(t.TempDir(), "", logger)

	// Event bus.
	bus := eventbus.New(64, logger)
	t.Cleanup(func() { bus.Close() })

	// Mock go2rtc — returns 200 on /api/streams for health checks.
	mockG2RTC := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("{}"))
	}))
	t.Cleanup(mockG2RTC.Close)

	g2rClient := go2rtc.NewClient(mockG2RTC.URL)
	cfg.Go2RTC.APIURL = mockG2RTC.URL

	// Camera manager.
	camManager := camera.NewManager(
		camRepo, g2rClient, bus,
		cfg.Storage, cfg.Go2RTC.RTSPURL,
		recRepo, nil, cfg.Detection, logger, nil,
	)

	// Create server.
	logLevel := new(slog.LevelVar)
	srv := New(
		cfg, "", "test-version", sqlDB,
		authSvc, nil, logLevel,
		camManager, camRepo, recRepo, detRepo,
		faceRepo, nil, retentionRepo, modelMgr,
		g2rClient, bus, notifRepo,
		nil, nil, nil, logger,
	)

	// Start httptest server using the Gin router directly.
	ts := httptest.NewServer(srv.router)
	t.Cleanup(ts.Close)
	t.Cleanup(func() { sqlDB.Close() })

	// Seed a camera for FK references.
	cam := &camera.CameraRecord{
		Name:       "testcam",
		Enabled:    true,
		MainStream: "rtsp://example.com/stream1",
		Record:     true,
		Detect:     false,
	}
	createdCam, err := camRepo.Create(ctx, cam)
	if err != nil {
		t.Fatalf("Create camera: %v", err)
	}

	// Seed events directly via SQL (detection.Repository has no Insert method).
	eventIDs := make([]int, 0, 4)
	for i, ev := range []struct {
		evType     string
		label      string
		confidence float64
		startTime  time.Time
	}{
		{"detection", "person", 0.95, time.Now().Add(-4 * time.Hour)},
		{"detection", "car", 0.80, time.Now().Add(-3 * time.Hour)},
		{"camera.connected", "", 0.0, time.Now().Add(-2 * time.Hour)},
		{"detection", "dog", 0.70, time.Now().Add(-1 * time.Hour)},
	} {
		var id int
		err := sqlDB.QueryRowContext(ctx,
			`INSERT INTO events (camera_id, type, label, confidence, data, thumbnail, has_clip, start_time, created_at)
			 VALUES (?, ?, ?, ?, '{}', '', 0, ?, CURRENT_TIMESTAMP)
			 RETURNING id`,
			createdCam.ID, ev.evType, ev.label, ev.confidence, ev.startTime,
		).Scan(&id)
		if err != nil {
			t.Fatalf("seed event %d: %v", i, err)
		}
		eventIDs = append(eventIDs, id)
	}

	// Login as admin to get cookies.
	adminClient := loginUser(t, ts.URL, "admin", "testpass")

	return &routesTestEnv{
		url:         ts.URL,
		adminClient: adminClient,
		adminUserID: adminUser.ID,
		cameraID:    createdCam.ID,
		eventIDs:    eventIDs,
		db:          sqlDB,
		ts:          ts,
		authSvc:     authSvc,
		authRepo:    authRepo,
	}
}

// loginUser logs in via POST /api/v1/auth/login and returns an http.Client with
// cookie jar populated from the Set-Cookie response headers.
func loginUser(t *testing.T, baseURL, username, password string) *http.Client {
	t.Helper()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New: %v", err)
	}
	client := &http.Client{Jar: jar}

	body, _ := json.Marshal(map[string]string{
		"username": username,
		"password": password,
	})
	resp, err := client.Post(baseURL+"/api/v1/auth/login", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("login request failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login expected 200, got %d", resp.StatusCode)
	}

	return client
}

// createViewerUser creates a viewer user and returns an authenticated client.
func createViewerUser(t *testing.T, env *routesTestEnv) *http.Client {
	t.Helper()

	hash, err := auth.HashPassword("viewerpass")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	_, err = env.authRepo.CreateUser(context.Background(), "viewer", hash, "viewer")
	if err != nil {
		t.Fatalf("CreateUser viewer: %v", err)
	}
	return loginUser(t, env.url, "viewer", "viewerpass")
}

// ─── Events tests ────────────────────────────────────────────────────────────

func TestEvents_List(t *testing.T) {
	env := setupRoutesTestServer(t)

	resp, err := env.adminClient.Get(env.url + "/api/v1/events")
	if err != nil {
		t.Fatalf("GET /events: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result struct {
		Events []json.RawMessage `json:"events"`
		Total  int               `json:"total"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result.Total != 4 {
		t.Errorf("expected total=4, got %d", result.Total)
	}
	if len(result.Events) != 4 {
		t.Errorf("expected 4 events, got %d", len(result.Events))
	}
}

func TestEvents_ListFilterByType(t *testing.T) {
	env := setupRoutesTestServer(t)

	resp, err := env.adminClient.Get(env.url + "/api/v1/events?type=detection")
	if err != nil {
		t.Fatalf("GET /events?type=detection: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result struct {
		Events []struct {
			Type string `json:"type"`
		} `json:"events"`
		Total int `json:"total"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result.Total != 3 {
		t.Errorf("expected total=3 detections, got %d", result.Total)
	}
	for i, ev := range result.Events {
		if ev.Type != "detection" {
			t.Errorf("event[%d] type=%q, expected detection", i, ev.Type)
		}
	}
}

func TestEvents_ListPagination(t *testing.T) {
	env := setupRoutesTestServer(t)

	resp, err := env.adminClient.Get(env.url + "/api/v1/events?limit=2&offset=0")
	if err != nil {
		t.Fatalf("GET /events?limit=2&offset=0: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result struct {
		Events []json.RawMessage `json:"events"`
		Total  int               `json:"total"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(result.Events) != 2 {
		t.Errorf("expected 2 events in page, got %d", len(result.Events))
	}
	// Total should reflect all matching events, not just this page.
	if result.Total != 4 {
		t.Errorf("expected total=4, got %d", result.Total)
	}
}

func TestEvents_GetByID(t *testing.T) {
	env := setupRoutesTestServer(t)

	eventID := env.eventIDs[0]
	resp, err := env.adminClient.Get(fmt.Sprintf("%s/api/v1/events/%d", env.url, eventID))
	if err != nil {
		t.Fatalf("GET /events/%d: %v", eventID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var ev struct {
		ID   int    `json:"id"`
		Type string `json:"type"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&ev); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if ev.ID != eventID {
		t.Errorf("expected id=%d, got %d", eventID, ev.ID)
	}
	if ev.Type != "detection" {
		t.Errorf("expected type=detection, got %q", ev.Type)
	}
}

func TestEvents_GetNotFound(t *testing.T) {
	env := setupRoutesTestServer(t)

	resp, err := env.adminClient.Get(env.url + "/api/v1/events/99999")
	if err != nil {
		t.Fatalf("GET /events/99999: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestEvents_Delete(t *testing.T) {
	env := setupRoutesTestServer(t)

	eventID := env.eventIDs[0]

	// DELETE the event.
	req, _ := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/api/v1/events/%d", env.url, eventID), nil)
	resp, err := env.adminClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE /events/%d: %v", eventID, err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}

	// Verify it's gone.
	resp2, err := env.adminClient.Get(fmt.Sprintf("%s/api/v1/events/%d", env.url, eventID))
	if err != nil {
		t.Fatalf("GET /events/%d after delete: %v", eventID, err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 after delete, got %d", resp2.StatusCode)
	}
}

// ─── Config & Health tests ──────────────────────────────────────────────────

func TestConfig_Get(t *testing.T) {
	env := setupRoutesTestServer(t)

	resp, err := env.adminClient.Get(env.url + "/api/v1/config")
	if err != nil {
		t.Fatalf("GET /config: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	for _, key := range []string{"server", "storage", "detection", "cameras"} {
		if _, ok := result[key]; !ok {
			t.Errorf("expected %q key in config response", key)
		}
	}
}

func TestConfig_Update(t *testing.T) {
	env := setupRoutesTestServer(t)

	body, _ := json.Marshal(map[string]interface{}{
		"server":  map[string]string{"log_level": "debug"},
		"storage": map[string]int{"hot_retention_days": 7},
	})
	req, _ := http.NewRequest(http.MethodPut, env.url+"/api/v1/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := env.adminClient.Do(req)
	if err != nil {
		t.Fatalf("PUT /config: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// The response is the full config (from handleGetConfig).
	var result map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	// Check that server.log_level was updated.
	var serverCfg struct {
		LogLevel string `json:"log_level"`
	}
	if err := json.Unmarshal(result["server"], &serverCfg); err != nil {
		t.Fatalf("decode server section: %v", err)
	}
	if serverCfg.LogLevel != "debug" {
		t.Errorf("expected log_level=debug, got %q", serverCfg.LogLevel)
	}

	// Check that storage.hot_retention_days was updated.
	var storageCfg struct {
		HotRetentionDays int `json:"hot_retention_days"`
	}
	if err := json.Unmarshal(result["storage"], &storageCfg); err != nil {
		t.Fatalf("decode storage section: %v", err)
	}
	if storageCfg.HotRetentionDays != 7 {
		t.Errorf("expected hot_retention_days=7, got %d", storageCfg.HotRetentionDays)
	}
}

func TestConfig_UpdateInvalid(t *testing.T) {
	env := setupRoutesTestServer(t)

	// log_level "invalid" is not valid per config.Validate().
	body, _ := json.Marshal(map[string]interface{}{
		"server": map[string]string{"log_level": "invalid"},
	})
	req, _ := http.NewRequest(http.MethodPut, env.url+"/api/v1/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := env.adminClient.Do(req)
	if err != nil {
		t.Fatalf("PUT /config: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}

	var errResp struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if errResp.Error == "" {
		t.Error("expected non-empty error message")
	}
}

func TestAdminHealth(t *testing.T) {
	env := setupRoutesTestServer(t)

	resp, err := env.adminClient.Get(env.url + "/api/v1/admin/health")
	if err != nil {
		t.Fatalf("GET /admin/health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	for _, key := range []string{"status", "uptime", "cameras_configured", "database", "go2rtc"} {
		if _, ok := result[key]; !ok {
			t.Errorf("expected %q key in admin health response", key)
		}
	}

	if status, ok := result["status"].(string); !ok || status != "ok" {
		t.Errorf("expected status=ok, got %v", result["status"])
	}
	if db, ok := result["database"].(string); !ok || db != "connected" {
		t.Errorf("expected database=connected, got %v", result["database"])
	}
}

// ─── Notification tests ─────────────────────────────────────────────────────

func TestNotif_CreateToken(t *testing.T) {
	env := setupRoutesTestServer(t)

	body, _ := json.Marshal(map[string]string{
		"provider": "webhook",
		"token":    "https://example.com/webhook",
		"label":    "test-hook",
	})
	resp, err := env.adminClient.Post(env.url+"/api/v1/notifications/tokens", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /notifications/tokens: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}

	var token struct {
		ID       int    `json:"id"`
		Provider string `json:"provider"`
		Token    string `json:"token"`
		Label    string `json:"label"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if token.ID == 0 {
		t.Error("expected non-zero token ID")
	}
	if token.Provider != "webhook" {
		t.Errorf("expected provider=webhook, got %q", token.Provider)
	}
	if token.Label != "test-hook" {
		t.Errorf("expected label=test-hook, got %q", token.Label)
	}
}

func TestNotif_ListTokens(t *testing.T) {
	env := setupRoutesTestServer(t)

	// Create a token first.
	createBody, _ := json.Marshal(map[string]string{
		"provider": "webhook",
		"token":    "https://example.com/webhook",
		"label":    "list-test",
	})
	createResp, err := env.adminClient.Post(env.url+"/api/v1/notifications/tokens", "application/json", bytes.NewReader(createBody))
	if err != nil {
		t.Fatalf("POST /notifications/tokens: %v", err)
	}
	createResp.Body.Close()

	// List tokens.
	resp, err := env.adminClient.Get(env.url + "/api/v1/notifications/tokens")
	if err != nil {
		t.Fatalf("GET /notifications/tokens: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var tokens []struct {
		ID       int    `json:"id"`
		Provider string `json:"provider"`
		Label    string `json:"label"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokens); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(tokens) < 1 {
		t.Fatal("expected at least 1 token")
	}

	found := false
	for _, tok := range tokens {
		if tok.Label == "list-test" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected to find token with label=list-test")
	}
}

func TestNotif_UpsertPref(t *testing.T) {
	env := setupRoutesTestServer(t)

	body, _ := json.Marshal(map[string]interface{}{
		"event_type": "detection",
		"enabled":    true,
		"critical":   false,
	})
	req, _ := http.NewRequest(http.MethodPut, env.url+"/api/v1/notifications/prefs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := env.adminClient.Do(req)
	if err != nil {
		t.Fatalf("PUT /notifications/prefs: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var pref struct {
		ID        int    `json:"id"`
		EventType string `json:"event_type"`
		Enabled   bool   `json:"enabled"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&pref); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if pref.ID == 0 {
		t.Error("expected non-zero pref ID")
	}
	if pref.EventType != "detection" {
		t.Errorf("expected event_type=detection, got %q", pref.EventType)
	}
	if !pref.Enabled {
		t.Error("expected enabled=true")
	}
}

func TestNotif_DeleteToken(t *testing.T) {
	env := setupRoutesTestServer(t)

	// Create a token to delete.
	createBody, _ := json.Marshal(map[string]string{
		"provider": "webhook",
		"token":    "https://example.com/webhook-delete",
		"label":    "delete-me",
	})
	createResp, err := env.adminClient.Post(env.url+"/api/v1/notifications/tokens", "application/json", bytes.NewReader(createBody))
	if err != nil {
		t.Fatalf("POST /notifications/tokens: %v", err)
	}
	defer createResp.Body.Close()

	var created struct {
		ID int `json:"id"`
	}
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	// DELETE the token.
	req, _ := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/api/v1/notifications/tokens/%d", env.url, created.ID), nil)
	resp, err := env.adminClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE /notifications/tokens/%d: %v", created.ID, err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}
}

// ─── Authorization tests ────────────────────────────────────────────────────

func TestAuthz_NonAdminCannotDeleteCamera(t *testing.T) {
	env := setupRoutesTestServer(t)
	viewerClient := createViewerUser(t, env)

	req, _ := http.NewRequest(http.MethodDelete, env.url+"/api/v1/cameras/testcam", nil)
	resp, err := viewerClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE /cameras/testcam as viewer: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

func TestAuthz_NonAdminCannotUpdateConfig(t *testing.T) {
	env := setupRoutesTestServer(t)
	viewerClient := createViewerUser(t, env)

	body, _ := json.Marshal(map[string]interface{}{
		"server": map[string]string{"log_level": "debug"},
	})
	req, _ := http.NewRequest(http.MethodPut, env.url+"/api/v1/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := viewerClient.Do(req)
	if err != nil {
		t.Fatalf("PUT /config as viewer: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

func TestAuthz_NonAdminCanListCameras(t *testing.T) {
	env := setupRoutesTestServer(t)
	viewerClient := createViewerUser(t, env)

	resp, err := viewerClient.Get(env.url + "/api/v1/cameras")
	if err != nil {
		t.Fatalf("GET /cameras as viewer: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestAuthz_UnauthenticatedBlocked(t *testing.T) {
	env := setupRoutesTestServer(t)

	// Use a plain client with no cookies.
	client := &http.Client{}
	resp, err := client.Get(env.url + "/api/v1/cameras")
	if err != nil {
		t.Fatalf("GET /cameras unauthenticated: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}
