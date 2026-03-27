//go:build cgo_embedding
// +build cgo_embedding

package embedding

/*
#cgo CFLAGS: -I${SRCDIR}/../../RapidSpeech.cpp/include -DRAPIDSPEECH_BUILD
#cgo LDFLAGS: -L${SRCDIR}/../../RapidSpeech.cpp/build -lrapidspeech_static
#cgo !windows LDFLAGS: -L${SRCDIR}/../../RapidSpeech.cpp/build/ggml/src -lggml -lggml-base -lggml-cpu
#cgo windows LDFLAGS: -L${SRCDIR}/../../RapidSpeech.cpp/build/ggml/src -l:ggml.a -l:ggml-base.a -l:ggml-cpu.a -lws2_32 -lgomp
#cgo LDFLAGS: -lstdc++ -lm
#cgo darwin LDFLAGS: -framework Accelerate

#include "rapidspeech.h"
#include <stdlib.h>
*/
import "C"

import (
	"fmt"
	"math"
	"sync"
	"unsafe"
)

// GemmaEmbedder wraps the RapidSpeech C API for Gemma 300M text embedding.
// Thread-safe via internal mutex (ggml context is not thread-safe).
type GemmaEmbedder struct {
	ctx *C.rs_context_t
	dim int
	mu  sync.Mutex
}

// NewGemmaEmbedder loads the Gemma embedding model from a GGUF file.
// dim specifies the output embedding dimension (MRL truncation: 128/256/512/768).
// Returns an error if the model file cannot be loaded.
func NewGemmaEmbedder(modelPath string, dim int) (*GemmaEmbedder, error) {
	if dim <= 0 {
		dim = 256 // default MRL truncation for memory use case
	}

	cPath := C.CString(modelPath)
	defer C.free(unsafe.Pointer(cPath))

	params := C.rs_default_params()
	params.model_path = cPath
	params.n_threads = 4
	params.use_gpu = C.bool(false) // CPU-only for embedding
	params.task_type = C.RS_TASK_TEXT_EMBED

	ctx := C.rs_init_from_file(params)
	if ctx == nil {
		return nil, fmt.Errorf("gemma_embedder: failed to load model from %s", modelPath)
	}

	return &GemmaEmbedder{ctx: ctx, dim: dim}, nil
}

// Embed returns the embedding vector for a single text string.
func (g *GemmaEmbedder) Embed(text string) ([]float32, error) {
	if g.ctx == nil {
		return nil, fmt.Errorf("gemma_embedder: context is nil")
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	C.rs_reset(g.ctx)

	cText := C.CString(text)
	defer C.free(unsafe.Pointer(cText))

	if rc := C.rs_push_text(g.ctx, cText); rc != 0 {
		return nil, fmt.Errorf("gemma_embedder: rs_push_text failed (rc=%d)", rc)
	}

	if rc := C.rs_process(g.ctx); rc < 0 {
		return nil, fmt.Errorf("gemma_embedder: rs_process failed (rc=%d)", rc)
	}

	var embPtr *C.float
	embDim := C.rs_get_embedding_output(g.ctx, &embPtr)
	if embDim <= 0 || embPtr == nil {
		return nil, fmt.Errorf("gemma_embedder: no embedding output (dim=%d)", embDim)
	}

	// Copy embedding to Go slice, truncating to requested dim (MRL).
	outDim := int(embDim)
	if g.dim > 0 && g.dim < outDim {
		outDim = g.dim
	}

	result := make([]float32, outDim)
	cSlice := unsafe.Slice((*float32)(unsafe.Pointer(embPtr)), int(embDim))
	copy(result, cSlice[:outDim])

	// L2 normalize after MRL truncation.
	l2normalize(result)

	return result, nil
}

// EmbedBatch returns embeddings for multiple texts (sequential, no batch API in ggml).
func (g *GemmaEmbedder) EmbedBatch(texts []string) ([][]float32, error) {
	results := make([][]float32, len(texts))
	for i, t := range texts {
		emb, err := g.Embed(t)
		if err != nil {
			return nil, fmt.Errorf("gemma_embedder: batch item %d: %w", i, err)
		}
		results[i] = emb
	}
	return results, nil
}

// Dim returns the output embedding dimension.
func (g *GemmaEmbedder) Dim() int { return g.dim }

// Close releases the ggml context and model resources.
func (g *GemmaEmbedder) Close() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.ctx != nil {
		C.rs_free(g.ctx)
		g.ctx = nil
	}
}

// l2normalize normalizes a vector in-place to unit length.
func l2normalize(v []float32) {
	var sq float64
	for _, x := range v {
		sq += float64(x) * float64(x)
	}
	if sq == 0 {
		return
	}
	inv := float32(1.0 / math.Sqrt(sq))
	for i := range v {
		v[i] *= inv
	}
}
