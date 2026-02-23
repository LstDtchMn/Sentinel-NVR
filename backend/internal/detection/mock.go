package detection

import (
	"context"
	"sync"
)

// MockDetector returns a configurable static detection list without performing
// any real inference. Intended for integration testing and local development
// when a CodeProject.AI backend is not available.
//
// Usage:
//
//	mock := NewMockDetector()
//	mock.SetResponse([]DetectedObject{{Label: "person", Confidence: 0.95, ...}})
//
// With an empty response (the default), Detect returns no detections — the
// detection pipeline silently idles and no events are published.
//
// MockDetector is safe for concurrent use: a single instance may be shared
// across multiple DetectionPipeline goroutines, satisfying the Detector
// interface contract (CG10).
type MockDetector struct {
	mu       sync.RWMutex
	response []DetectedObject
}

// NewMockDetector returns a MockDetector with no detections configured.
func NewMockDetector() *MockDetector {
	return &MockDetector{}
}

// SetResponse configures the detections returned by every subsequent Detect call.
// Safe to call concurrently with Detect.
func (m *MockDetector) SetResponse(r []DetectedObject) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.response = r
}

// Detect returns the configured response, ignoring the image bytes.
// Implements Detector. Safe for concurrent use.
func (m *MockDetector) Detect(_ context.Context, _ []byte) ([]DetectedObject, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if len(m.response) == 0 {
		return nil, nil
	}
	// Return a copy to prevent callers from mutating the stored response slice.
	result := make([]DetectedObject, len(m.response))
	copy(result, m.response)
	return result, nil
}
