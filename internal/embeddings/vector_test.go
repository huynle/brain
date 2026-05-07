package embeddings

import (
	"math"
	"testing"
)

func TestEncodeDecodeFloat32VectorUsesLittleEndianBlob(t *testing.T) {
	encoded := EncodeFloat32Vector([]float32{1, -2.5})
	want := []byte{0, 0, 128, 63, 0, 0, 32, 192}
	if string(encoded) != string(want) {
		t.Fatalf("encoded = %v, want little-endian float32 blob %v", encoded, want)
	}

	decoded, err := DecodeFloat32Vector(encoded)
	if err != nil {
		t.Fatalf("DecodeFloat32Vector failed: %v", err)
	}
	if len(decoded) != 2 || decoded[0] != 1 || decoded[1] != -2.5 {
		t.Fatalf("decoded = %+v, want original vector", decoded)
	}
}

func TestDecodeFloat32VectorRejectsInvalidLength(t *testing.T) {
	if _, err := DecodeFloat32Vector([]byte{1, 2, 3}); err == nil {
		t.Fatal("expected invalid length error")
	}
}

func TestCosineSimilarity(t *testing.T) {
	got, err := CosineSimilarity([]float32{1, 0, 1}, []float32{0, 1, 1})
	if err != nil {
		t.Fatalf("CosineSimilarity failed: %v", err)
	}
	if math.Abs(float64(got-0.5)) > 0.0001 {
		t.Fatalf("similarity = %v, want 0.5", got)
	}
}

func TestCosineSimilarityRejectsInvalidVectors(t *testing.T) {
	if _, err := CosineSimilarity([]float32{1}, []float32{1, 2}); err == nil {
		t.Fatal("expected dimension mismatch error")
	}
	if _, err := CosineSimilarity([]float32{0}, []float32{1}); err == nil {
		t.Fatal("expected zero vector error")
	}
	if _, err := CosineSimilarity(nil, nil); err == nil {
		t.Fatal("expected empty vector error")
	}
}
