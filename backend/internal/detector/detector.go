// Package detector defines the AI inference interface (CG10).
// This is the key abstraction that allows swapping between
// OpenVINO, TensorRT, CoreML, and Coral backends.
package detector

import (
	"context"
	"sync"
)

// Detection represents a single object found in a frame.
type Detection struct {
	Label      string  `json:"label"`      // e.g. "person", "vehicle", "dog"
	Confidence float64 `json:"confidence"` // 0.0 to 1.0
	BBox       BBox    `json:"bbox"`       // Bounding box in normalized coordinates
}

// BBox is a bounding box with coordinates normalized to [0, 1].
type BBox struct {
	X1 float64 `json:"x1"` // Top-left X
	Y1 float64 `json:"y1"` // Top-left Y
	X2 float64 `json:"x2"` // Bottom-right X
	Y2 float64 `json:"y2"` // Bottom-right Y
}

// Frame is a decoded video frame ready for inference.
type Frame struct {
	Width  int
	Height int
	Data   []byte // Raw RGB24 pixel data
}

// Detector is the interface all AI backends must implement.
// Implementations live in sub-packages (openvino/, tensorrt/, etc.)
type Detector interface {
	// Init loads the model and prepares the backend.
	// device is backend-specific: "GPU", "CPU", "cuda:0", "/dev/dri/renderD128", etc.
	Init(ctx context.Context, modelPath string, device string) error

	// Detect runs inference on a single frame and returns detections.
	Detect(ctx context.Context, frame Frame) ([]Detection, error)

	// Close releases all resources held by the backend.
	Close() error

	// Name returns the backend identifier (e.g., "openvino", "tensorrt").
	Name() string
}

// registry maps backend names to constructor functions.
// Protected by registryMu for goroutine safety.
var (
	registryMu sync.RWMutex
	registry   = map[string]func() Detector{}
)

// Register adds a detector backend constructor to the global registry.
// Typically called from init() functions in backend sub-packages.
func Register(name string, fn func() Detector) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[name] = fn
}

// Get returns the constructor for a named detector backend.
func Get(name string) (func() Detector, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	fn, ok := registry[name]
	return fn, ok
}
