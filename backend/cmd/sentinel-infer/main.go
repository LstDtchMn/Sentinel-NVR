// sentinel-infer: CodeProject.AI-compatible ONNX inference HTTP server (R3, CG10).
//
// Exposes two endpoints:
//
//	GET  /health                → {"status":"ok","model":"<filename>"}
//	POST /v1/vision/detection   → multipart/form-data field "image" (JPEG) → predictions JSON
//
// Response format is intentionally identical to CodeProject.AI so that the
// main sentinel binary's RemoteDetector can point at this server unchanged.
//
// Usage:
//
//	sentinel-infer -port 9099 -model /data/models/general_security.onnx \
//	               -lib /usr/local/lib/libonnxruntime.so.1.18.1 -threshold 0.5
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/LstDtchMn/Sentinel-NVR/sentinel-infer/model"
)

func main() {
	port      := flag.Int("port", 9099, "HTTP port to listen on")
	modelPath := flag.String("model", "", "path to ONNX model file (required)")
	libPath   := flag.String("lib", "/usr/local/lib/libonnxruntime.so.1.18.1", "path to libonnxruntime shared library")
	threshold := flag.Float64("threshold", 0.5, "confidence threshold [0.0-1.0]")
	flag.Parse()

	if *modelPath == "" {
		log.Fatal("sentinel-infer: -model is required")
	}
	if _, err := os.Stat(*modelPath); err != nil {
		log.Fatalf("sentinel-infer: model file not found: %v", err)
	}

	log.Printf("sentinel-infer: loading model %q (threshold %.2f)", *modelPath, *threshold)
	m, err := model.NewYOLOv8Model(*libPath, *modelPath, float32(*threshold))
	if err != nil {
		log.Fatalf("sentinel-infer: failed to load model: %v", err)
	}
	defer m.Close()
	log.Printf("sentinel-infer: model loaded (%s)", m.ModelName())

	mux := http.NewServeMux()
	mux.HandleFunc("/health", healthHandler(m))
	mux.HandleFunc("/v1/vision/detection", detectionHandler(m))
	mux.HandleFunc("/v1/face/embed", faceEmbedHandler())        // Phase 13 stub (R11)
	mux.HandleFunc("/v1/audio/classify", audioClassifyHandler()) // Phase 13 stub (R12)

	srv := &http.Server{
		Addr:    fmt.Sprintf("127.0.0.1:%d", *port),
		Handler: mux,
	}

	// Shutdown on SIGINT/SIGTERM.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-quit
		log.Println("sentinel-infer: shutting down")
		srv.Close()
	}()

	log.Printf("sentinel-infer: listening on 127.0.0.1:%d", *port)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("sentinel-infer: server error: %v", err)
	}
}

// healthResponse is the JSON payload for GET /health.
type healthResponse struct {
	Status string `json:"status"`
	Model  string `json:"model"`
}

func healthHandler(m *model.YOLOv8Model) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(healthResponse{Status: "ok", Model: m.ModelName()})
	}
}

// detectionResponse mirrors the CodeProject.AI detection response format so that
// RemoteDetector (already implemented) can parse it without modification.
type detectionResponse struct {
	Success     bool                    `json:"success"`
	Predictions []predictionResponse    `json:"predictions"`
	Error       string                  `json:"error,omitempty"`
}

// predictionResponse is a single bounding box in absolute pixel coordinates.
// Field names match CodeProject.AI exactly (x_min / y_min / x_max / y_max).
type predictionResponse struct {
	Label      string  `json:"label"`
	Confidence float32 `json:"confidence"`
	XMin       int     `json:"x_min"`
	YMin       int     `json:"y_min"`
	XMax       int     `json:"x_max"`
	YMax       int     `json:"y_max"`
}

func detectionHandler(m *model.YOLOv8Model) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Explicit method check rather than Go 1.22 "POST /path" ServeMux syntax keeps
		// the 405 response body consistent with the rest of this handler's JSON error
		// format (net/http's built-in 405 handler returns plain text, not JSON).
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			json.NewEncoder(w).Encode(detectionResponse{Error: "POST required"})
			return
		}

		// Limit upload size to 10 MB — a 4K JPEG is typically < 2 MB.
		r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(detectionResponse{Error: "parsing multipart: " + err.Error()})
			return
		}

		file, _, err := r.FormFile("image")
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(detectionResponse{Error: "missing 'image' field: " + err.Error()})
			return
		}
		defer file.Close()

		jpegBytes, err := readFormFile(file)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(detectionResponse{Error: "reading image: " + err.Error()})
			return
		}
		if len(jpegBytes) == 0 {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(detectionResponse{Error: "image field is empty"})
			return
		}

		dets, err := m.Detect(jpegBytes)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(detectionResponse{Success: false, Error: err.Error()})
			return
		}

		predictions := make([]predictionResponse, 0, len(dets))
		for _, d := range dets {
			predictions = append(predictions, predictionResponse{
				Label:      d.Label,
				Confidence: d.Confidence,
				XMin:       d.BBox.XMin,
				YMin:       d.BBox.YMin,
				XMax:       d.BBox.XMax,
				YMax:       d.BBox.YMax,
			})
		}

		json.NewEncoder(w).Encode(detectionResponse{Success: true, Predictions: predictions})
	}
}

// readFormFile reads the uploaded file into memory.
// Using a separate function keeps the handler readable and isolates the I/O.
func readFormFile(f multipart.File) ([]byte, error) {
	return io.ReadAll(f)
}

// ─── Phase 13 stubs (R11 face recognition, R12 audio classification) ────────
//
// These endpoints are stub implementations that return "not implemented" until
// the ArcFace and YAMNet ONNX models are integrated into the model package.
// The main sentinel binary's RemoteFaceRecognizer and AudioClassifier are coded
// against the expected request/response format documented below.

// faceEmbedHandler is a stub for POST /v1/face/embed.
// Expected request: multipart/form-data with "image" (JPEG) and "max_faces" (int) fields.
// Expected response: {"success":true,"faces":[{"embedding":[...512 floats...],"x_min":0.1,...}]}
func faceEmbedHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			json.NewEncoder(w).Encode(map[string]any{"success": false, "error": "POST required"})
			return
		}
		// TODO(phase13): load ArcFace ONNX model, detect faces in the uploaded image,
		// extract 512-dim embeddings, return bounding boxes + embeddings.
		w.WriteHeader(http.StatusNotImplemented)
		json.NewEncoder(w).Encode(map[string]any{
			"success": false,
			"error":   "face embedding not yet implemented — ArcFace ONNX model required",
			"faces":   []any{},
		})
	}
}

// audioClassifyHandler is a stub for POST /v1/audio/classify.
// Expected request: multipart/form-data with "audio" (PCM16 mono 16kHz) field.
// Expected response: {"success":true,"classifications":[{"label":"glass_break","confidence":0.92}]}
func audioClassifyHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			json.NewEncoder(w).Encode(map[string]any{"success": false, "error": "POST required"})
			return
		}
		// TODO(phase13): load YAMNet ONNX model, classify audio classes
		// (glass_break, dog_bark, baby_cry, gunshot, smoke_alarm, etc.).
		w.WriteHeader(http.StatusNotImplemented)
		json.NewEncoder(w).Encode(map[string]any{
			"success":         false,
			"error":           "audio classification not yet implemented — YAMNet ONNX model required",
			"classifications": []any{},
		})
	}
}
