// routes_faces.go — face recognition CRUD and enrollment handlers (Phase 13, R11).

package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/detection"
)

// handleListFaces returns all enrolled faces (without embeddings).
// GET /api/v1/faces
func (s *Server) handleListFaces(c *gin.Context) {
	if s.faceRepo == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "face recognition not configured"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	faces, err := s.faceRepo.List(ctx)
	if err != nil {
		s.logger.Error("failed to list faces", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"faces": faces})
}

// handleGetFace returns a single enrolled face by ID.
// GET /api/v1/faces/:id
func (s *Server) handleGetFace(c *gin.Context) {
	if s.faceRepo == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "face recognition not configured"})
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid face ID"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	face, err := s.faceRepo.GetByID(ctx, id)
	if errors.Is(err, detection.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "face not found"})
		return
	}
	if err != nil {
		s.logger.Error("failed to get face", "id", id, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, face)
}

// handleCreateFace enrolls a new face by receiving a name and a JPEG reference photo.
// The embedding is extracted via the sentinel-infer face embedding endpoint.
// POST /api/v1/faces — multipart/form-data: name (text), image (JPEG file)
// For now, accepts a pre-computed embedding JSON array if the inference endpoint
// is not yet available, allowing manual enrollment via API.
func (s *Server) handleCreateFace(c *gin.Context) {
	if s.faceRepo == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "face recognition not configured"})
		return
	}
	if !s.requireAdmin(c) {
		return
	}

	var req struct {
		Name      string    `json:"name" binding:"required"`
		Embedding []float32 `json:"embedding" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name and embedding are required"})
		return
	}
	if len(req.Name) == 0 || len(req.Name) > 128 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name must be 1-128 characters"})
		return
	}
	// sentinel-infer uses ArcFace which produces 512-dim unit vectors.
	// Reject anything else so cosine similarity calculations are consistent
	// and malformed clients cannot store oversized BLOBs.
	if len(req.Embedding) != 512 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "embedding must have exactly 512 dimensions (ArcFace)"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	face, err := s.faceRepo.Create(ctx, req.Name, req.Embedding, "")
	if err != nil {
		s.logger.Error("failed to create face", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusCreated, face)
}

// handleUpdateFace renames an enrolled face.
// PUT /api/v1/faces/:id   body: {"name":"new name"}
func (s *Server) handleUpdateFace(c *gin.Context) {
	if s.faceRepo == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "face recognition not configured"})
		return
	}
	if !s.requireAdmin(c) {
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid face ID"})
		return
	}

	var req struct {
		Name string `json:"name" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
		return
	}
	if len(req.Name) == 0 || len(req.Name) > 128 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name must be 1-128 characters"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	if err := s.faceRepo.Update(ctx, id, req.Name); err != nil {
		if errors.Is(err, detection.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "face not found"})
			return
		}
		s.logger.Error("failed to update face", "id", id, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	// Return the updated face record — consistent with other update endpoints.
	updated, getErr := s.faceRepo.GetByID(ctx, id)
	if getErr != nil {
		s.logger.Error("failed to fetch updated face", "id", id, "error", getErr)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, updated)
}

// handleDeleteFace removes an enrolled face by ID.
// DELETE /api/v1/faces/:id
func (s *Server) handleDeleteFace(c *gin.Context) {
	if s.faceRepo == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "face recognition not configured"})
		return
	}
	if !s.requireAdmin(c) {
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid face ID"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	if err := s.faceRepo.Delete(ctx, id); err != nil {
		if errors.Is(err, detection.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "face not found"})
			return
		}
		s.logger.Error("failed to delete face", "id", id, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	idStr := c.Param("id")
	s.logger.Info("face deleted", "id", idStr, "user", c.GetString("username"))
	c.Status(http.StatusNoContent)
}

// handleEnrollFace enrolls a new face from a JPEG photo via sentinel-infer (R11).
// POST /api/v1/faces/enroll — multipart/form-data: name (text field), image (JPEG file)
// The image is forwarded to sentinel-infer /v1/face/embed; the first detected face
// embedding is stored. Returns 422 if no face is detected in the photo.
// When face recognition is disabled (faceRecognizer nil), returns 503.
func (s *Server) handleEnrollFace(c *gin.Context) {
	if s.faceRepo == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "face recognition not configured"})
		return
	}
	if s.faceRecognizer == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "face recognition inference is disabled; use the raw embedding API instead"})
		return
	}
	if !s.requireAdmin(c) {
		return
	}

	name := c.PostForm("name")
	if name == "" || len(name) > 128 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name must be 1-128 characters"})
		return
	}

	file, err := c.FormFile("image")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "image file is required"})
		return
	}

	// Limit photo size to 16 MB (generous for a single reference JPEG).
	const maxPhotoBytes = 16 << 20
	if file.Size > maxPhotoBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "image must be ≤ 16 MB"})
		return
	}

	f, err := file.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read uploaded file"})
		return
	}
	defer f.Close()

	jpegBytes, err := io.ReadAll(io.LimitReader(f, maxPhotoBytes))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read image data"})
		return
	}

	// Call sentinel-infer to extract face embeddings from the photo (30s budget).
	embedCtx, embedCancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer embedCancel()

	embeddings, err := s.faceRecognizer.EmbedFaces(embedCtx, jpegBytes, 1)
	if err != nil {
		// TODO(review): L19 — return generic error message, log detail server-side
		s.logger.Warn("face embed call failed", "error", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("face embedding failed: %v", err)})
		return
	}
	if len(embeddings) == 0 {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "no face detected in the uploaded photo"})
		return
	}

	// Use the first (and only, max_faces=1) detected face.
	embedding := embeddings[0].Embedding
	if len(embedding) != 512 {
		s.logger.Warn("sentinel-infer returned unexpected embedding dimension",
			"got", len(embedding), "expected", 512)
		c.JSON(http.StatusBadGateway, gin.H{"error": "unexpected embedding dimension from inference server"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	face, err := s.faceRepo.Create(ctx, name, embedding, "")
	if err != nil {
		s.logger.Error("failed to enroll face", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusCreated, face)
}
