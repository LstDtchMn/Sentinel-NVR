// Package go2rtc provides a REST API client for communicating with a go2rtc
// sidecar instance. It handles stream registration, removal, and health checks.
package go2rtc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Client communicates with a go2rtc instance via its REST API.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// StreamInfo represents a go2rtc stream and its producer/consumer state.
type StreamInfo struct {
	Producers []ProducerInfo `json:"producers"`
	Consumers []ConsumerInfo `json:"consumers"`
}

// ProducerInfo describes an active source feeding a stream.
type ProducerInfo struct {
	URL       string `json:"url"`
	MediaType string `json:"media_type,omitempty"`
}

// ConsumerInfo describes an active client consuming a stream.
type ConsumerInfo struct {
	URL       string `json:"url"`
	MediaType string `json:"media_type,omitempty"`
}

// drainAndClose reads any remaining body bytes (so the HTTP connection can be reused)
// and then closes the body. Always use this instead of bare resp.Body.Close().
func drainAndClose(resp *http.Response) {
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
}

// NewClient creates a go2rtc API client.
// baseURL is the go2rtc API address, e.g. "http://go2rtc:1984".
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Streams returns all currently configured streams from go2rtc.
func (c *Client) Streams(ctx context.Context) (map[string]*StreamInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/streams", nil)
	if err != nil {
		return nil, fmt.Errorf("go2rtc: creating request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("go2rtc: listing streams: %w", err)
	}
	defer drainAndClose(resp)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("go2rtc: list streams returned %d: %s", resp.StatusCode, body)
	}

	var streams map[string]*StreamInfo
	if err := json.NewDecoder(resp.Body).Decode(&streams); err != nil {
		return nil, fmt.Errorf("go2rtc: decoding streams response: %w", err)
	}
	return streams, nil
}

// AddStream registers a named RTSP source stream in go2rtc.
// name is the logical stream identifier (e.g. camera name).
// src is the source URL (e.g. "rtsp://user:pass@ip:554/stream1").
func (c *Client) AddStream(ctx context.Context, name, src string) error {
	u := c.baseURL + "/api/streams?" + url.Values{
		"name": {name},
		"src":  {src},
	}.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, u, nil)
	if err != nil {
		return fmt.Errorf("go2rtc: creating add request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("go2rtc: adding stream %q: %w", name, err)
	}
	defer drainAndClose(resp)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("go2rtc: add stream %q returned %d: %s", name, resp.StatusCode, body)
	}
	return nil
}

// RemoveStream removes a named stream from go2rtc.
func (c *Client) RemoveStream(ctx context.Context, name string) error {
	u := c.baseURL + "/api/streams?" + url.Values{
		"src": {name},
	}.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, u, nil)
	if err != nil {
		return fmt.Errorf("go2rtc: creating delete request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("go2rtc: removing stream %q: %w", name, err)
	}
	defer drainAndClose(resp)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("go2rtc: remove stream %q returned %d: %s", name, resp.StatusCode, body)
	}
	return nil
}

// Health checks whether go2rtc is responding to API requests.
func (c *Client) Health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/streams", nil)
	if err != nil {
		return fmt.Errorf("go2rtc: creating health request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("go2rtc: health check failed: %w", err)
	}
	defer drainAndClose(resp)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("go2rtc: health check returned %d", resp.StatusCode)
	}
	return nil
}

// WaitReady polls Health() with exponential backoff until go2rtc is reachable
// or the context is cancelled. Backoff starts at 100ms and caps at 2s.
func (c *Client) WaitReady(ctx context.Context) error {
	backoff := 100 * time.Millisecond
	maxBackoff := 2 * time.Second

	for {
		if err := c.Health(ctx); err == nil {
			return nil
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("go2rtc: not ready: %w", ctx.Err())
		case <-time.After(backoff):
		}

		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}
