package detection

import (
	"math"
	"testing"
)

func TestCosineSimilarityIdentical(t *testing.T) {
	a := []float32{1, 0, 0}
	b := []float32{1, 0, 0}
	sim := cosineSimilarity(a, b)
	if math.Abs(sim-1.0) > 1e-6 {
		t.Errorf("identical vectors: similarity = %f, want 1.0", sim)
	}
}

func TestCosineSimilarityOrthogonal(t *testing.T) {
	a := []float32{1, 0, 0}
	b := []float32{0, 1, 0}
	sim := cosineSimilarity(a, b)
	if math.Abs(sim) > 1e-6 {
		t.Errorf("orthogonal vectors: similarity = %f, want 0.0", sim)
	}
}

func TestCosineSimilarityOpposite(t *testing.T) {
	a := []float32{1, 0, 0}
	b := []float32{-1, 0, 0}
	sim := cosineSimilarity(a, b)
	if math.Abs(sim-(-1.0)) > 1e-6 {
		t.Errorf("opposite vectors: similarity = %f, want -1.0", sim)
	}
}

func TestCosineSimilarityDifferentLengths(t *testing.T) {
	a := []float32{1, 0}
	b := []float32{1, 0, 0}
	sim := cosineSimilarity(a, b)
	// Should handle gracefully (use shorter length)
	if sim < -1 || sim > 1 {
		t.Errorf("similarity %f out of range [-1, 1]", sim)
	}
}

func TestCosineSimilarityZeroVector(t *testing.T) {
	a := []float32{0, 0, 0}
	b := []float32{1, 0, 0}
	sim := cosineSimilarity(a, b)
	// Division by zero should return 0
	if math.IsNaN(sim) || math.IsInf(sim, 0) {
		t.Errorf("zero vector: similarity should be 0, got %f", sim)
	}
}

func TestEncodeDecodeEmbedding(t *testing.T) {
	original := []float32{1.0, 2.5, -3.14, 0.0, 100.0}
	encoded := encodeEmbedding(original)
	decoded := decodeEmbedding(encoded)

	if len(decoded) != len(original) {
		t.Fatalf("length mismatch: got %d, want %d", len(decoded), len(original))
	}
	for i := range original {
		if math.Abs(float64(decoded[i]-original[i])) > 1e-6 {
			t.Errorf("index %d: got %f, want %f", i, decoded[i], original[i])
		}
	}
}

func TestEncodeDecodeEmbeddingEmpty(t *testing.T) {
	var empty []float32
	encoded := encodeEmbedding(empty)
	decoded := decodeEmbedding(encoded)
	if len(decoded) != 0 {
		t.Errorf("expected empty, got %d elements", len(decoded))
	}
}

func TestDecodeEmbeddingNil(t *testing.T) {
	decoded := decodeEmbedding(nil)
	if len(decoded) != 0 {
		t.Errorf("expected empty slice, got %d elements", len(decoded))
	}
}
