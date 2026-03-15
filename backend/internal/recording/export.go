// Package recording — export.go provides server-side clip extraction from recorded segments.
// Uses ffmpeg -c copy for fast, lossless sub-clip extraction. Exported clips are
// cleaned up automatically after 1 hour.
package recording

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
)

// ExportService handles server-side clip extraction from recorded segments.
type ExportService struct {
	repo      *Repository
	exportDir string
	logger    *slog.Logger
	mu        sync.Mutex
	active    int // concurrent export count
	maxActive int
}

// NewExportService creates an export service that writes clips to exportDir.
func NewExportService(repo *Repository, exportDir string, logger *slog.Logger) *ExportService {
	os.MkdirAll(exportDir, 0755)
	return &ExportService{
		repo:      repo,
		exportDir: exportDir,
		logger:    logger.With("component", "export"),
		maxActive: 3,
	}
}

// ExportResult contains the result of a clip export operation.
type ExportResult struct {
	ExportID    string  `json:"export_id"`
	DownloadURL string  `json:"download_url"`
	DurationS   float64 `json:"duration_s"`
	SizeBytes   int64   `json:"size_bytes"`
}

// ExportClip extracts a sub-clip from recorded segments using ffmpeg -c copy.
// Maximum export duration is 5 minutes. At most maxActive concurrent exports are allowed.
func (s *ExportService) ExportClip(ctx context.Context, cameraName string, start, end time.Time) (*ExportResult, error) {
	if end.Before(start) || end.Equal(start) {
		return nil, fmt.Errorf("end time must be after start time")
	}
	duration := end.Sub(start)
	if duration > 5*time.Minute {
		return nil, fmt.Errorf("maximum export duration is 5 minutes")
	}

	s.mu.Lock()
	if s.active >= s.maxActive {
		s.mu.Unlock()
		return nil, fmt.Errorf("too many concurrent exports (max %d)", s.maxActive)
	}
	s.active++
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.active--
		s.mu.Unlock()
	}()

	// Find segments spanning the time range
	segments, err := s.repo.List(ctx, cameraName, start, end, 100, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to query segments: %w", err)
	}
	if len(segments) == 0 {
		return nil, fmt.Errorf("no recordings found for the requested time range")
	}

	exportID := uuid.New().String()[:8]
	outputPath := filepath.Join(s.exportDir, exportID+".mp4")

	// Single segment case (most common)
	if len(segments) == 1 {
		seg := segments[0]
		offset := start.Sub(seg.StartTime)
		if offset < 0 {
			offset = 0
		}
		cmd := exec.CommandContext(ctx, "ffmpeg",
			"-ss", fmt.Sprintf("%.3f", offset.Seconds()),
			"-i", seg.Path,
			"-t", fmt.Sprintf("%.3f", duration.Seconds()),
			"-c", "copy",
			"-movflags", "+faststart",
			"-y", outputPath,
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			s.logger.Error("ffmpeg export failed", "error", err, "output", string(out))
			return nil, fmt.Errorf("ffmpeg export failed: %w", err)
		}
	} else {
		// Multi-segment: create concat file
		concatPath := filepath.Join(s.exportDir, exportID+"_concat.txt")
		f, err := os.Create(concatPath)
		if err != nil {
			return nil, fmt.Errorf("failed to create concat list: %w", err)
		}
		for _, seg := range segments {
			fmt.Fprintf(f, "file '%s'\n", seg.Path)
		}
		f.Close()
		defer os.Remove(concatPath)

		// Extract from concatenated source
		offset := start.Sub(segments[0].StartTime)
		if offset < 0 {
			offset = 0
		}
		cmd := exec.CommandContext(ctx, "ffmpeg",
			"-f", "concat", "-safe", "0",
			"-i", concatPath,
			"-ss", fmt.Sprintf("%.3f", offset.Seconds()),
			"-t", fmt.Sprintf("%.3f", duration.Seconds()),
			"-c", "copy",
			"-movflags", "+faststart",
			"-y", outputPath,
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			s.logger.Error("ffmpeg multi-segment export failed", "error", err, "output", string(out))
			return nil, fmt.Errorf("ffmpeg export failed: %w", err)
		}
	}

	// Get file info
	info, err := os.Stat(outputPath)
	if err != nil {
		return nil, fmt.Errorf("export file not created: %w", err)
	}

	// Schedule cleanup after 1 hour
	go func() {
		time.Sleep(1 * time.Hour)
		os.Remove(outputPath)
		s.logger.Debug("cleaned up export", "id", exportID)
	}()

	return &ExportResult{
		ExportID:    exportID,
		DownloadURL: fmt.Sprintf("/api/v1/recordings/export/%s/download", exportID),
		DurationS:   duration.Seconds(),
		SizeBytes:   info.Size(),
	}, nil
}

// ServePath returns the filesystem path for a given export ID, or empty if not found.
func (s *ExportService) ServePath(exportID string) string {
	path := filepath.Join(s.exportDir, exportID+".mp4")
	if _, err := os.Stat(path); err != nil {
		return ""
	}
	return path
}
