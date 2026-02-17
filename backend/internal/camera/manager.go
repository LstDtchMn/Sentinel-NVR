package camera

import (
	"log/slog"
	"sync"

	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/config"
)

// Manager supervises all camera pipelines.
type Manager struct {
	cfg     *config.Config
	logger  *slog.Logger
	cameras map[string]*Pipeline
	mu      sync.RWMutex
}

func NewManager(cfg *config.Config, logger *slog.Logger) *Manager {
	return &Manager{
		cfg:     cfg,
		logger:  logger.With("component", "camera_manager"),
		cameras: make(map[string]*Pipeline),
	}
}

// Start initializes pipelines for all enabled cameras.
func (m *Manager) Start() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i := range m.cfg.Cameras {
		cam := &m.cfg.Cameras[i]
		if !cam.Enabled {
			m.logger.Info("skipping disabled camera", "name", cam.Name)
			continue
		}

		pipeline := NewPipeline(cam, m.logger)
		m.cameras[cam.Name] = pipeline
		go pipeline.Start()
	}

	m.logger.Info("camera manager started", "active_cameras", len(m.cameras))
}

// Stop gracefully shuts down all camera pipelines.
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for name, pipeline := range m.cameras {
		m.logger.Info("stopping camera pipeline", "name", name)
		pipeline.Stop()
	}
	m.logger.Info("all camera pipelines stopped")
}

// Status returns the current state of a camera by name.
func (m *Manager) Status(name string) (PipelineStatus, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	pipeline, exists := m.cameras[name]
	if !exists {
		return PipelineStatus{}, false
	}
	return pipeline.Status(), true
}
