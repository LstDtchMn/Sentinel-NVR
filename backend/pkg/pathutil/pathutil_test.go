package pathutil

import (
	"path/filepath"
	"testing"
)

func TestIsUnderPath(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		base     string
		expected bool
	}{
		{"child file", filepath.Join("/data", "recordings", "cam1", "file.mp4"), "/data/recordings", true},
		{"nested child", filepath.Join("/data", "recordings", "a", "b", "c.mp4"), "/data/recordings", true},
		{"same path", "/data/recordings", "/data/recordings", false},
		{"parent traversal", filepath.Join("/data", "recordings", "..", "secrets"), "/data/recordings", false},
		{"sibling directory", "/data/other", "/data/recordings", false},
		{"completely different", "/tmp/file", "/data/recordings", false},
		{"empty path", "", "/data", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsUnderPath(tt.path, tt.base)
			if got != tt.expected {
				t.Errorf("IsUnderPath(%q, %q) = %v, want %v", tt.path, tt.base, got, tt.expected)
			}
		})
	}
}
