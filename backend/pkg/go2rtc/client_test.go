package go2rtc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// ---------- Streams (GET /api/streams) ----------

func TestStreams_Success(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/api/streams" {
			t.Errorf("path = %s, want /api/streams", r.URL.Path)
		}

		resp := map[string]*StreamInfo{
			"front_door": {
				Producers: []ProducerInfo{{URL: "rtsp://192.168.1.10:554/stream1"}},
				Consumers: []ConsumerInfo{{URL: "ws://browser"}},
			},
			"backyard": {
				Producers: []ProducerInfo{},
				Consumers: nil,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	streams, err := c.Streams(context.Background())
	if err != nil {
		t.Fatalf("Streams() error: %v", err)
	}
	if len(streams) != 2 {
		t.Fatalf("len(streams) = %d, want 2", len(streams))
	}

	fd := streams["front_door"]
	if fd == nil {
		t.Fatal("front_door stream is nil")
	}
	if len(fd.Producers) != 1 {
		t.Fatalf("front_door producers = %d, want 1", len(fd.Producers))
	}
	if fd.Producers[0].URL != "rtsp://192.168.1.10:554/stream1" {
		t.Errorf("producer URL = %q, want rtsp://192.168.1.10:554/stream1", fd.Producers[0].URL)
	}
	if len(fd.Consumers) != 1 {
		t.Fatalf("front_door consumers = %d, want 1", len(fd.Consumers))
	}

	by := streams["backyard"]
	if by == nil {
		t.Fatal("backyard stream is nil")
	}
	if len(by.Producers) != 0 {
		t.Errorf("backyard producers = %d, want 0", len(by.Producers))
	}
}

func TestStreams_EmptyMap(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, "{}")
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	streams, err := c.Streams(context.Background())
	if err != nil {
		t.Fatalf("Streams() error: %v", err)
	}
	if len(streams) != 0 {
		t.Errorf("len(streams) = %d, want 0", len(streams))
	}
}

func TestStreams_NilStreamInfo(t *testing.T) {
	t.Parallel()

	// go2rtc can return null for a stream value — JSON null decodes to nil *StreamInfo
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"camera1": null, "camera2": {"producers":[],"consumers":[]}}`)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	streams, err := c.Streams(context.Background())
	if err != nil {
		t.Fatalf("Streams() error: %v", err)
	}
	if len(streams) != 2 {
		t.Fatalf("len(streams) = %d, want 2", len(streams))
	}
	if streams["camera1"] != nil {
		t.Errorf("camera1 should be nil (JSON null), got %+v", streams["camera1"])
	}
	if streams["camera2"] == nil {
		t.Fatal("camera2 should not be nil")
	}
}

func TestStreams_ServerError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	_, err := c.Streams(context.Background())
	if err == nil {
		t.Fatal("Streams() should return error on 500")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should mention status 500: %v", err)
	}
}

func TestStreams_InvalidJSON(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, "NOT JSON")
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	_, err := c.Streams(context.Background())
	if err == nil {
		t.Fatal("Streams() should return error on invalid JSON")
	}
	if !strings.Contains(err.Error(), "decoding") {
		t.Errorf("error should mention decoding: %v", err)
	}
}

func TestStreams_ConnectionRefused(t *testing.T) {
	t.Parallel()

	// Use a server that's immediately closed — connection refused
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	srv.Close()

	c := NewClient(srv.URL)
	_, err := c.Streams(context.Background())
	if err == nil {
		t.Fatal("Streams() should return error when server is unreachable")
	}
}

func TestStreams_CancelledContext(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(5 * time.Second)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := c.Streams(ctx)
	if err == nil {
		t.Fatal("Streams() should return error on cancelled context")
	}
}

// ---------- AddStream (PUT /api/streams) ----------

func TestAddStream_Success(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("method = %s, want PUT", r.Method)
		}
		if r.URL.Path != "/api/streams" {
			t.Errorf("path = %s, want /api/streams", r.URL.Path)
		}
		name := r.URL.Query().Get("name")
		src := r.URL.Query().Get("src")
		if name != "front_door" {
			t.Errorf("name = %q, want %q", name, "front_door")
		}
		if src != "rtsp://admin:pass@192.168.1.10:554/stream1" {
			t.Errorf("src = %q, want expected RTSP URL", src)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	err := c.AddStream(context.Background(), "front_door", "rtsp://admin:pass@192.168.1.10:554/stream1")
	if err != nil {
		t.Fatalf("AddStream() error: %v", err)
	}
}

func TestAddStream_ReadOnlyConfig400(t *testing.T) {
	t.Parallel()

	// go2rtc returns 400 when config is :ro — stream IS in memory, treat as success
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "config is read-only", http.StatusBadRequest)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	err := c.AddStream(context.Background(), "cam1", "rtsp://ip/stream")
	if err != nil {
		t.Fatalf("AddStream() should succeed on 400 (read-only config), got: %v", err)
	}
}

func TestAddStream_ServerError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "go2rtc exploded", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	err := c.AddStream(context.Background(), "cam1", "rtsp://ip/stream")
	if err == nil {
		t.Fatal("AddStream() should return error on 500")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should mention status 500: %v", err)
	}
	if !strings.Contains(err.Error(), "cam1") {
		t.Errorf("error should mention stream name: %v", err)
	}
}

func TestAddStream_SpecialCharsInURL(t *testing.T) {
	t.Parallel()

	var receivedName, receivedSrc string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedName = r.URL.Query().Get("name")
		receivedSrc = r.URL.Query().Get("src")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	name := "cam with spaces & symbols"
	src := "rtsp://user:p@ss%word@10.0.0.1:554/h264?channel=1&subtype=0"
	err := c.AddStream(context.Background(), name, src)
	if err != nil {
		t.Fatalf("AddStream() error: %v", err)
	}
	if receivedName != name {
		t.Errorf("received name = %q, want %q", receivedName, name)
	}
	if receivedSrc != src {
		t.Errorf("received src = %q, want %q", receivedSrc, src)
	}
}

func TestAddStream_ConnectionRefused(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	srv.Close()

	c := NewClient(srv.URL)
	err := c.AddStream(context.Background(), "cam1", "rtsp://ip/stream")
	if err == nil {
		t.Fatal("AddStream() should return error when server is unreachable")
	}
}

// ---------- RemoveStream (DELETE /api/streams) ----------

func TestRemoveStream_Success(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("method = %s, want DELETE", r.Method)
		}
		if r.URL.Path != "/api/streams" {
			t.Errorf("path = %s, want /api/streams", r.URL.Path)
		}
		// go2rtc uses "src" param for delete (not "name")
		src := r.URL.Query().Get("src")
		if src != "front_door" {
			t.Errorf("src = %q, want %q", src, "front_door")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	err := c.RemoveStream(context.Background(), "front_door")
	if err != nil {
		t.Fatalf("RemoveStream() error: %v", err)
	}
}

func TestRemoveStream_DynamicStream400(t *testing.T) {
	t.Parallel()

	// go2rtc returns 400 for dynamic streams — cosmetic, stream still removed.
	// Unlike AddStream, RemoveStream does NOT treat 400 as success.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "dynamic stream", http.StatusBadRequest)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	err := c.RemoveStream(context.Background(), "dynamic_cam")
	if err == nil {
		t.Fatal("RemoveStream() should return error on 400")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("error should mention status 400: %v", err)
	}
}

func TestRemoveStream_ServerError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "failure", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	err := c.RemoveStream(context.Background(), "cam1")
	if err == nil {
		t.Fatal("RemoveStream() should return error on 500")
	}
}

func TestRemoveStream_ConnectionRefused(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	srv.Close()

	c := NewClient(srv.URL)
	err := c.RemoveStream(context.Background(), "cam1")
	if err == nil {
		t.Fatal("RemoveStream() should return error when server is unreachable")
	}
}

// ---------- FrameJPEG (GET /api/frame.jpeg) ----------

func TestFrameJPEG_Success(t *testing.T) {
	t.Parallel()

	fakeJPEG := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10} // JPEG header bytes

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/api/frame.jpeg" {
			t.Errorf("path = %s, want /api/frame.jpeg", r.URL.Path)
		}
		src := r.URL.Query().Get("src")
		if src != "front_door" {
			t.Errorf("src = %q, want %q", src, "front_door")
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(fakeJPEG)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	data, err := c.FrameJPEG(context.Background(), "front_door")
	if err != nil {
		t.Fatalf("FrameJPEG() error: %v", err)
	}
	if len(data) != len(fakeJPEG) {
		t.Errorf("len(data) = %d, want %d", len(data), len(fakeJPEG))
	}
	for i, b := range data {
		if b != fakeJPEG[i] {
			t.Errorf("data[%d] = %x, want %x", i, b, fakeJPEG[i])
			break
		}
	}
}

func TestFrameJPEG_EmptyBody(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.WriteHeader(http.StatusOK)
		// Write nothing — empty body
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	_, err := c.FrameJPEG(context.Background(), "cam1")
	if err == nil {
		t.Fatal("FrameJPEG() should return error on empty response body")
	}
	if !strings.Contains(err.Error(), "empty response body") {
		t.Errorf("error should mention empty body: %v", err)
	}
}

func TestFrameJPEG_NotFound(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "stream not found", http.StatusNotFound)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	_, err := c.FrameJPEG(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("FrameJPEG() should return error on 404")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("error should mention status 404: %v", err)
	}
}

func TestFrameJPEG_ServerError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	_, err := c.FrameJPEG(context.Background(), "cam1")
	if err == nil {
		t.Fatal("FrameJPEG() should return error on 500")
	}
}

func TestFrameJPEG_ConnectionRefused(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	srv.Close()

	c := NewClient(srv.URL)
	_, err := c.FrameJPEG(context.Background(), "cam1")
	if err == nil {
		t.Fatal("FrameJPEG() should return error when server is unreachable")
	}
}

// ---------- Health (GET /api/streams) ----------

func TestHealth_Success(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/api/streams" {
			t.Errorf("path = %s, want /api/streams", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	if err := c.Health(context.Background()); err != nil {
		t.Fatalf("Health() error: %v", err)
	}
}

func TestHealth_ServerError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	err := c.Health(context.Background())
	if err == nil {
		t.Fatal("Health() should return error on non-200")
	}
	if !strings.Contains(err.Error(), "503") {
		t.Errorf("error should mention status 503: %v", err)
	}
}

func TestHealth_ConnectionRefused(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	srv.Close()

	c := NewClient(srv.URL)
	if err := c.Health(context.Background()); err == nil {
		t.Fatal("Health() should return error when server is unreachable")
	}
}

// ---------- WaitReady ----------

func TestWaitReady_ImmediateSuccess(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := c.WaitReady(ctx); err != nil {
		t.Fatalf("WaitReady() error: %v", err)
	}
}

func TestWaitReady_EventualSuccess(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := calls.Add(1)
		if n < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := c.WaitReady(ctx); err != nil {
		t.Fatalf("WaitReady() error: %v", err)
	}
	if n := calls.Load(); n < 3 {
		t.Errorf("expected at least 3 calls, got %d", n)
	}
}

func TestWaitReady_ContextCancelled(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	err := c.WaitReady(ctx)
	if err == nil {
		t.Fatal("WaitReady() should return error when context expires")
	}
	if !strings.Contains(err.Error(), "not ready") {
		t.Errorf("error should mention 'not ready': %v", err)
	}
}

// ---------- NewClient ----------

func TestNewClient(t *testing.T) {
	t.Parallel()

	c := NewClient("http://localhost:1984")
	if c == nil {
		t.Fatal("NewClient returned nil")
	}
	if c.baseURL != "http://localhost:1984" {
		t.Errorf("baseURL = %q, want %q", c.baseURL, "http://localhost:1984")
	}
	if c.httpClient == nil {
		t.Fatal("httpClient is nil")
	}
	if c.httpClient.Timeout != 10*time.Second {
		t.Errorf("Timeout = %v, want 10s", c.httpClient.Timeout)
	}
}

// ---------- Table-driven: HTTP status code handling ----------

func TestAddStream_StatusCodes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		status     int
		wantErr    bool
		errContain string
	}{
		{"200 OK", http.StatusOK, false, ""},
		{"400 read-only config treated as success", http.StatusBadRequest, false, ""},
		{"403 forbidden", http.StatusForbidden, true, "403"},
		{"404 not found", http.StatusNotFound, true, "404"},
		{"500 server error", http.StatusInternalServerError, true, "500"},
		{"502 bad gateway", http.StatusBadGateway, true, "502"},
		{"503 unavailable", http.StatusServiceUnavailable, true, "503"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
			}))
			defer srv.Close()

			c := NewClient(srv.URL)
			err := c.AddStream(context.Background(), "cam1", "rtsp://ip/stream")
			if tt.wantErr && err == nil {
				t.Fatalf("AddStream() should return error for status %d", tt.status)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("AddStream() unexpected error for status %d: %v", tt.status, err)
			}
			if tt.errContain != "" && !strings.Contains(err.Error(), tt.errContain) {
				t.Errorf("error %q should contain %q", err.Error(), tt.errContain)
			}
		})
	}
}

func TestRemoveStream_StatusCodes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		status     int
		wantErr    bool
		errContain string
	}{
		{"200 OK", http.StatusOK, false, ""},
		{"400 dynamic stream", http.StatusBadRequest, true, "400"},
		{"404 not found", http.StatusNotFound, true, "404"},
		{"500 server error", http.StatusInternalServerError, true, "500"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
			}))
			defer srv.Close()

			c := NewClient(srv.URL)
			err := c.RemoveStream(context.Background(), "cam1")
			if tt.wantErr && err == nil {
				t.Fatalf("RemoveStream() should return error for status %d", tt.status)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("RemoveStream() unexpected error for status %d: %v", tt.status, err)
			}
			if tt.errContain != "" && !strings.Contains(err.Error(), tt.errContain) {
				t.Errorf("error %q should contain %q", err.Error(), tt.errContain)
			}
		})
	}
}

func TestFrameJPEG_StatusCodes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		status     int
		body       []byte
		wantErr    bool
		errContain string
	}{
		{"200 OK with data", http.StatusOK, []byte{0xFF, 0xD8}, false, ""},
		{"200 OK empty body", http.StatusOK, nil, true, "empty"},
		{"404 not found", http.StatusNotFound, nil, true, "404"},
		{"500 server error", http.StatusInternalServerError, nil, true, "500"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if tt.status != http.StatusOK {
					http.Error(w, "error", tt.status)
					return
				}
				w.Header().Set("Content-Type", "image/jpeg")
				w.WriteHeader(tt.status)
				if tt.body != nil {
					w.Write(tt.body)
				}
			}))
			defer srv.Close()

			c := NewClient(srv.URL)
			data, err := c.FrameJPEG(context.Background(), "cam1")
			if tt.wantErr && err == nil {
				t.Fatalf("FrameJPEG() should return error for %s", tt.name)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("FrameJPEG() unexpected error: %v", err)
			}
			if tt.errContain != "" && !strings.Contains(err.Error(), tt.errContain) {
				t.Errorf("error %q should contain %q", err.Error(), tt.errContain)
			}
			if !tt.wantErr && len(data) != len(tt.body) {
				t.Errorf("len(data) = %d, want %d", len(data), len(tt.body))
			}
		})
	}
}

// ---------- Edge cases ----------

func TestAddStream_EmptyName(t *testing.T) {
	t.Parallel()

	var receivedName string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedName = r.URL.Query().Get("name")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	err := c.AddStream(context.Background(), "", "rtsp://ip/stream")
	if err != nil {
		t.Fatalf("AddStream() error: %v", err)
	}
	if receivedName != "" {
		t.Errorf("received name = %q, want empty", receivedName)
	}
}

func TestRemoveStream_EmptyName(t *testing.T) {
	t.Parallel()

	var receivedSrc string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedSrc = r.URL.Query().Get("src")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	err := c.RemoveStream(context.Background(), "")
	if err != nil {
		t.Fatalf("RemoveStream() error: %v", err)
	}
	if receivedSrc != "" {
		t.Errorf("received src = %q, want empty", receivedSrc)
	}
}

func TestFrameJPEG_LargeImage(t *testing.T) {
	t.Parallel()

	// Simulate a realistic-ish JPEG (512 KB)
	fakeJPEG := make([]byte, 512*1024)
	fakeJPEG[0] = 0xFF
	fakeJPEG[1] = 0xD8

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(fakeJPEG)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	data, err := c.FrameJPEG(context.Background(), "cam1")
	if err != nil {
		t.Fatalf("FrameJPEG() error: %v", err)
	}
	if len(data) != len(fakeJPEG) {
		t.Errorf("len(data) = %d, want %d", len(data), len(fakeJPEG))
	}
}

func TestStreams_ProducerMediaType(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"cam1": {
				"producers": [{"url":"rtsp://ip/s1","media_type":"video/h264"}],
				"consumers": [{"url":"ws://a","media_type":"video/h264"}]
			}
		}`)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	streams, err := c.Streams(context.Background())
	if err != nil {
		t.Fatalf("Streams() error: %v", err)
	}
	si := streams["cam1"]
	if si == nil {
		t.Fatal("cam1 is nil")
	}
	if si.Producers[0].MediaType != "video/h264" {
		t.Errorf("producer MediaType = %q, want %q", si.Producers[0].MediaType, "video/h264")
	}
	if si.Consumers[0].MediaType != "video/h264" {
		t.Errorf("consumer MediaType = %q, want %q", si.Consumers[0].MediaType, "video/h264")
	}
}

// ---------- Verify query param encoding ----------

func TestAddStream_QueryParamEncoding(t *testing.T) {
	t.Parallel()

	var rawQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	err := c.AddStream(context.Background(), "my camera", "rtsp://user:p@ss@10.0.0.1/stream")
	if err != nil {
		t.Fatalf("AddStream() error: %v", err)
	}

	// Verify the query string was properly URL-encoded
	if !strings.Contains(rawQuery, "name=my+camera") && !strings.Contains(rawQuery, "name=my%20camera") {
		t.Errorf("name not properly encoded in query: %s", rawQuery)
	}
	if !strings.Contains(rawQuery, "src=") {
		t.Errorf("src param missing from query: %s", rawQuery)
	}
}

func TestRemoveStream_UsesSrcParam(t *testing.T) {
	t.Parallel()

	// Verify RemoveStream uses "src" not "name" — important go2rtc API detail
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("name") != "" {
			t.Error("RemoveStream should use 'src' param, not 'name'")
		}
		src := r.URL.Query().Get("src")
		if src != "my_camera" {
			t.Errorf("src = %q, want %q", src, "my_camera")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	err := c.RemoveStream(context.Background(), "my_camera")
	if err != nil {
		t.Fatalf("RemoveStream() error: %v", err)
	}
}

// ---------- drainAndClose coverage (via concurrent use) ----------

func TestConcurrentRequests(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/streams":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"cam1":{"producers":[],"consumers":[]}}`)
		case r.Method == http.MethodPut:
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodGet && r.URL.Path == "/api/frame.jpeg":
			w.Header().Set("Content-Type", "image/jpeg")
			w.Write([]byte{0xFF, 0xD8, 0xFF})
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	ctx := context.Background()

	const goroutines = 10
	errs := make(chan error, goroutines*3)

	for i := 0; i < goroutines; i++ {
		go func() {
			_, err := c.Streams(ctx)
			errs <- err
		}()
		go func(n int) {
			errs <- c.AddStream(ctx, fmt.Sprintf("cam%d", n), "rtsp://ip/stream")
		}(i)
		go func() {
			_, err := c.FrameJPEG(ctx, "cam1")
			errs <- err
		}()
	}

	for i := 0; i < goroutines*3; i++ {
		if err := <-errs; err != nil {
			t.Errorf("concurrent request error: %v", err)
		}
	}
}
