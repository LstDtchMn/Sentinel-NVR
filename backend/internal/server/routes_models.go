// routes_models.go — AI model management handlers (R10).

package server

import (
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"

	"github.com/LstDtchMn/Sentinel-NVR/backend/pkg/models"
)

// modelEntry is the JSON response for a single model in the list.
type modelEntry struct {
	Filename    string `json:"filename"`
	Name        string `json:"name"`
	Description string `json:"description"`
	SizeBytes   int64  `json:"size_bytes"`
	Installed   bool   `json:"installed"`
	Curated     bool   `json:"curated"` // true = part of the built-in manifest
}

// handleListModels returns the curated manifest merged with locally installed models.
// GET /api/v1/models
func (s *Server) handleListModels(c *gin.Context) {
	if !s.requireAdmin(c) {
		return
	}
	local, err := s.modelManager.ListLocal()
	if err != nil {
		s.logger.Error("models: list local failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list local models"})
		return
	}
	localSet := make(map[string]bool, len(local))
	for _, f := range local {
		localSet[f] = true
	}

	var entries []modelEntry

	// Emit curated manifest entries first with installed status.
	seen := make(map[string]bool)
	for _, m := range models.Manifest {
		entries = append(entries, modelEntry{
			Filename:    m.Filename,
			Name:        m.Name,
			Description: m.Description,
			SizeBytes:   m.SizeBytes,
			Installed:   localSet[m.Filename],
			Curated:     true,
		})
		seen[m.Filename] = true
	}

	// Append locally installed models not in the manifest (user uploads).
	for _, f := range local {
		if seen[f] {
			continue
		}
		// Stat the file for size.
		var size int64
		cfg := s.snapConfig()
		if fi, err := os.Stat(filepath.Join(cfg.Models.Dir, f)); err == nil {
			size = fi.Size()
		}
		entries = append(entries, modelEntry{
			Filename:  f,
			Name:      f,
			SizeBytes: size,
			Installed: true,
			Curated:   false,
		})
	}

	c.JSON(http.StatusOK, entries)
}

// handleDownloadModel triggers download of a curated model from the manifest.
// POST /api/v1/models/:filename/download
func (s *Server) handleDownloadModel(c *gin.Context) {
	if !s.requireAdmin(c) {
		return
	}
	filename := c.Param("filename")

	// Find the model in the curated manifest.
	var info *models.ModelInfo
	for _, m := range models.Manifest {
		if m.Filename == filename {
			info = &m
			break
		}
	}
	if info == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "model not found in curated manifest"})
		return
	}

	path, err := s.modelManager.EnsureModel(*info)
	if err != nil {
		s.logger.Error("models: download failed", "model", filename, "error", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "download failed: " + err.Error()})
		return
	}

	s.logger.Info("model downloaded", "model", filename, "path", path)
	c.JSON(http.StatusOK, gin.H{"filename": filename, "path": path, "status": "installed"})
}

// maxModelBytes caps the size of uploaded ONNX model files (2 GiB).
// Models larger than this are rejected to prevent disk exhaustion.
const maxModelBytes = 2 << 30

// handleUploadModel accepts a multipart ONNX file upload.
// POST /api/v1/models/upload  (multipart/form-data, field "file")
func (s *Server) handleUploadModel(c *gin.Context) {
	if !s.requireAdmin(c) {
		return
	}
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing 'file' field: " + err.Error()})
		return
	}
	defer file.Close()

	filename := header.Filename
	if filepath.Ext(filename) != ".onnx" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "only .onnx model files are accepted"})
		return
	}
	// Sanitise filename — strip directory components.
	filename = filepath.Base(filename)

	cfg := s.snapConfig()
	if err := os.MkdirAll(cfg.Models.Dir, 0755); err != nil {
		s.logger.Error("models: mkdir failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create models directory"})
		return
	}

	destPath := filepath.Join(cfg.Models.Dir, filename)
	tmp := destPath + ".upload"
	f, err := os.Create(tmp)
	if err != nil {
		s.logger.Error("models: create temp file failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create temp file"})
		return
	}

	written, err := io.Copy(f, io.LimitReader(file, maxModelBytes+1))
	if closeErr := f.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(tmp)
		s.logger.Error("models: write failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to write model file"})
		return
	}
	if written > maxModelBytes {
		os.Remove(tmp)
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "model file exceeds 2 GiB limit"})
		return
	}

	if err := os.Rename(tmp, destPath); err != nil {
		os.Remove(tmp)
		s.logger.Error("models: rename failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save model file"})
		return
	}

	s.logger.Info("model uploaded", "model", filename, "bytes", written)
	c.JSON(http.StatusCreated, gin.H{"filename": filename, "size_bytes": written, "status": "installed"})
}

// handleDeleteModel removes a locally installed model file.
// DELETE /api/v1/models/:filename
func (s *Server) handleDeleteModel(c *gin.Context) {
	if !s.requireAdmin(c) {
		return
	}
	filename := filepath.Base(c.Param("filename"))
	if filepath.Ext(filename) != ".onnx" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid model filename"})
		return
	}

	cfg := s.snapConfig()
	path := filepath.Join(cfg.Models.Dir, filename)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		c.JSON(http.StatusNotFound, gin.H{"error": "model file not found"})
		return
	}

	if err := os.Remove(path); err != nil {
		s.logger.Error("models: delete failed", "model", filename, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete model file"})
		return
	}

	s.logger.Info("model deleted", "model", filename)
	c.Status(http.StatusNoContent)
}
