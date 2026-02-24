package detection

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"time"
)

// FaceRecognizer extracts face embeddings from a JPEG image.
// Implementations must be safe for concurrent use.
type FaceRecognizer interface {
	// EmbedFaces extracts face embeddings from a JPEG image, optionally cropped to
	// a bounding box region. Returns zero or more face embeddings.
	EmbedFaces(ctx context.Context, jpegBytes []byte, maxFaces int) ([]FaceEmbedding, error)
}

// FaceEmbedding is a single face detected and embedded from an image.
type FaceEmbedding struct {
	Embedding []float32 `json:"embedding"` // 512-dim ArcFace vector
	BBox      BBox      `json:"bbox"`      // face location in normalised coords
}

// RemoteFaceRecognizer calls the sentinel-infer /v1/face/embed HTTP endpoint.
type RemoteFaceRecognizer struct {
	url    string
	client *http.Client
	logger *slog.Logger
}

// NewRemoteFaceRecognizer creates a face recognizer backed by sentinel-infer.
func NewRemoteFaceRecognizer(baseURL string, logger *slog.Logger) *RemoteFaceRecognizer {
	return &RemoteFaceRecognizer{
		url: baseURL + "/v1/face/embed",
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
		logger: logger.With("component", "face_recognizer"),
	}
}

// embedResponse is the JSON response from sentinel-infer /v1/face/embed.
type embedResponse struct {
	Success bool            `json:"success"`
	Faces   []faceResult    `json:"faces"`
	Error   string          `json:"error,omitempty"`
}

type faceResult struct {
	Embedding []float32 `json:"embedding"`
	XMin      float64   `json:"x_min"`
	YMin      float64   `json:"y_min"`
	XMax      float64   `json:"x_max"`
	YMax      float64   `json:"y_max"`
}

// EmbedFaces sends a JPEG to sentinel-infer and returns face embeddings.
func (r *RemoteFaceRecognizer) EmbedFaces(ctx context.Context, jpegBytes []byte, maxFaces int) ([]FaceEmbedding, error) {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	fw, err := w.CreateFormFile("image", "frame.jpg")
	if err != nil {
		return nil, fmt.Errorf("creating form file: %w", err)
	}
	if _, err := fw.Write(jpegBytes); err != nil {
		return nil, fmt.Errorf("writing image data: %w", err)
	}
	// Write max_faces field so sentinel-infer can limit processing.
	if err := w.WriteField("max_faces", fmt.Sprintf("%d", maxFaces)); err != nil {
		return nil, fmt.Errorf("writing max_faces field: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("closing multipart writer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.url, &body)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("face embed request: %w", err)
	}
	defer func() {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("face embed HTTP %d: %s", resp.StatusCode, string(b))
	}

	var result embedResponse
	if err := json.Unmarshal(b, &result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	if !result.Success {
		return nil, fmt.Errorf("face embed failed: %s", result.Error)
	}

	embeddings := make([]FaceEmbedding, 0, len(result.Faces))
	for _, f := range result.Faces {
		embeddings = append(embeddings, FaceEmbedding{
			Embedding: f.Embedding,
			BBox: BBox{
				XMin: f.XMin,
				YMin: f.YMin,
				XMax: f.XMax,
				YMax: f.YMax,
			},
		})
		if len(embeddings) >= maxFaces {
			break
		}
	}
	return embeddings, nil
}
