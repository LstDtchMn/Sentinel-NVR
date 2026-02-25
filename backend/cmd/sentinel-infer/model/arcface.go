// ArcFace embedding model for face recognition (R11).
//
// Loads an ArcFace ONNX model (112×112 RGB input → 512-dim L2-normalised
// embedding).  ONNX Runtime must already be initialised before constructing
// an ArcFaceModel — the main YOLOv8 model init handles this.
//
// When no face-detection model is available the whole image is treated as a
// single face (sufficient for enrolment photos).  A proper face detector
// (SCRFD / RetinaFace) can be layered on top for real-time pipeline matching.
package model

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"math"

	ort "github.com/yalue/onnxruntime_go"
	"golang.org/x/image/draw"
)

// arcfaceInput is the spatial resolution expected by standard ArcFace models.
const arcfaceInput = 112

// embeddingDim is the output vector length of ArcFace-R50 / R100.
const embeddingDim = 512

// FaceEmbeddingResult is a single face with its 512-dim embedding and
// normalised bounding box (0–1 range relative to the input image).
type FaceEmbeddingResult struct {
	Embedding []float32 `json:"embedding"`
	XMin      float64   `json:"x_min"`
	YMin      float64   `json:"y_min"`
	XMax      float64   `json:"x_max"`
	YMax      float64   `json:"y_max"`
}

// ArcFaceModel wraps an ONNX Runtime session for ArcFace face embedding.
// It is safe for concurrent use — each Embed call creates independent tensors.
type ArcFaceModel struct {
	session *ort.DynamicAdvancedSession
}

// NewArcFaceModel loads an ArcFace ONNX model.
// ONNX Runtime must be initialised (via NewYOLOv8Model or manually) before
// calling this function.
//
// Expected model I/O (InsightFace ArcFace-R50/R100 PyTorch export):
//
//	Input:  "input.1"  shape (1, 3, 112, 112)  — RGB, normalised (x-127.5)/127.5
//	Output: "output"   shape (1, 512)           — L2-normalised embedding
func NewArcFaceModel(modelPath string) (*ArcFaceModel, error) {
	inputNames := []string{"input.1"}
	outputNames := []string{"output"}

	session, err := ort.NewDynamicAdvancedSession(modelPath, inputNames, outputNames, nil)
	if err != nil {
		return nil, fmt.Errorf("loading ArcFace model %q: %w", modelPath, err)
	}
	return &ArcFaceModel{session: session}, nil
}

// Embed extracts face embeddings from a JPEG image.  When no face bounding
// boxes are provided (faceBoxes == nil) the whole image is treated as a single
// face — this is the expected mode for the enrolment photo upload flow.
//
// faceBoxes are normalised [0,1] coordinates.  maxFaces <= 0 means no limit.
func (m *ArcFaceModel) Embed(jpegBytes []byte, maxFaces int, faceBoxes []FaceBox) ([]FaceEmbeddingResult, error) {
	src, err := jpeg.Decode(bytes.NewReader(jpegBytes))
	if err != nil {
		return nil, fmt.Errorf("decoding JPEG: %w", err)
	}
	origW := src.Bounds().Dx()
	origH := src.Bounds().Dy()
	if origW == 0 || origH == 0 {
		return nil, fmt.Errorf("JPEG has zero dimensions (%d×%d)", origW, origH)
	}

	// Default: treat the whole image as a single face.
	if len(faceBoxes) == 0 {
		faceBoxes = []FaceBox{{XMin: 0, YMin: 0, XMax: 1, YMax: 1}}
	}
	if maxFaces > 0 && len(faceBoxes) > maxFaces {
		faceBoxes = faceBoxes[:maxFaces]
	}

	results := make([]FaceEmbeddingResult, 0, len(faceBoxes))
	for _, box := range faceBoxes {
		embedding, err := m.embedCrop(src, origW, origH, box)
		if err != nil {
			return nil, err
		}
		results = append(results, FaceEmbeddingResult{
			Embedding: embedding,
			XMin:      box.XMin,
			YMin:      box.YMin,
			XMax:      box.XMax,
			YMax:      box.YMax,
		})
	}
	return results, nil
}

// FaceBox is a normalised bounding box (0–1) for a detected face.
type FaceBox struct {
	XMin float64
	YMin float64
	XMax float64
	YMax float64
}

// embedCrop crops the source image to the given normalised box, resizes to
// 112×112, preprocesses, and runs ArcFace inference.
func (m *ArcFaceModel) embedCrop(src image.Image, origW, origH int, box FaceBox) ([]float32, error) {
	// Convert normalised box → pixel coordinates.
	cropRect := image.Rect(
		clampInt(int(box.XMin*float64(origW)), 0, origW),
		clampInt(int(box.YMin*float64(origH)), 0, origH),
		clampInt(int(box.XMax*float64(origW)), 0, origW),
		clampInt(int(box.YMax*float64(origH)), 0, origH),
	)
	if cropRect.Dx() < 2 || cropRect.Dy() < 2 {
		return nil, fmt.Errorf("face crop too small (%d×%d)", cropRect.Dx(), cropRect.Dy())
	}

	// Resize face crop to 112×112.
	face := image.NewRGBA(image.Rect(0, 0, arcfaceInput, arcfaceInput))
	draw.BiLinear.Scale(face, face.Bounds(), src, cropRect, draw.Over, nil)

	// Preprocess: CHW, RGB, (pixel - 127.5) / 127.5 → range [-1, 1].
	inputData := make([]float32, 3*arcfaceInput*arcfaceInput)
	for y := 0; y < arcfaceInput; y++ {
		for x := 0; x < arcfaceInput; x++ {
			r, g, b, _ := face.At(x, y).RGBA()
			inputData[0*arcfaceInput*arcfaceInput+y*arcfaceInput+x] = (float32(r>>8) - 127.5) / 127.5
			inputData[1*arcfaceInput*arcfaceInput+y*arcfaceInput+x] = (float32(g>>8) - 127.5) / 127.5
			inputData[2*arcfaceInput*arcfaceInput+y*arcfaceInput+x] = (float32(b>>8) - 127.5) / 127.5
		}
	}

	// Run ArcFace inference.
	inputShape := ort.NewShape(1, 3, arcfaceInput, arcfaceInput)
	inputTensor, err := ort.NewTensor(inputShape, inputData)
	if err != nil {
		return nil, fmt.Errorf("creating ArcFace input tensor: %w", err)
	}
	defer inputTensor.Destroy()

	outputData := make([]float32, embeddingDim)
	outputShape := ort.NewShape(1, embeddingDim)
	outputTensor, err := ort.NewTensor(outputShape, outputData)
	if err != nil {
		return nil, fmt.Errorf("creating ArcFace output tensor: %w", err)
	}
	defer outputTensor.Destroy()

	if err := m.session.Run([]ort.Value{inputTensor}, []ort.Value{outputTensor}); err != nil {
		return nil, fmt.Errorf("ArcFace inference: %w", err)
	}

	return l2Normalize(outputData), nil
}

// Close releases the ONNX session.
func (m *ArcFaceModel) Close() error {
	if m.session != nil {
		return m.session.Destroy()
	}
	return nil
}

// l2Normalize returns a copy of v with unit L2 norm.
func l2Normalize(v []float32) []float32 {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	norm := math.Sqrt(sum)
	if norm < 1e-10 {
		out := make([]float32, len(v))
		copy(out, v)
		return out
	}
	out := make([]float32, len(v))
	for i, x := range v {
		out[i] = float32(float64(x) / norm)
	}
	return out
}
