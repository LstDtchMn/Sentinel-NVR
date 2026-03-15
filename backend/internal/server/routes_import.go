// routes_import.go — migration/import handlers (Phase 14, R15).

package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/camera"
	"github.com/LstDtchMn/Sentinel-NVR/backend/pkg/importers"
)

// parseImportFile reads the uploaded file and format, then returns a parsed ImportResult.
func (s *Server) parseImportFile(c *gin.Context) (*importers.ImportResult, bool) {
	format := c.PostForm("format")
	if format == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "format is required (blue_iris or frigate)"})
		return nil, false
	}

	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file upload is required"})
		return nil, false
	}
	f, err := file.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read uploaded file"})
		return nil, false
	}
	defer f.Close()

	// Use io.ReadAll with LimitReader — f.Read may return fewer bytes than
	// file.Size (partial read), and file.Size can be spoofed by the client.
	data, err := io.ReadAll(io.LimitReader(f, 5*1024*1024+1))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read file contents"})
		return nil, false
	}
	if len(data) > 5*1024*1024 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file exceeds 5MB limit"})
		return nil, false
	}

	var result *importers.ImportResult
	switch format {
	case "blue_iris":
		result = importers.ParseBlueIris(data)
	case "frigate":
		result = importers.ParseFrigate(data)
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported format — use 'blue_iris' or 'frigate'"})
		return nil, false
	}
	return result, true
}

// handleImportPreview parses an uploaded config file and returns a preview of
// what would be imported — without touching the database (dry run).
// POST /api/v1/import/preview — multipart/form-data: format (text), file (upload)
func (s *Server) handleImportPreview(c *gin.Context) {
	if !s.requireAdmin(c) {
		return
	}
	result, ok := s.parseImportFile(c)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, result)
}

// handleImportExecute parses the uploaded file and creates cameras in the database.
// Cameras that already exist (by name) are skipped with a warning.
// POST /api/v1/import — multipart/form-data: format (text), file (upload)
func (s *Server) handleImportExecute(c *gin.Context) {
	if !s.requireAdmin(c) {
		return
	}
	result, ok := s.parseImportFile(c)
	if !ok {
		return
	}
	if len(result.Cameras) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"imported": 0,
			"skipped":  0,
			"errors":   result.Errors,
			"warnings": result.Warnings,
		})
		return
	}

	imported := 0
	skipped := 0
	// Copy parse errors into a separate slice — appending to result.Errors would
	// also append to result.Warnings via shared backing array aliasing.
	importErrors := make([]string, len(result.Errors))
	copy(importErrors, result.Errors)

	for _, cam := range result.Cameras {
		rec := &camera.CameraRecord{
			Name:       cam.Name,
			Enabled:    cam.Enabled,
			MainStream: cam.MainStream,
			SubStream:  cam.SubStream,
			Record:     cam.Record,
			Detect:     cam.Detect,
			ONVIFHost:  cam.ONVIFHost,
			ONVIFPort:  cam.ONVIFPort,
			ONVIFUser:  cam.ONVIFUser,
			ONVIFPass:  cam.ONVIFPass,
		}

		camCtx, camCancel := context.WithTimeout(context.Background(), 15*time.Second)
		_, err := s.camManager.AddCamera(camCtx, rec)
		camCancel()
		if err != nil {
			if errors.Is(err, camera.ErrDuplicate) {
				skipped++
				result.Warnings = append(result.Warnings,
					fmt.Sprintf("camera %q: already exists, skipped", cam.Name))
			} else {
				importErrors = append(importErrors,
					fmt.Sprintf("camera %q: %v", cam.Name, err))
			}
			continue
		}
		imported++
	}

	s.logger.Info("import completed",
		"format", result.Format,
		"imported", imported,
		"skipped", skipped,
		"errors", len(importErrors),
	)

	c.JSON(http.StatusOK, gin.H{
		"imported": imported,
		"skipped":  skipped,
		"errors":   importErrors,
		"warnings": result.Warnings,
	})
}
