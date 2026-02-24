// Package model implements YOLOv8n inference via ONNX Runtime (R3, CG10).
// It preprocesses JPEG frames to 640×640 (letterbox, gray padding 114),
// runs the ONNX session, then converts raw output to labeled bounding boxes
// in the coordinate space of the original input image.
package model

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"sort"

	ort "github.com/yalue/onnxruntime_go"
	"golang.org/x/image/draw"
)

// inputSize is the fixed spatial resolution expected by YOLOv8n.
const inputSize = 640

// padValue is the gray fill used for letterbox padding (YOLOv8 training default).
const padValue = 114

// iouThreshold is the IoU overlap threshold for non-maximum suppression.
// 0.45 is the COCO evaluation standard: lower values suppress more overlapping
// boxes (aggressive), higher values preserve more (permissive). Matches the
// default used by Ultralytics YOLOv8 at export time.
const iouThreshold = 0.45

// COCO80 maps class index → human-readable label (YOLOv8 COCO80 class order).
var COCO80 = [80]string{
	"person", "bicycle", "car", "motorcycle", "airplane", "bus", "train", "truck",
	"boat", "traffic light", "fire hydrant", "stop sign", "parking meter", "bench",
	"bird", "cat", "dog", "horse", "sheep", "cow", "elephant", "bear", "zebra",
	"giraffe", "backpack", "umbrella", "handbag", "tie", "suitcase", "frisbee",
	"skis", "snowboard", "sports ball", "kite", "baseball bat", "baseball glove",
	"skateboard", "surfboard", "tennis racket", "bottle", "wine glass", "cup",
	"fork", "knife", "spoon", "bowl", "banana", "apple", "sandwich", "orange",
	"broccoli", "carrot", "hot dog", "pizza", "donut", "cake", "chair", "couch",
	"potted plant", "bed", "dining table", "toilet", "tv", "laptop", "mouse",
	"remote", "keyboard", "cell phone", "microwave", "oven", "toaster", "sink",
	"refrigerator", "book", "clock", "vase", "scissors", "teddy bear", "hair drier",
	"toothbrush",
}

// BBox is a bounding box in absolute pixel coordinates of the original image.
type BBox struct {
	XMin int `json:"x_min"`
	YMin int `json:"y_min"`
	XMax int `json:"x_max"`
	YMax int `json:"y_max"`
}

// Detection is a single inference result with label, confidence, and bounding box.
type Detection struct {
	Label      string  `json:"label"`
	Confidence float32 `json:"confidence"`
	BBox       BBox    `json:"bbox"`
}

// YOLOv8Model wraps an ONNX Runtime session for YOLOv8n object detection.
// It is safe for concurrent use — each Detect call creates independent tensors.
type YOLOv8Model struct {
	session   *ort.DynamicAdvancedSession
	threshold float32
	modelName string
}

// NewYOLOv8Model loads a YOLOv8n ONNX model file and initialises the ONNX Runtime
// environment. libPath is the path to libonnxruntime.so; modelPath is the .onnx file.
// threshold filters out detections below the given confidence (0.0–1.0).
func NewYOLOv8Model(libPath, modelPath string, threshold float32) (*YOLOv8Model, error) {
	ort.SetSharedLibraryPath(libPath)
	if err := ort.InitializeEnvironment(); err != nil {
		return nil, fmt.Errorf("initializing ONNX Runtime: %w", err)
	}

	inputNames := []string{"images"}
	outputNames := []string{"output0"}

	session, err := ort.NewDynamicAdvancedSession(modelPath, inputNames, outputNames, nil)
	if err != nil {
		return nil, fmt.Errorf("loading ONNX model %q: %w", modelPath, err)
	}

	// Extract filename for /health endpoint display.
	name := modelPath
	for i := len(modelPath) - 1; i >= 0; i-- {
		if modelPath[i] == '/' || modelPath[i] == '\\' {
			name = modelPath[i+1:]
			break
		}
	}

	return &YOLOv8Model{
		session:   session,
		threshold: threshold,
		modelName: name,
	}, nil
}

// ModelName returns the base filename of the loaded ONNX model.
func (m *YOLOv8Model) ModelName() string { return m.modelName }

// Detect runs YOLOv8n inference on a raw JPEG image.
// Returns bounding boxes in the coordinate space of the original input image.
//
// Pipeline:
//  1. Decode JPEG → get original W×H
//  2. Letterbox-resize to 640×640, pad gray (114)
//  3. HWC uint8 → CHW float32 / 255.0
//  4. Run ONNX session → output shape (1, 84, 8400)
//  5. For each of 8400 anchors: find best class, filter by threshold
//  6. Un-letterbox bbox to original pixel coordinates
//  7. Per-class NMS (IoU threshold 0.45)
func (m *YOLOv8Model) Detect(jpegBytes []byte) ([]Detection, error) {
	// --- Step 1: decode JPEG ---
	src, err := jpeg.Decode(bytes.NewReader(jpegBytes))
	if err != nil {
		return nil, fmt.Errorf("decoding JPEG: %w", err)
	}
	origW := src.Bounds().Dx()
	origH := src.Bounds().Dy()
	if origW == 0 || origH == 0 {
		return nil, fmt.Errorf("JPEG has zero dimensions (%dx%d)", origW, origH)
	}

	// --- Step 2: letterbox resize ---
	scale := float32(inputSize) / float32(origW)
	if sh := float32(inputSize) / float32(origH); sh < scale {
		scale = sh
	}
	newW := int(float32(origW) * scale)
	newH := int(float32(origH) * scale)
	padX := (inputSize - newW) / 2
	padY := (inputSize - newH) / 2

	canvas := image.NewRGBA(image.Rect(0, 0, inputSize, inputSize))
	// Fill with gray pad value.
	gray := color.RGBA{R: padValue, G: padValue, B: padValue, A: 255}
	for y := 0; y < inputSize; y++ {
		for x := 0; x < inputSize; x++ {
			canvas.SetRGBA(x, y, gray)
		}
	}
	dst := canvas.SubImage(image.Rect(padX, padY, padX+newW, padY+newH)).(*image.RGBA)
	draw.BiLinear.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)

	// --- Step 3: HWC uint8 → CHW float32 / 255.0 ---
	// ONNX expects [1, 3, 640, 640] in RGB channel order.
	inputData := make([]float32, 1*3*inputSize*inputSize)
	for y := 0; y < inputSize; y++ {
		for x := 0; x < inputSize; x++ {
			r, g, b, _ := canvas.At(x, y).RGBA()
			// RGBA() returns values in [0, 65535]; shift to [0, 255].
			inputData[0*inputSize*inputSize+y*inputSize+x] = float32(r>>8) / 255.0
			inputData[1*inputSize*inputSize+y*inputSize+x] = float32(g>>8) / 255.0
			inputData[2*inputSize*inputSize+y*inputSize+x] = float32(b>>8) / 255.0
		}
	}

	// --- Step 4: run ONNX session ---
	inputShape := ort.NewShape(1, 3, inputSize, inputSize)
	inputTensor, err := ort.NewTensor(inputShape, inputData)
	if err != nil {
		return nil, fmt.Errorf("creating input tensor: %w", err)
	}
	defer inputTensor.Destroy()

	// Output shape for YOLOv8n: (1, 84, 8400)
	// 84 = 4 (cx, cy, w, h in 640px space) + 80 class scores.
	// 8400 = number of anchors at three scales (80x80 + 40x40 + 20x20 = 8400).
	outputShape := ort.NewShape(1, 84, 8400)
	outputData := make([]float32, 1*84*8400)
	outputTensor, err := ort.NewTensor(outputShape, outputData)
	if err != nil {
		return nil, fmt.Errorf("creating output tensor: %w", err)
	}
	defer outputTensor.Destroy()

	inputs := []ort.Value{inputTensor}
	outputs := []ort.Value{outputTensor}
	if err := m.session.Run(inputs, outputs); err != nil {
		return nil, fmt.Errorf("running ONNX inference: %w", err)
	}

	// --- Step 5: parse output + filter by threshold ---
	// Layout: output[channel][anchor], accessed as outputData[channel*8400 + anchor].
	type rawDet struct {
		classIdx int
		conf     float32
		cx640    float32 // cx in 640px space
		cy640    float32
		w640     float32
		h640     float32
	}
	var raws []rawDet
	const numAnchors = 8400
	const numClasses = 80

	for anchor := 0; anchor < numAnchors; anchor++ {
		cx := outputData[0*numAnchors+anchor]
		cy := outputData[1*numAnchors+anchor]
		w := outputData[2*numAnchors+anchor]
		h := outputData[3*numAnchors+anchor]

		// Find best class score.
		bestClass := -1
		bestScore := float32(0)
		for cls := 0; cls < numClasses; cls++ {
			score := outputData[(4+cls)*numAnchors+anchor]
			if score > bestScore {
				bestScore = score
				bestClass = cls
			}
		}
		if bestClass < 0 || bestScore < m.threshold {
			continue
		}
		raws = append(raws, rawDet{classIdx: bestClass, conf: bestScore, cx640: cx, cy640: cy, w640: w, h640: h})
	}

	if len(raws) == 0 {
		return nil, nil
	}

	// --- Step 6: un-letterbox to original pixel coordinates ---
	type classedDet struct {
		classIdx int
		conf     float32
		bbox     BBox
	}
	var classed []classedDet
	for _, r := range raws {
		// Reverse letterbox: subtract padding, divide by scale.
		cxOrig := (r.cx640 - float32(padX)) / scale
		cyOrig := (r.cy640 - float32(padY)) / scale
		wOrig := r.w640 / scale
		hOrig := r.h640 / scale

		xMin := clampInt(int(cxOrig-wOrig/2), 0, origW-1)
		yMin := clampInt(int(cyOrig-hOrig/2), 0, origH-1)
		xMax := clampInt(int(cxOrig+wOrig/2), 0, origW-1)
		yMax := clampInt(int(cyOrig+hOrig/2), 0, origH-1)
		if xMax <= xMin || yMax <= yMin {
			continue
		}
		classed = append(classed, classedDet{
			classIdx: r.classIdx,
			conf:     r.conf,
			bbox:     BBox{XMin: xMin, YMin: yMin, XMax: xMax, YMax: yMax},
		})
	}

	// --- Step 7: per-class NMS ---
	// Group by class, sort by confidence descending, suppress overlapping boxes.
	byClass := make(map[int][]classedDet)
	for _, d := range classed {
		byClass[d.classIdx] = append(byClass[d.classIdx], d)
	}

	var detections []Detection
	for classIdx, dets := range byClass {
		sort.Slice(dets, func(i, j int) bool { return dets[i].conf > dets[j].conf })
		kept := nms(dets, iouThreshold)
		for _, d := range kept {
			label := "unknown"
			if classIdx < len(COCO80) {
				label = COCO80[classIdx]
			}
			detections = append(detections, Detection{
				Label:      label,
				Confidence: d.conf,
				BBox:       d.bbox,
			})
		}
	}

	return detections, nil
}

// Close releases ONNX Runtime resources. Call once when the model is no longer needed.
func (m *YOLOv8Model) Close() error {
	if m.session != nil {
		return m.session.Destroy()
	}
	return nil
}

// nms applies non-maximum suppression to a confidence-sorted slice of detections.
// Returns the subset of detections after removing boxes with IoU > threshold.
func nms(dets []classedDet, threshold float32) []classedDet {
	suppressed := make([]bool, len(dets))
	var kept []classedDet
	for i := 0; i < len(dets); i++ {
		if suppressed[i] {
			continue
		}
		kept = append(kept, dets[i])
		for j := i + 1; j < len(dets); j++ {
			if suppressed[j] {
				continue
			}
			if iou(dets[i].bbox, dets[j].bbox) > threshold {
				suppressed[j] = true
			}
		}
	}
	return kept
}

// iou computes the intersection-over-union of two bounding boxes.
func iou(a, b BBox) float32 {
	interXMin := max(a.XMin, b.XMin)
	interYMin := max(a.YMin, b.YMin)
	interXMax := min(a.XMax, b.XMax)
	interYMax := min(a.YMax, b.YMax)
	if interXMax <= interXMin || interYMax <= interYMin {
		return 0
	}
	inter := float64((interXMax - interXMin) * (interYMax - interYMin))
	areaA := float64((a.XMax - a.XMin) * (a.YMax - a.YMin))
	areaB := float64((b.XMax - b.XMin) * (b.YMax - b.YMin))
	union := areaA + areaB - inter
	if union <= 0 {
		return 0
	}
	return float32(inter / union)
}

func clampInt(v, lo, hi int) int {
	return int(math.Min(math.Max(float64(v), float64(lo)), float64(hi)))
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
