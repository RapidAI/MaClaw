package embedding

// NoopEmbedder is a no-op implementation used when no embedding model is available.
// All methods are safe to call; Embed returns nil without error.
type NoopEmbedder struct{}

func NewNoopEmbedder() NoopEmbedder {
	return NoopEmbedder{}
}

func (NoopEmbedder) Embed(string) ([]float32, error) { return nil, nil }
func (NoopEmbedder) EmbedBatch(texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	return out, nil
}
func (NoopEmbedder) Dim() int { return 0 }
func (NoopEmbedder) Close()   {}

// IsNoop returns true if the embedder is a NoopEmbedder.
func IsNoop(e Embedder) bool {
	if e == nil {
		return true
	}
	_, ok := e.(NoopEmbedder)
	return ok
}
