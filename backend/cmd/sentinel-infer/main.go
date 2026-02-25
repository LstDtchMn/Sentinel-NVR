// sentinel-infer: CodeProject.AI-compatible ONNX inference HTTP server (R3, CG10, R11).
//
// Exposes the following endpoints:
//
//	GET  /health                → {"status":"ok","model":"<filename>"}
//	POST /v1/vision/detection   → multipart "image" (JPEG) → detection predictions JSON
//	POST /v1/face/embed         → multipart "image" (JPEG) + "max_faces" → face embeddings JSON
//
// Response format is intentionally identical to CodeProject.AI so that the
// main sentinel binary's RemoteDetector can point at this server unchanged.
//
// Usage:
//
//	sentinel-infer -port 9099 -model /data/models/general_security.onnx \
//	               -face-model /data/models/face_recognition.onnx \
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
	port          := flag.Int("port", 9099, "HTTP port to listen on")
	modelPath     := flag.String("model", "", "path to ONNX model file (required)")
	faceModelPath := flag.String("face-model", "", "path to ArcFace ONNX model file (optional, enables /v1/face/embed)")
	libPath       := flag.String("lib", "/usr/local/lib/libonnxruntime.so.1.18.1", "path to libonnxruntime shared library")
	threshold     := flag.Float64("threshold", 0.5, "confidence threshold [0.0-1.0]")
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

	// Load optional ArcFace model for face embedding (R11).
	var faceModel *model.ArcFaceModel
	if *faceModelPath != "" {
		if _, err := os.Stat(*faceModelPath); err != nil {
			log.Fatalf("sentinel-infer: face model file not found: %v", err)
		}
		log.Printf("sentinel-infer: loading ArcFace model %q", *faceModelPath)
		faceModel, err = model.NewArcFaceModel(*faceModelPath)
		if err != nil {
			log.Fatalf("sentinel-infer: failed to load ArcFace model: %v", err)
		}
		defer faceModel.Close()
		log.Println("sentinel-infer: ArcFace model loaded")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", healthHandler(m))
	mux.HandleFunc("/v1/vision/detection", detectionHandler(m))
	mux.HandleFunc("/v1/face/embed", faceEmbedHandler(faceModel))
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

// ─── Phase 13 (R11 face recognition, R12 audio classification) ──────────────

// faceEmbedHandler handles POST /v1/face/embed.
// Request: multipart/form-data with "image" (JPEG) and optional "max_faces" (int, default 5).
// Response: {"success":true,"faces":[{"embedding":[...512 floats...],"x_min":0.0,...}]}
//
// When faceModel is nil (no -face-model flag) the endpoint returns 501 Not Implemented.
// When no face detector is configured the whole image is treated as a single face —
// this is sufficient for the enrolment photo upload flow.
func faceEmbedHandler(faceModel *model.ArcFaceModel) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			json.NewEncoder(w).Encode(map[string]any{"success": false, "error": "POST required"})
			return
		}
		if faceModel == nil {
			w.WriteHeader(http.StatusNotImplemented)
			json.NewEncoder(w).Encode(map[string]any{
				"success": false,
				"error":   "face embedding not available — start sentinel-infer with -face-model flag",
				"faces":   []any{},
			})
			return
		}

		// Limit upload to 16 MB (matches frontend validation).
		r.Body = http.MaxBytesReader(w, r.Body, 16<<20)
		if err := r.ParseMultipartForm(16 << 20); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]any{"success": false, "error": "parsing multipart: " + err.Error()})
			return
		}

		file, _, err := r.FormFile("image")
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]any{"success": false, "error": "missing 'image' field: " + err.Error()})
			return
		}
		defer file.Close()

		jpegBytes, err := readFormFile(file)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]any{"success": false, "error": "reading image: " + err.Error()})
			return
		}
		if len(jpegBytes) == 0 {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]any{"success": false, "error": "image field is empty"})
			return
		}

		maxFaces := 5
		if mf := r.FormValue("max_faces"); mf != "" {
			if v, err := fmt.Sscanf(mf, "%d", &maxFaces); err != nil || v != 1 || maxFaces < 1 {
				maxFaces = 5
			}
		}

		// No face detection model — pass nil boxes so ArcFace treats the whole
		// image as a single face.  This is the expected mode for enrolment.
		results, err := faceModel.Embed(jpegBytes, maxFaces, nil)
		if err != nil {
			log.Printf("sentinel-infer: face embed error: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]any{"success": false, "error": err.Error()})
			return
		}

		json.NewEncoder(w).Encode(map[string]any{"success": true, "faces": results})
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
