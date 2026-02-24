package detection

import (
	"testing"
)

func TestFilterByZonesNoZones(t *testing.T) {
	dets := []DetectedObject{
		{Label: "person", Confidence: 0.9, BBox: BBox{XMin: 0.1, YMin: 0.1, XMax: 0.3, YMax: 0.3}},
	}
	result := filterByZones(dets, nil)
	if len(result) != 1 {
		t.Errorf("expected 1 detection, got %d", len(result))
	}
}

func TestFilterByZonesInclude(t *testing.T) {
	// Detection at centre (0.5, 0.5)
	dets := []DetectedObject{
		{Label: "person", Confidence: 0.9, BBox: BBox{XMin: 0.4, YMin: 0.4, XMax: 0.6, YMax: 0.6}},
		{Label: "car", Confidence: 0.8, BBox: BBox{XMin: 0.0, YMin: 0.0, XMax: 0.1, YMax: 0.1}},
	}
	zones := []Zone{
		{
			ID:   "z1",
			Name: "driveway",
			Type: ZoneInclude,
			Points: []ZonePoint{
				{X: 0.3, Y: 0.3},
				{X: 0.7, Y: 0.3},
				{X: 0.7, Y: 0.7},
				{X: 0.3, Y: 0.7},
			},
		},
	}
	result := filterByZones(dets, zones)
	if len(result) != 1 {
		t.Fatalf("expected 1 detection (person inside zone), got %d", len(result))
	}
	if result[0].Label != "person" {
		t.Errorf("expected person, got %s", result[0].Label)
	}
}

func TestFilterByZonesExclude(t *testing.T) {
	dets := []DetectedObject{
		{Label: "person", Confidence: 0.9, BBox: BBox{XMin: 0.4, YMin: 0.4, XMax: 0.6, YMax: 0.6}},
		{Label: "car", Confidence: 0.8, BBox: BBox{XMin: 0.8, YMin: 0.8, XMax: 0.9, YMax: 0.9}},
	}
	zones := []Zone{
		{
			ID:   "z1",
			Name: "road",
			Type: ZoneExclude,
			Points: []ZonePoint{
				{X: 0.3, Y: 0.3},
				{X: 0.7, Y: 0.3},
				{X: 0.7, Y: 0.7},
				{X: 0.3, Y: 0.7},
			},
		},
	}
	result := filterByZones(dets, zones)
	if len(result) != 1 {
		t.Fatalf("expected 1 detection (car outside exclude zone), got %d", len(result))
	}
	if result[0].Label != "car" {
		t.Errorf("expected car, got %s", result[0].Label)
	}
}

func TestPointInPolygonSquare(t *testing.T) {
	// Unit square
	poly := []ZonePoint{
		{X: 0, Y: 0},
		{X: 1, Y: 0},
		{X: 1, Y: 1},
		{X: 0, Y: 1},
	}
	tests := []struct {
		x, y   float64
		inside bool
	}{
		{0.5, 0.5, true},
		{0.1, 0.1, true},
		{0.9, 0.9, true},
		{1.5, 0.5, false},
		{-0.1, 0.5, false},
		{0.5, -0.1, false},
		{0.5, 1.5, false},
	}
	for _, tt := range tests {
		got := pointInPolygon(tt.x, tt.y, poly)
		if got != tt.inside {
			t.Errorf("pointInPolygon(%g, %g) = %v, want %v", tt.x, tt.y, got, tt.inside)
		}
	}
}

func TestPointInPolygonTriangle(t *testing.T) {
	tri := []ZonePoint{
		{X: 0.5, Y: 0.0},
		{X: 1.0, Y: 1.0},
		{X: 0.0, Y: 1.0},
	}
	if !pointInPolygon(0.5, 0.5, tri) {
		t.Error("centre of triangle should be inside")
	}
	if pointInPolygon(0.0, 0.0, tri) {
		t.Error("origin should be outside triangle")
	}
}

func TestPointInPolygonTooFewPoints(t *testing.T) {
	// Fewer than 3 points: always false
	if pointInPolygon(0.5, 0.5, []ZonePoint{{X: 0, Y: 0}, {X: 1, Y: 1}}) {
		t.Error("polygon with 2 points should always return false")
	}
	if pointInPolygon(0.5, 0.5, nil) {
		t.Error("nil polygon should return false")
	}
}

func TestMatchBestFace(t *testing.T) {
	// Create test embeddings
	embedding := make([]float32, 512)
	embedding[0] = 1.0

	face1Emb := make([]float32, 512)
	face1Emb[0] = 1.0 // identical to query

	face2Emb := make([]float32, 512)
	face2Emb[1] = 1.0 // orthogonal

	faces := []FaceRecord{
		{ID: 1, Name: "Alice", Embedding: face1Emb},
		{ID: 2, Name: "Bob", Embedding: face2Emb},
	}

	face, sim := matchBestFace(embedding, faces, 0.5)
	if face == nil {
		t.Fatal("expected a match")
	}
	if face.Name != "Alice" {
		t.Errorf("expected Alice, got %s", face.Name)
	}
	if sim < 0.99 {
		t.Errorf("similarity = %f, expected ~1.0", sim)
	}
}

func TestMatchBestFaceBelowThreshold(t *testing.T) {
	embedding := make([]float32, 512)
	embedding[0] = 1.0

	faceEmb := make([]float32, 512)
	faceEmb[1] = 1.0 // orthogonal = 0 similarity

	faces := []FaceRecord{
		{ID: 1, Name: "Unknown", Embedding: faceEmb},
	}

	face, _ := matchBestFace(embedding, faces, 0.5)
	if face != nil {
		t.Error("expected no match for orthogonal embeddings with threshold 0.5")
	}
}

func TestMatchBestFaceEmptyFaces(t *testing.T) {
	embedding := make([]float32, 512)
	face, sim := matchBestFace(embedding, nil, 0.5)
	if face != nil {
		t.Error("expected nil for empty face list")
	}
	if sim != 0 {
		t.Errorf("expected 0 similarity, got %f", sim)
	}
}
