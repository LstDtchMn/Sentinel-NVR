// Package models provides AI model download and management for Sentinel NVR (R10, CG10).
// Models are downloaded on first use and cached in the local models directory.
// This eliminates the need for external Docker containers to manage AI models.
package models

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ModelInfo describes a downloadable AI model.
type ModelInfo struct {
	Name        string `json:"name"`         // human-readable name
	Filename    string `json:"filename"`      // e.g. "general_security.onnx"
	URL         string `json:"url"`           // download URL
	SHA256      string `json:"sha256"`        // expected hash for integrity verification
	SizeBytes   int64  `json:"size_bytes"`    // expected file size
	Description string `json:"description"`   // what this model detects
}

// Manifest lists all available pre-packaged models (R10).
// These are the models that Sentinel NVR ships with or can auto-download.
// URLs point to the project's model hosting (set at build time or via config).
var Manifest = []ModelInfo{
	{
		Name:        "General Security",
		Filename:    "general_security.onnx",
		Description: "YOLOv8n — Person, Vehicle, Animal detection",
	},
	{
		Name:        "Package Delivery",
		Filename:    "package_delivery.onnx",
		Description: "Fine-tuned for packages at doorsteps",
	},
	{
		Name:        "Face Recognition",
		Filename:    "face_recognition.onnx",
		Description: "ArcFace embeddings for face identification (R11)",
	},
	{
		Name:        "Audio Classification",
		Filename:    "audio_classify.onnx",
		Description: "YAMNet — Glass Break, Dog Bark, Baby Cry (R12)",
	},
}

// Manager handles model downloads, caching, and integrity verification.
type Manager struct {
	modelsDir string // directory where models are stored
	baseURL   string // base URL for model downloads (empty = manual install only)
	logger    *slog.Logger
	mu        sync.RWMutex
}

// NewManager creates a model manager that stores models in the given directory.
// baseURL is the remote server hosting the model files (e.g. "https://models.sentinel-nvr.dev/v1").
// If baseURL is empty, models must be placed manually in modelsDir.
func NewManager(modelsDir string, baseURL string, logger *slog.Logger) *Manager {
	return &Manager{
		modelsDir: modelsDir,
		baseURL:   baseURL,
		logger:    logger.With("component", "model_manager"),
	}
}

// ModelPath returns the absolute path to a model file.
// Returns an error if the model doesn't exist locally and cannot be downloaded.
func (m *Manager) ModelPath(filename string) (string, error) {
	path := filepath.Join(m.modelsDir, filename)
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	return "", fmt.Errorf("model %q not found in %s (place the file manually or configure a model download URL)", filename, m.modelsDir)
}

// EnsureModel checks if a model exists locally and downloads it if not.
// Returns the local path to the model file.
// The write lock is released before downloading to avoid blocking concurrent callers.
func (m *Manager) EnsureModel(info ModelInfo) (string, error) {
	path := filepath.Join(m.modelsDir, info.Filename)

	// Phase 1: Check under read lock.
	m.mu.RLock()
	if _, err := os.Stat(path); err == nil {
		if info.SHA256 != "" {
			if ok, _ := verifyHash(path, info.SHA256); ok {
				m.mu.RUnlock()
				return path, nil
			}
			m.logger.Warn("model hash mismatch, re-downloading", "model", info.Filename)
		} else {
			m.mu.RUnlock()
			return path, nil
		}
	}
	m.mu.RUnlock()

	// Resolve download URL before releasing any lock.
	if m.baseURL == "" || info.URL == "" {
		downloadURL := info.URL
		if downloadURL == "" && m.baseURL != "" {
			downloadURL = m.baseURL + "/" + info.Filename
		}
		if downloadURL == "" {
			return "", fmt.Errorf("model %q not found and no download URL configured", info.Filename)
		}
		info.URL = downloadURL
	}

	// Phase 2: Download without holding the lock — can take minutes for large models.
	// download() uses atomic rename (write to .tmp then rename) so concurrent reads
	// of the final path are safe.
	return m.download(info, path)
}

// ListLocal returns all model files found in the models directory.
func (m *Manager) ListLocal() ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if err := os.MkdirAll(m.modelsDir, 0755); err != nil {
		return nil, fmt.Errorf("creating models directory: %w", err)
	}

	entries, err := os.ReadDir(m.modelsDir)
	if err != nil {
		return nil, fmt.Errorf("reading models directory: %w", err)
	}

	var models []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".onnx" {
			models = append(models, e.Name())
		}
	}
	return models, nil
}

// download fetches a model file from the given URL and saves it to path.
// Uses a temporary file + rename for atomic writes (no partial files on failure).
func (m *Manager) download(info ModelInfo, path string) (string, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return "", fmt.Errorf("creating models directory: %w", err)
	}

	m.logger.Info("downloading model", "model", info.Filename, "url", info.URL)

	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Get(info.URL) //nolint:noctx — long download, no user context
	if err != nil {
		return "", fmt.Errorf("downloading %s: %w", info.Filename, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("downloading %s: HTTP %d", info.Filename, resp.StatusCode)
	}

	// Write to temp file first, then rename for atomicity.
	tmp := path + ".download"
	f, err := os.Create(tmp)
	if err != nil {
		return "", fmt.Errorf("creating temp file: %w", err)
	}

	written, err := io.Copy(f, resp.Body)
	if closeErr := f.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(tmp)
		return "", fmt.Errorf("writing %s: %w", info.Filename, err)
	}

	m.logger.Info("model downloaded", "model", info.Filename, "bytes", written)

	// Verify hash if provided.
	if info.SHA256 != "" {
		ok, actualHash := verifyHash(tmp, info.SHA256)
		if !ok {
			os.Remove(tmp)
			return "", fmt.Errorf("hash mismatch for %s: expected %s, got %s", info.Filename, info.SHA256, actualHash)
		}
	}

	// Atomic rename.
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return "", fmt.Errorf("renaming %s: %w", info.Filename, err)
	}

	return path, nil
}

// verifyHash checks if a file's SHA256 matches the expected hash.
func verifyHash(path, expectedHex string) (bool, string) {
	f, err := os.Open(path)
	if err != nil {
		return false, ""
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return false, ""
	}
	actual := hex.EncodeToString(h.Sum(nil))
	return actual == expectedHex, actual
}
