package tool

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/embedding/tensor"
)

type stubConcurrentEmbedder struct{}

func (stubConcurrentEmbedder) Embed(text string) ([]float32, error) {
	return []float32{1, 0, 0}, nil
}
func (stubConcurrentEmbedder) EmbedBatch(texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = []float32{1, 0, 0}
	}
	return out, nil
}
func (stubConcurrentEmbedder) Dim() int { return 3 }
func (stubConcurrentEmbedder) Close()   {}
func (stubConcurrentEmbedder) EmbedConcurrent(text string) ([]float32, error) {
	return []float32{1, 0, 0}, nil
}

func TestHybridDoesNotMutateMatMulParallel(t *testing.T) {
	tensor.SetMatMulMaxParallel(5)
	defer tensor.SetMatMulMaxParallel(0)
	c := NewToolEmbeddingCache(stubConcurrentEmbedder{})
	texts := map[string]string{"a": "one", "b": "two", "c": "three", "d": "four"}
	if _, err := c.GetBatch(texts); err != nil {
		t.Fatal(err)
	}
	if tensor.MatMulMaxParallelForTest() != 5 {
		t.Fatalf("hybrid mutated cap to %d", tensor.MatMulMaxParallelForTest())
	}
}
