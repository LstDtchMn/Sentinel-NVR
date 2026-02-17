package server

import (
	"encoding/json"
	"net/http"
	"runtime"
	"time"
)

var startTime = time.Now()

func (s *Server) registerRoutes(mux *http.ServeMux) {
	// API v1
	mux.HandleFunc("GET /api/v1/health", s.handleHealth)
	mux.HandleFunc("GET /api/v1/config", s.handleGetConfig)
	mux.HandleFunc("GET /api/v1/cameras", s.handleListCameras)
}

type healthResponse struct {
	Status    string `json:"status"`
	Version   string `json:"version"`
	Uptime    string `json:"uptime"`
	GoVersion string `json:"go_version"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	Cameras   int    `json:"cameras_configured"`
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	resp := healthResponse{
		Status:    "ok",
		Version:   "0.1.0-dev",
		Uptime:    time.Since(startTime).Round(time.Second).String(),
		GoVersion: runtime.Version(),
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
		Cameras:   len(s.cfg.Cameras),
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	// Return config with sensitive fields redacted
	type safeCamera struct {
		Name    string `json:"name"`
		Enabled bool   `json:"enabled"`
		Record  bool   `json:"record"`
		Detect  bool   `json:"detect"`
	}

	cameras := make([]safeCamera, len(s.cfg.Cameras))
	for i, c := range s.cfg.Cameras {
		cameras[i] = safeCamera{
			Name:    c.Name,
			Enabled: c.Enabled,
			Record:  c.Record,
			Detect:  c.Detect,
		}
	}

	resp := map[string]any{
		"server":    s.cfg.Server,
		"storage":   s.cfg.Storage,
		"detection": map[string]any{"enabled": s.cfg.Detection.Enabled, "backend": s.cfg.Detection.Backend},
		"cameras":   cameras,
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleListCameras(w http.ResponseWriter, r *http.Request) {
	type cameraStatus struct {
		Name      string `json:"name"`
		Enabled   bool   `json:"enabled"`
		Recording bool   `json:"recording"`
		Detecting bool   `json:"detecting"`
		Status    string `json:"status"` // connected | disconnected | error
	}

	statuses := make([]cameraStatus, len(s.cfg.Cameras))
	for i, c := range s.cfg.Cameras {
		statuses[i] = cameraStatus{
			Name:      c.Name,
			Enabled:   c.Enabled,
			Recording: false, // Stub — will be filled by CameraManager
			Detecting: false,
			Status:    "disconnected",
		}
	}
	writeJSON(w, http.StatusOK, statuses)
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
