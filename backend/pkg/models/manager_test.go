package models

import (
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestNewManager(t *testing.T) {
	m := NewManager("/tmp/models", "https://example.com", testLogger())
	if m.modelsDir != "/tmp/models" {
		t.Errorf("modelsDir = %q, want /tmp/models", m.modelsDir)
	}
	if m.baseURL != "https://example.com" {
		t.Errorf("baseURL = %q, want https://example.com", m.baseURL)
	}
}

func TestModelPathExists(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir, "", testLogger())

	// Create a fake model file
	modelPath := filepath.Join(dir, "test.onnx")
	if err := os.WriteFile(modelPath, []byte("fake-model"), 0644); err != nil {
		t.Fatal(err)
	}

	path, err := m.ModelPath("test.onnx")
	if err != nil {
		t.Fatalf("ModelPath failed: %v", err)
	}
	if path != modelPath {
		t.Errorf("path = %q, want %q", path, modelPath)
	}
}

func TestModelPathNotFound(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir, "", testLogger())

	_, err := m.ModelPath("nonexistent.onnx")
	if err == nil {
		t.Error("expected error for nonexistent model")
	}
}

func TestListLocal(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir, "", testLogger())

	// Create some model files
	for _, name := range []string{"model_a.onnx", "model_b.onnx", "not_a_model.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("data"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	models, err := m.ListLocal()
	if err != nil {
		t.Fatalf("ListLocal failed: %v", err)
	}
	if len(models) != 2 {
		t.Errorf("expected 2 models, got %d: %v", len(models), models)
	}
}

func TestListLocalEmpty(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir, "", testLogger())

	models, err := m.ListLocal()
	if err != nil {
		t.Fatalf("ListLocal failed: %v", err)
	}
	if len(models) != 0 {
		t.Errorf("expected 0 models, got %d", len(models))
	}
}

func TestEnsureModelDownload(t *testing.T) {
	modelContent := []byte("fake-onnx-model-data")
	h := sha256.Sum256(modelContent)
	hash := hex.EncodeToString(h[:])

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(modelContent)
	}))
	defer srv.Close()

	dir := t.TempDir()
	m := NewManager(dir, "", testLogger())

	info := ModelInfo{
		Name:     "Test Model",
		Filename: "test.onnx",
		URL:      srv.URL + "/test.onnx",
		SHA256:   hash,
	}

	path, err := m.EnsureModel(info)
	if err != nil {
		t.Fatalf("EnsureModel failed: %v", err)
	}

	// Verify the file exists
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading downloaded model: %v", err)
	}
	if string(data) != string(modelContent) {
		t.Error("downloaded model content mismatch")
	}
}

func TestEnsureModelAlreadyExists(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir, "", testLogger())

	// Pre-create the model file
	modelPath := filepath.Join(dir, "test.onnx")
	if err := os.WriteFile(modelPath, []byte("existing-model"), 0644); err != nil {
		t.Fatal(err)
	}

	info := ModelInfo{
		Filename: "test.onnx",
	}

	path, err := m.EnsureModel(info)
	if err != nil {
		t.Fatalf("EnsureModel failed: %v", err)
	}
	if path != modelPath {
		t.Errorf("path = %q, want %q", path, modelPath)
	}
}

func TestEnsureModelHashMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("some-model-data"))
	}))
	defer srv.Close()

	dir := t.TempDir()
	m := NewManager(dir, "", testLogger())

	info := ModelInfo{
		Filename: "test.onnx",
		URL:      srv.URL + "/test.onnx",
		SHA256:   "0000000000000000000000000000000000000000000000000000000000000000",
	}

	_, err := m.EnsureModel(info)
	if err == nil {
		t.Error("expected error for hash mismatch")
	}
}

func TestVerifyHash(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.bin")
	content := []byte("hello world")
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatal(err)
	}

	h := sha256.Sum256(content)
	expected := hex.EncodeToString(h[:])

	ok, actual := verifyHash(path, expected)
	if !ok {
		t.Errorf("hash should match: expected %s, got %s", expected, actual)
	}

	ok, _ = verifyHash(path, "wrong-hash")
	if ok {
		t.Error("wrong hash should not match")
	}
}

func TestManifestEntries(t *testing.T) {
	if len(Manifest) < 4 {
		t.Errorf("expected at least 4 manifest entries, got %d", len(Manifest))
	}

	names := make(map[string]bool)
	for _, m := range Manifest {
		if m.Filename == "" {
			t.Error("manifest entry with empty filename")
		}
		if m.Name == "" {
			t.Error("manifest entry with empty name")
		}
		if names[m.Filename] {
			t.Errorf("duplicate filename in manifest: %s", m.Filename)
		}
		names[m.Filename] = true
	}
}
