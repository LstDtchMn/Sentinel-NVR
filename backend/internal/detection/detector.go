// Package detection implements AI object detection for Sentinel NVR (R3, CG10).
// It provides a pluggable Detector interface with implementations for remote
// HTTP inference backends (CodeProject.AI format) and a mock for testing.
// Local ONNX/OpenVINO/TensorRT backends require CGo and are deferred to Phase 11.
package detection

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/config"
)

// ErrNotFound is returned by repository methods when an event does not exist.
var ErrNotFound = errors.New("event not found")

// ZoneType distinguishes inclusion zones from exclusion zones.
// Include: only detect objects whose bbox centre is inside the zone.
// Exclude: suppress detections whose bbox centre is inside the zone.
type ZoneType string

const (
	ZoneInclude ZoneType = "include"
	ZoneExclude ZoneType = "exclude"
)

// ZonePoint is a single vertex of a zone polygon, normalised to [0.0, 1.0]
// relative to the frame width and height.
type ZonePoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// Zone is a named polygon region on a camera's field of view.
// Zones are stored as a JSON array in the cameras.zones column (migration 010).
type Zone struct {
	ID     string      `json:"id"`
	Name   string      `json:"name"`
	Type   ZoneType    `json:"type"`
	Points []ZonePoint `json:"points"`
}

// CameraInfo carries camera identity and zone config into the detection package
// without importing the camera package — preventing a circular dependency:
//
//	camera/pipeline.go → detection → eventbus, go2rtc  (no camera import)
type CameraInfo struct {
	ID    int
	Name  string
	Zones []Zone // configured detection zones; empty = full-frame detection
}

// BBox is a normalized bounding box where all coordinates are in the range
// [0.0, 1.0] relative to the image width and height. Normalization removes
// the dependency on the source image resolution from the stored event data.
type BBox struct {
	XMin float64 `json:"x_min"`
	YMin float64 `json:"y_min"`
	XMax float64 `json:"x_max"`
	YMax float64 `json:"y_max"`
}

// DetectedObject is a single inference result from a Detector.
type DetectedObject struct {
	Label      string  `json:"label"`
	Confidence float64 `json:"confidence"`
	BBox       BBox    `json:"bbox"`
}

// Detector runs inference on a raw JPEG image and returns detected objects.
// Implementations must be safe for concurrent use — a single Detector instance
// is shared across all camera DetectionPipelines (CG10).
type Detector interface {
	Detect(ctx context.Context, jpegBytes []byte) ([]DetectedObject, error)
}

// NewDetector creates the appropriate Detector from the detection configuration.
// Returns (nil, nil) when detection is disabled — callers must check for nil
// before starting detection pipelines.
func NewDetector(cfg *config.DetectionConfig, logger *slog.Logger) (Detector, error) {
	if !cfg.Enabled {
		return nil, nil
	}

	switch cfg.Backend {
	case "remote":
		if cfg.RemoteURL == "" {
			return nil, fmt.Errorf("detection.remote_url is required when backend=remote")
		}
		return NewRemoteDetector(cfg.RemoteURL, logger), nil
	case "mock":
		return NewMockDetector(), nil
	case "onnx", "local":
		// LocalDetector manages a sentinel-infer subprocess (CGo ONNX Runtime).
		// main.go checks for the Startable interface and calls Start() before use.
		return NewLocalDetector(cfg, logger)
	default:
		return nil, fmt.Errorf("unsupported detection backend %q (supported: remote, mock, onnx)", cfg.Backend)
	}
}
