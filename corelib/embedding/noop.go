package embedding

import "reflect"

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

// IsNoop reports that e cannot embed. A nil interface and an interface holding
// a nil pointer are both absent: the pointer's method set still type-asserts,
// and calling Dim or Embed on it is a nil dereference.
func IsNoop(e Embedder) bool {
	if embedderAbsent(e) {
		return true
	}
	_, ok := e.(NoopEmbedder)
	return ok
}

func embedderAbsent(e Embedder) bool {
	if e == nil {
		return true
	}
	// The live models are these two pointers. A type switch answers the nil
	// pointer without reflection; other implementations fall through.
	switch p := e.(type) {
	case *Qwen3Embedder:
		return p == nil
	case *GemmaEmbedder:
		return p == nil
	case NoopEmbedder:
		return false
	default:
		return reflectValueIsNil(e)
	}
}

func reflectValueIsNil(v any) bool {
	value := reflect.ValueOf(v)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
