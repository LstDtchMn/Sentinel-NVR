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

// RemoteAudioClassifier calls the sentinel-infer /v1/audio/classify HTTP endpoint
// to classify audio events (glass break, dog bark, baby cry, etc.) from PCM data.
// Follows the same pattern as RemoteFaceRecognizer for consistency (R12, CG10).
type RemoteAudioClassifier struct {
	url    string
	client *http.Client
	logger *slog.Logger
}

// Compile-time assertion: RemoteAudioClassifier implements AudioClassifier.
var _ AudioClassifier = (*RemoteAudioClassifier)(nil)

// NewRemoteAudioClassifier creates an audio classifier backed by sentinel-infer.
func NewRemoteAudioClassifier(baseURL string, logger *slog.Logger) *RemoteAudioClassifier {
	return &RemoteAudioClassifier{
		url: baseURL + "/v1/audio/classify",
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		logger: logger.With("component", "audio_classifier"),
	}
}

// audioClassifyResponse is the JSON response from sentinel-infer /v1/audio/classify.
type audioClassifyResponse struct {
	Success         bool             `json:"success"`
	Classifications []AudioDetection `json:"classifications"`
	Error           string           `json:"error,omitempty"`
}

// Classify sends PCM16 mono 16kHz audio data to sentinel-infer and returns classifications.
func (c *RemoteAudioClassifier) Classify(ctx context.Context, pcmData []byte) ([]AudioDetection, error) {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	fw, err := w.CreateFormFile("audio", "sample.pcm")
	if err != nil {
		return nil, fmt.Errorf("creating form file: %w", err)
	}
	if _, err := fw.Write(pcmData); err != nil {
		return nil, fmt.Errorf("writing audio data: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("closing multipart writer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, &body)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("audio classify request: %w", err)
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
		return nil, fmt.Errorf("audio classify HTTP %d: %s", resp.StatusCode, string(b))
	}

	var result audioClassifyResponse
	if err := json.Unmarshal(b, &result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	if !result.Success {
		return nil, fmt.Errorf("audio classify failed: %s", result.Error)
	}

	return result.Classifications, nil
}
