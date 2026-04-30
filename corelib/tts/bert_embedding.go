package tts

import (
	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

// ComputeBertEmbedding generates per-phoneme embeddings using Gemma Embedder.
// Returns bert1024 [1024, T] (zeros) and jaBert768 [768, T] (Gemma per-token states).
//
// Strategy: run Gemma on the input text to get per-token hidden states [seq, 768],
// then align/interpolate to match the phoneme sequence length T.
func ComputeBertEmbedding(text string, phonemes []string, T int,
	emb embedding.Embedder) (bert1024 []float32, jaBert768 []float32) {

	bert1024 = make([]float32, 1024*T) // zeros
	jaBert768 = make([]float32, 768*T) // will be filled

	gemma, ok := emb.(*embedding.GemmaEmbedder)
	if gemma == nil || !ok {
		return
	}

	// Get per-token hidden states from Gemma
	states, seq, dim, err := gemma.EmbedTokenStates(text)
	if err != nil || seq == 0 {
		return
	}

	// states is [seq, dim] row-major. We need [768, T] column-major for MeloTTS.
	// Alignment: linearly interpolate Gemma's seq tokens to T phoneme positions.
	// This is a simple approach — each phoneme position maps to a fractional
	// position in the Gemma token sequence.

	useDim := 768
	if dim < useDim {
		useDim = dim
	}

	if seq == 1 {
		// Single token — broadcast to all positions
		for t := 0; t < T; t++ {
			for d := 0; d < useDim; d++ {
				jaBert768[d*T+t] = states[d]
			}
		}
		return
	}

	// Linear interpolation: map phoneme position t ∈ [0, T-1] to
	// Gemma token position s ∈ [0, seq-1]
	for t := 0; t < T; t++ {
		// Fractional position in Gemma sequence
		frac := float32(t) * float32(seq-1) / float32(T-1)
		s0 := int(frac)
		s1 := s0 + 1
		if s1 >= seq {
			s1 = seq - 1
		}
		w1 := frac - float32(s0)
		w0 := 1.0 - w1

		for d := 0; d < useDim; d++ {
			v := w0*states[s0*dim+d] + w1*states[s1*dim+d]
			jaBert768[d*T+t] = v
		}
	}

	return
}
