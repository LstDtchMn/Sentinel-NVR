package detection

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image/jpeg"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"time"
)

// RemoteDetector sends JPEG frames to a CodeProject.AI-compatible HTTP endpoint
// and parses the bounding-box response. It normalizes absolute pixel coordinates
// to [0.0, 1.0] using the source image dimensions decoded from the JPEG header.
//
// CodeProject.AI API:
//
//	POST {url}/v1/vision/detection  multipart/form-data, field "image" = JPEG bytes
//	Response: {"success": bool, "predictions": [{label, confidence, x_min, y_min, x_max, y_max}]}
type RemoteDetector struct {
	url        string
	httpClient *http.Client
	logger     *slog.Logger
}

// NewRemoteDetector creates a RemoteDetector targeting the given base URL.
// The client timeout (15s) is a defense-in-depth fallback; callers are expected
// to pass a context with a shorter deadline (10s in processFrame). The longer
// transport timeout ensures the connection is eventually closed if the context
// mechanism fails to cancel the request.
func NewRemoteDetector(baseURL string, logger *slog.Logger) *RemoteDetector {
	return &RemoteDetector{
		url:    baseURL,
		logger: logger.With("component", "remote_detector"),
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// remoteResponse is the JSON payload returned by CodeProject.AI detection endpoints.
type remoteResponse struct {
	Success     bool               `json:"success"`
	Predictions []remotePrediction `json:"predictions"`
	Error       string             `json:"error,omitempty"`
}

// remotePrediction is a single bounding box + label from the remote backend.
// Coordinates are absolute pixels in the source image.
type remotePrediction struct {
	Label      string  `json:"label"`
	Confidence float64 `json:"confidence"`
	XMin       int     `json:"x_min"`
	YMin       int     `json:"y_min"`
	XMax       int     `json:"x_max"`
	YMax       int     `json:"y_max"`
}

// Detect posts the JPEG to the remote backend and returns normalized detections.
// Implements Detector.
func (r *RemoteDetector) Detect(ctx context.Context, jpegBytes []byte) ([]DetectedObject, error) {
	// Decode image dimensions from the JPEG header (no full decompression — fast).
	// Required to normalize absolute pixel coordinates to [0.0, 1.0].
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(jpegBytes))
	if err != nil {
		return nil, fmt.Errorf("decoding JPEG config for bbox normalization: %w", err)
	}
	if cfg.Width == 0 || cfg.Height == 0 {
		return nil, fmt.Errorf("JPEG has zero dimensions (%dx%d)", cfg.Width, cfg.Height)
	}
	w := float64(cfg.Width)
	h := float64(cfg.Height)

	// Build multipart form with the JPEG as "image" field.
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("image", "frame.jpg")
	if err != nil {
		return nil, fmt.Errorf("creating multipart field: %w", err)
	}
	if _, err := fw.Write(jpegBytes); err != nil {
		return nil, fmt.Errorf("writing JPEG to multipart: %w", err)
	}
	if err := mw.Close(); err != nil {
		return nil, fmt.Errorf("closing multipart writer: %w", err)
	}

	endpoint := r.url + "/v1/vision/detection"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, &buf)
	if err != nil {
		return nil, fmt.Errorf("creating detection request: %w", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("posting to detection backend: %w", err)
	}
	defer func() {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("detection backend returned %d: %s", resp.StatusCode, body)
	}

	var result remoteResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding detection response: %w", err)
	}
	if !result.Success {
		msg := result.Error
		if msg == "" {
			msg = "backend returned success=false with no error message"
		}
		return nil, fmt.Errorf("detection backend error: %s", msg)
	}

	objects := make([]DetectedObject, 0, len(result.Predictions))
	for _, p := range result.Predictions {
		objects = append(objects, DetectedObject{
			Label:      p.Label,
			Confidence: p.Confidence,
			BBox: BBox{
				XMin: float64(p.XMin) / w,
				YMin: float64(p.YMin) / h,
				XMax: float64(p.XMax) / w,
				YMax: float64(p.YMax) / h,
			},
		})
	}
	return objects, nil
}
