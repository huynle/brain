package embeddings

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

// EncodeFloat32Vector encodes a vector as a stable little-endian float32 BLOB.
func EncodeFloat32Vector(vector []float32) []byte {
	encoded := make([]byte, len(vector)*4)
	for i, value := range vector {
		binary.LittleEndian.PutUint32(encoded[i*4:], math.Float32bits(value))
	}
	return encoded
}

// DecodeFloat32Vector decodes a little-endian float32 BLOB produced by EncodeFloat32Vector.
func DecodeFloat32Vector(data []byte) ([]float32, error) {
	if len(data)%4 != 0 {
		return nil, fmt.Errorf("float32 vector blob length %d is not divisible by 4", len(data))
	}
	vector := make([]float32, len(data)/4)
	for i := range vector {
		vector[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[i*4:]))
	}
	return vector, nil
}

// CosineSimilarity returns the cosine similarity for same-dimensional vectors.
func CosineSimilarity(a, b []float32) (float32, error) {
	if len(a) != len(b) {
		return 0, fmt.Errorf("vector dimension mismatch: %d != %d", len(a), len(b))
	}
	if len(a) == 0 {
		return 0, errors.New("cosine similarity requires non-empty vectors")
	}

	var dot float64
	var normA float64
	var normB float64
	for i := range a {
		av := float64(a[i])
		bv := float64(b[i])
		dot += av * bv
		normA += av * av
		normB += bv * bv
	}
	if normA == 0 || normB == 0 {
		return 0, errors.New("cosine similarity requires non-zero vectors")
	}

	return float32(dot / (math.Sqrt(normA) * math.Sqrt(normB))), nil
}
