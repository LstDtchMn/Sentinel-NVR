package detection

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"time"

	"github.com/LstDtchMn/Sentinel-NVR/backend/internal/config"
)

// Startable is an optional interface implemented by detectors that manage
// subprocess or connection lifecycle. The main binary checks for it after
// calling NewDetector and wires up Start/Stop around the server lifetime.
type Startable interface {
	Start(ctx context.Context) error
	Stop()
}

// LocalDetector manages the sentinel-infer subprocess and delegates Detect
// calls to a RemoteDetector pointed at the local inference server (CG10, R3).
//
// The sentinel-infer binary is a separate Go module that links ONNX Runtime via
// CGo — keeping the main sentinel binary pure Go and cross-compilable (R1).
// LocalDetector wraps RemoteDetector so that the Detector interface is unchanged
// and all codec/NMS logic lives in the infer binary, not the main process.
type LocalDetector struct {
	cfg     *config.DetectionConfig
	remote  *RemoteDetector
	mu      sync.Mutex // guards cmd; Start() writes, Stop() reads concurrently
	cmd     *exec.Cmd
	logger  *slog.Logger
	port    int
	binPath string // resolved at construction time; used by Start()
}

// Compile-time assertion: LocalDetector implements both Detector and Startable.
var _ Detector = (*LocalDetector)(nil)
var _ Startable = (*LocalDetector)(nil)

// NewLocalDetector creates a LocalDetector from the detection configuration.
// Does not start the subprocess — call Start() before using Detect().
func NewLocalDetector(cfg *config.DetectionConfig, logger *slog.Logger) (*LocalDetector, error) {
	if cfg.Model == "" {
		return nil, fmt.Errorf("detection.model path is required when backend=onnx")
	}
	binPath, err := resolveInferBinary(cfg.InferenceBinary)
	if err != nil {
		return nil, fmt.Errorf("locating sentinel-infer binary: %w", err)
	}
	logger.Info("resolved sentinel-infer binary", "path", binPath)

	return &LocalDetector{
		cfg:     cfg,
		logger:  logger.With("component", "local_detector"),
		port:    cfg.InferencePort,
		binPath: binPath,
	}, nil
}

// Start launches the sentinel-infer subprocess and waits until its /health
// endpoint responds (up to 30 seconds). Returns an error if the subprocess
// exits early or does not become healthy within the timeout.
func (l *LocalDetector) Start(ctx context.Context) error {
	binPath := l.binPath

	threshold := fmt.Sprintf("%.3f", l.cfg.ConfidenceThresholdValue())
	libPath := resolveORTLibPath()

	// Build command using a local variable; assign to l.cmd under the mutex
	// so Stop() (which may be called concurrently) sees a consistent value.
	cmd := exec.CommandContext(ctx, binPath, // nolint:gosec — binary path is config-controlled
		"-port", strconv.Itoa(l.port),
		"-model", l.cfg.Model,
		"-lib", libPath,
		"-threshold", threshold,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting sentinel-infer: %w", err)
	}

	l.mu.Lock()
	l.cmd = cmd
	l.mu.Unlock()

	l.logger.Info("sentinel-infer started", "pid", cmd.Process.Pid, "port", l.port)

	// Poll /health until it responds 200 or the timeout expires.
	healthURL := fmt.Sprintf("http://127.0.0.1:%d/health", l.port)
	deadline := time.Now().Add(30 * time.Second)
	client := &http.Client{Timeout: time.Second}
	healthy := false
	for time.Now().Before(deadline) {
		// Check if the subprocess has already exited (crash on startup).
		select {
		case <-ctx.Done():
			cmd.Process.Kill()
			return ctx.Err()
		default:
		}

		resp, err := client.Get(healthURL) // nolint:noctx — short-lived health poll, no user data
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			l.logger.Info("sentinel-infer healthy", "url", healthURL)
			healthy = true
			break
		}
		if resp != nil {
			resp.Body.Close()
		}
		// Check if the subprocess has already exited to avoid waiting 30s unnecessarily.
		// ProcessState is set after cmd.Wait() returns. If it is already set,
		// the process died and Wait was called; this is a partial check only.
		if cmd.ProcessState != nil {
			return fmt.Errorf("sentinel-infer exited during startup: %v", cmd.ProcessState)
		}
		time.Sleep(500 * time.Millisecond)
	}

	// Only run the final verification if the loop exited via deadline (not break).
	if !healthy {
		resp, err := client.Get(healthURL)
		if err != nil {
			cmd.Process.Kill()
			return fmt.Errorf("sentinel-infer did not become healthy within 30s: %w", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			cmd.Process.Kill()
			return fmt.Errorf("sentinel-infer /health returned %d", resp.StatusCode)
		}
	}

	l.remote = NewRemoteDetector(fmt.Sprintf("http://127.0.0.1:%d", l.port), l.logger)
	return nil
}

// Detect delegates to the RemoteDetector. Must be called after Start().
func (l *LocalDetector) Detect(ctx context.Context, jpegBytes []byte) ([]DetectedObject, error) {
	if l.remote == nil {
		return nil, fmt.Errorf("LocalDetector.Start() has not been called")
	}
	return l.remote.Detect(ctx, jpegBytes)
}

// Stop sends SIGINT to the sentinel-infer subprocess and waits for it to exit.
// Safe to call even if Start() failed (cmd may be nil).
func (l *LocalDetector) Stop() {
	l.mu.Lock()
	cmd := l.cmd
	l.mu.Unlock()

	if cmd == nil || cmd.Process == nil {
		return
	}
	l.logger.Info("stopping sentinel-infer", "pid", cmd.Process.Pid)
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		// Process may have already exited; fall through to Wait.
		l.logger.Debug("sending SIGINT to sentinel-infer failed", "error", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		l.logger.Warn("sentinel-infer did not stop within 5s, killing")
		cmd.Process.Kill()
	}
}

// resolveInferBinary returns the path to the sentinel-infer binary.
// Priority: 1. explicit cfg value  2. same directory as this process  3. PATH
func resolveInferBinary(configured string) (string, error) {
	// 1. Explicit path from config (e.g. /usr/local/bin/sentinel-infer).
	if configured != "" {
		if _, err := os.Stat(configured); err == nil {
			return configured, nil
		}
	}

	// 2. Same directory as the currently running sentinel binary.
	exe, err := os.Executable()
	if err == nil {
		candidate := filepath.Join(filepath.Dir(exe), inferBinaryName())
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}

	// 3. PATH lookup.
	if path, err := exec.LookPath(inferBinaryName()); err == nil {
		return path, nil
	}

	return "", fmt.Errorf("sentinel-infer not found at %q, next to sentinel binary, or in PATH", configured)
}

func inferBinaryName() string {
	if runtime.GOOS == "windows" {
		return "sentinel-infer.exe"
	}
	return "sentinel-infer"
}

// resolveORTLibPath returns the expected path for the ONNX Runtime shared library.
// On Linux (Docker) this is the path installed by the Dockerfile.
// On other platforms the flag default passes through from main().
func resolveORTLibPath() string {
	if runtime.GOOS == "linux" {
		return "/usr/local/lib/libonnxruntime.so.1.18.1"
	}
	// On macOS / Windows, callers must set detection.inference_binary
	// explicitly or ensure onnxruntime is on the system library path.
	return "libonnxruntime.so.1.18.1"
}
