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

// CameraInfo carries camera identity into the detection package without
// importing the camera package — preventing a circular dependency:
//
//	camera/pipeline.go → detection → eventbus, go2rtc  (no camera import)
type CameraInfo struct {
	ID   int
	Name string
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
	default:
		return nil, fmt.Errorf("unsupported detection backend %q (supported: remote, mock; "+
			"local backends require CGo and are available in Phase 11)", cfg.Backend)
	}
}
