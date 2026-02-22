// Package camera manages per-camera RTSP pipelines (R1, R2, R3).
// Each camera gets its own Pipeline goroutine supervised by the Manager.
// Cameras are stored in SQLite (source of truth) and synced to go2rtc.
package camera

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/config"
	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/eventbus"
	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/recording"
	"github.com/LstDtchMn/Sentinel-NVR/backend/pkg/go2rtc"
)

// validCameraName allows alphanumeric characters, spaces, dashes, and underscores.
// Must be 1-64 characters, starting with an alphanumeric character, and the last
// character must not be a space (trailing spaces produce confusing filesystem paths).
// Phase 2 note: camera names become filesystem directory names for recordings.
// Spaces are allowed here but must be sanitized (e.g. replaced with underscores)
// when constructing recording paths: {hot_path}/{sanitized_name}/{date}/{time}.mp4
var validCameraName = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9 _-]{0,62}[a-zA-Z0-9_-])?$`)

// SanitizeName converts a camera name to a filesystem-safe directory name.
// Spaces → underscores, lowercased. Used for recording paths only;
// the DB and API keep the original name.
func SanitizeName(name string) string {
	s := strings.ToLower(name)
	s = strings.ReplaceAll(s, " ", "_")
	return s
}

// allowedStreamSchemes lists the protocols accepted for camera stream URLs.
var allowedStreamSchemes = map[string]bool{
	"rtsp":  true,
	"rtsps": true,
	"rtmp":  true,
}

// ValidateCameraInput checks a camera record for invalid or dangerous values.
// Exported so route handlers can give precise 400 responses before calling manager methods.
func ValidateCameraInput(cam *CameraRecord) error {
	if cam.Name == "" {
		return fmt.Errorf("camera name is required")
	}
	if !validCameraName.MatchString(cam.Name) {
		return fmt.Errorf("invalid camera name: must be 1-64 alphanumeric/spaces/dashes/underscores, starting with alphanumeric")
	}
	if cam.MainStream == "" {
		return fmt.Errorf("main_stream URL is required")
	}
	if err := validateStreamURL(cam.MainStream); err != nil {
		return fmt.Errorf("invalid main_stream: %w", err)
	}
	if cam.SubStream != "" {
		if err := validateStreamURL(cam.SubStream); err != nil {
			return fmt.Errorf("invalid sub_stream: %w", err)
		}
	}
	if cam.ONVIFPort < 0 || cam.ONVIFPort > 65535 {
		return fmt.Errorf("invalid onvif_port: must be 0-65535")
	}
	return nil
}

func validateStreamURL(raw string) error {
	if len(raw) > 2048 {
		return fmt.Errorf("URL exceeds maximum length (2048)")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("malformed URL")
	}
	if !allowedStreamSchemes[strings.ToLower(u.Scheme)] {
		return fmt.Errorf("unsupported protocol %q (must be rtsp, rtsps, or rtmp)", u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("missing host")
	}
	return nil
}

// RedactStreamURL replaces user:password in a stream URL for safe logging.
func RedactStreamURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "<invalid-url>"
	}
	if u.User != nil {
		u.User = url.UserPassword("***", "***")
	}
	return u.String()
}

// CameraWithStatus combines a DB record with live pipeline state.
type CameraWithStatus struct {
	CameraRecord   `json:",inline"`
	PipelineStatus PipelineStatus `json:"pipeline_status"`
}

// Manager supervises all camera pipelines, loading cameras from the database
// and syncing their streams to go2rtc. Stop() waits for all pipeline
// goroutines to finish before returning.
type Manager struct {
	repo       *Repository
	g2r        *go2rtc.Client
	bus        *eventbus.Bus
	storageCfg config.StorageConfig
	rtspBase   string // go2rtc RTSP base URL (e.g. "rtsp://go2rtc:8554")
	recRepo    *recording.Repository
	logger     *slog.Logger
	cameras    map[string]*Pipeline
	wg         sync.WaitGroup
	mu         sync.RWMutex
}

// NewManager creates a camera manager backed by the database and go2rtc.
func NewManager(
	repo *Repository,
	g2r *go2rtc.Client,
	bus *eventbus.Bus,
	storageCfg config.StorageConfig,
	rtspBase string,
	recRepo *recording.Repository,
	logger *slog.Logger,
) *Manager {
	return &Manager{
		repo:       repo,
		g2r:        g2r,
		bus:        bus,
		storageCfg: storageCfg,
		rtspBase:   rtspBase,
		recRepo:    recRepo,
		logger:     logger.With("component", "camera_manager"),
		cameras:    make(map[string]*Pipeline),
	}
}

// Start loads all enabled cameras from the database, syncs streams to go2rtc,
// and starts pipeline goroutines.
// go2rtc network calls run outside the write lock so that concurrent ListCameras
// calls are not blocked for the full startup duration (up to 5s per camera).
func (m *Manager) Start(ctx context.Context) error {
	cameras, err := m.repo.List(ctx)
	if err != nil {
		return fmt.Errorf("loading cameras from database: %w", err)
	}

	started := 0
	for i := range cameras {
		cam := cameras[i]
		if !cam.Enabled {
			m.logger.Info("skipping disabled camera", "name", cam.Name)
			continue
		}

		// go2rtc sync runs outside the write lock — can take up to 5s per camera
		if err := m.syncToGo2RTC(ctx, &cam); err != nil {
			m.logger.Error("failed to sync camera to go2rtc",
				"name", cam.Name,
				"main_stream", RedactStreamURL(cam.MainStream),
			)
			// Don't fail startup — pipeline will detect missing stream
		}

		pipeline := NewPipeline(&cam, m.g2r, m.rtspBase, m.storageCfg.HotPath, m.storageCfg.SegmentDuration, m.recRepo, m.bus, m.logger)
		m.mu.Lock()
		m.cameras[cam.Name] = pipeline
		m.wg.Add(1)
		m.mu.Unlock()

		started++
		go func() {
			defer m.wg.Done()
			pipeline.Start()
		}()
	}

	m.logger.Info("camera manager started", "active_cameras", started)
	return nil
}

// Stop gracefully shuts down all camera pipelines and waits for their
// goroutines to finish.
func (m *Manager) Stop() {
	m.mu.Lock()
	for name, pipeline := range m.cameras {
		m.logger.Info("stopping camera pipeline", "name", name)
		pipeline.Stop()
	}
	m.mu.Unlock()

	m.wg.Wait()
	m.logger.Info("all camera pipelines stopped")
}

// AddCamera validates the input, creates a camera in the DB, syncs to go2rtc, and starts a pipeline.
func (m *Manager) AddCamera(ctx context.Context, cam *CameraRecord) (*CameraWithStatus, error) {
	if err := ValidateCameraInput(cam); err != nil {
		return nil, err
	}

	created, err := m.repo.Create(ctx, cam)
	if err != nil {
		return nil, err
	}

	result := &CameraWithStatus{CameraRecord: *created}

	if created.Enabled {
		if err := m.syncToGo2RTC(ctx, created); err != nil {
			m.logger.Error("failed to sync new camera to go2rtc",
				"name", created.Name,
				"main_stream", RedactStreamURL(created.MainStream),
			)
		}

		pipeline := NewPipeline(created, m.g2r, m.rtspBase, m.storageCfg.HotPath, m.storageCfg.SegmentDuration, m.recRepo, m.bus, m.logger)
		m.mu.Lock()
		m.cameras[created.Name] = pipeline
		m.wg.Add(1) // must be inside lock so Stop() can't race between map insert and wg tracking
		m.mu.Unlock()

		go func() {
			defer m.wg.Done()
			pipeline.Start()
		}()

		result.PipelineStatus = pipeline.Status()
	}

	m.bus.Publish(eventbus.Event{
		Type:  "camera.added",
		Label: created.Name,
	})

	m.logger.Info("camera added", "name", created.Name, "enabled", created.Enabled)
	return result, nil
}

// UpdateCamera validates the input, updates a camera in the DB, and restarts the pipeline if needed.
// The lock is released between stopping the old pipeline and starting the new one to allow
// go2rtc network calls (removeFromGo2RTC, syncToGo2RTC) to run outside the critical section.
// This window is safe because camera names are unique per DB constraint — concurrent updates
// to the same camera would fail at the DB level.
func (m *Manager) UpdateCamera(ctx context.Context, name string, cam *CameraRecord) (*CameraWithStatus, error) {
	// For updates, the name comes from the URL path — validate stream URLs and other fields.
	cam.Name = name // ensure the record uses the canonical URL name
	if err := ValidateCameraInput(cam); err != nil {
		return nil, err
	}

	old, err := m.repo.GetByName(ctx, name)
	if err != nil {
		return nil, err
	}

	updated, err := m.repo.Update(ctx, name, cam)
	if err != nil {
		return nil, err
	}

	streamChanged := old.MainStream != updated.MainStream || old.SubStream != updated.SubStream
	enabledChanged := old.Enabled != updated.Enabled
	recordChanged := old.Record != updated.Record
	needsRestart := streamChanged || enabledChanged || recordChanged

	if needsRestart {
		// Stop old pipeline if running
		m.mu.Lock()
		var oldPipeline *Pipeline
		if pipeline, exists := m.cameras[name]; exists {
			pipeline.Stop()
			delete(m.cameras, name)
			oldPipeline = pipeline
		}
		m.mu.Unlock()

		// Wait for old pipeline goroutine to fully exit before starting new one,
		// preventing two ffmpeg processes writing to the same camera's directory.
		if oldPipeline != nil {
			<-oldPipeline.startDone
		}

		// Remove old streams from go2rtc
		m.removeFromGo2RTC(ctx, name, old.SubStream != "")

		// Start new pipeline if enabled
		if updated.Enabled {
			if err := m.syncToGo2RTC(ctx, updated); err != nil {
				m.logger.Error("failed to sync updated camera to go2rtc",
					"name", name,
					"main_stream", RedactStreamURL(updated.MainStream),
				)
			}

			pipeline := NewPipeline(updated, m.g2r, m.rtspBase, m.storageCfg.HotPath, m.storageCfg.SegmentDuration, m.recRepo, m.bus, m.logger)
			m.mu.Lock()
			m.cameras[name] = pipeline
			m.wg.Add(1) // must be inside lock so Stop() can't race between map insert and wg tracking
			m.mu.Unlock()

			go func() {
				defer m.wg.Done()
				pipeline.Start()
			}()
		}
	}

	result := &CameraWithStatus{CameraRecord: *updated}
	m.mu.RLock()
	if pipeline, exists := m.cameras[name]; exists {
		result.PipelineStatus = pipeline.Status()
	}
	m.mu.RUnlock()

	m.bus.Publish(eventbus.Event{
		Type:  "camera.updated",
		Label: name,
	})

	m.logger.Info("camera updated", "name", name, "restarted", needsRestart)
	return result, nil
}

// RemoveCamera stops the pipeline, removes from go2rtc, and deletes from DB.
func (m *Manager) RemoveCamera(ctx context.Context, name string) error {
	cam, err := m.repo.GetByName(ctx, name)
	if err != nil {
		return err
	}

	// Stop pipeline
	m.mu.Lock()
	var oldPipeline *Pipeline
	if pipeline, exists := m.cameras[name]; exists {
		pipeline.Stop()
		delete(m.cameras, name)
		oldPipeline = pipeline
	}
	m.mu.Unlock()

	// Wait for pipeline goroutine to fully exit
	if oldPipeline != nil {
		<-oldPipeline.startDone
	}

	// Remove from go2rtc
	m.removeFromGo2RTC(ctx, name, cam.SubStream != "")

	// Delete recording files from disk first: if this fails, the caller gets an error
	// and the DB record survives, allowing a retry. If we deleted the DB row first and
	// then crashed, the files would be orphaned permanently with no reference.
	sanitized := SanitizeName(name)
	recDir := filepath.Join(m.storageCfg.HotPath, sanitized)
	if err := os.RemoveAll(recDir); err != nil {
		m.logger.Warn("failed to remove recording directory", "path", recDir, "error", err)
		// Non-fatal: proceed with DB deletion so the camera can be removed from the UI.
		// Orphaned files can be cleaned up by a storage maintenance job.
	}

	// Delete from DB (cascades to recordings table via FK)
	if err := m.repo.Delete(ctx, name); err != nil {
		return err
	}

	m.bus.Publish(eventbus.Event{
		Type:  "camera.removed",
		Label: name,
	})

	m.logger.Info("camera removed", "name", name)
	return nil
}

// ListCameras returns all cameras from the DB with live pipeline status.
func (m *Manager) ListCameras(ctx context.Context) ([]CameraWithStatus, error) {
	cameras, err := m.repo.List(ctx)
	if err != nil {
		return nil, err
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]CameraWithStatus, len(cameras))
	for i, cam := range cameras {
		result[i] = CameraWithStatus{CameraRecord: cam}
		if pipeline, exists := m.cameras[cam.Name]; exists {
			result[i].PipelineStatus = pipeline.Status()
		}
	}
	return result, nil
}

// GetCamera returns a single camera from the DB with live pipeline status.
func (m *Manager) GetCamera(ctx context.Context, name string) (*CameraWithStatus, error) {
	cam, err := m.repo.GetByName(ctx, name)
	if err != nil {
		return nil, err
	}

	result := &CameraWithStatus{CameraRecord: *cam}
	m.mu.RLock()
	if pipeline, exists := m.cameras[name]; exists {
		result.PipelineStatus = pipeline.Status()
	}
	m.mu.RUnlock()

	return result, nil
}

// Status returns the pipeline status for a camera by name.
func (m *Manager) Status(name string) (PipelineStatus, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	pipeline, exists := m.cameras[name]
	if !exists {
		return PipelineStatus{}, false
	}
	return pipeline.Status(), true
}

// syncToGo2RTC registers a camera's streams with go2rtc via its REST API.
// Stream naming convention: main = "{camera_name}", sub = "{camera_name}_sub".
// Phase 3 will use these names for WebRTC/MSE live view URLs.
func (m *Manager) syncToGo2RTC(ctx context.Context, cam *CameraRecord) error {
	if err := m.g2r.AddStream(ctx, cam.Name, cam.MainStream); err != nil {
		return fmt.Errorf("adding main stream: %w", err)
	}
	if cam.SubStream != "" {
		if err := m.g2r.AddStream(ctx, cam.Name+"_sub", cam.SubStream); err != nil {
			return fmt.Errorf("adding sub stream: %w", err)
		}
	}
	return nil
}

// removeFromGo2RTC removes a camera's streams from go2rtc.
func (m *Manager) removeFromGo2RTC(ctx context.Context, name string, hasSubStream bool) {
	if err := m.g2r.RemoveStream(ctx, name); err != nil {
		m.logger.Warn("failed to remove main stream from go2rtc", "name", name, "error", err)
	}
	if hasSubStream {
		if err := m.g2r.RemoveStream(ctx, name+"_sub"); err != nil {
			m.logger.Warn("failed to remove sub stream from go2rtc", "name", name+"_sub", "error", err)
		}
	}
}
