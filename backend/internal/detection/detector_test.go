package detection

import (
	"context"
	"sync"
	"testing"

	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/config"
)

func TestNewDetectorDisabled(t *testing.T) {
	cfg := &config.DetectionConfig{Enabled: false}
	det, err := NewDetector(cfg, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if det != nil {
		t.Error("expected nil detector when disabled")
	}
}

func TestNewDetectorMock(t *testing.T) {
	cfg := &config.DetectionConfig{
		Enabled: true,
		Backend: "mock",
	}
	det, err := NewDetector(cfg, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if det == nil {
		t.Fatal("expected non-nil mock detector")
	}

	mock, ok := det.(*MockDetector)
	if !ok {
		t.Fatal("expected *MockDetector type")
	}

	// Set a response
	mock.SetResponse([]DetectedObject{
		{Label: "person", Confidence: 0.95, BBox: BBox{XMin: 0.1, YMin: 0.1, XMax: 0.5, YMax: 0.9}},
	})

	results, err := mock.Detect(context.Background(), []byte("fake-jpeg"))
	if err != nil {
		t.Fatalf("Detect returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 detection, got %d", len(results))
	}
	if results[0].Label != "person" {
		t.Errorf("Label = %q, want %q", results[0].Label, "person")
	}
}

func TestNewDetectorUnsupportedBackend(t *testing.T) {
	cfg := &config.DetectionConfig{
		Enabled: true,
		Backend: "unknown",
	}
	_, err := NewDetector(cfg, nil)
	if err == nil {
		t.Error("expected error for unsupported backend")
	}
}

func TestMockDetectorConcurrency(t *testing.T) {
	mock := NewMockDetector()
	mock.SetResponse([]DetectedObject{
		{Label: "car", Confidence: 0.8},
	})

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results, err := mock.Detect(context.Background(), []byte("fake"))
			if err != nil {
				t.Errorf("Detect error: %v", err)
			}
			if len(results) != 1 {
				t.Errorf("expected 1 result, got %d", len(results))
			}
		}()
	}
	wg.Wait()
}
