package asr

import (
	"path/filepath"
	"strconv"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/embedding/tensor"
)

// BenchmarkSVShortAttention4Heads_6x128 is the fused attention workload for
// 200 ms audio: two LFR frames plus four prompt frames.
func BenchmarkSVShortAttention4Heads_6x128(b *testing.B) {
	const frames, heads, headDim, hidden = 6, 4, 128, 512
	q := make([]float32, heads*frames*headDim)
	k := make([]float32, heads*frames*headDim)
	v := make([]float32, heads*frames*headDim)
	out := make([]float32, frames*hidden)
	scores := make([]float32, frames*frames)
	for i := range q {
		q[i] = float32((i%17)-8) * 0.01
		k[i] = float32((i%13)-6) * 0.02
		v[i] = float32((i%11)-5) * 0.03
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for h := 0; h < heads; h++ {
			base := h * frames * headDim
			svAttnScoresPackedQ128NS(out, q[base:base+frames*headDim], k[base:base+frames*headDim], v[base:base+frames*headDim], scores, frames, hidden, h*headDim)
		}
	}
}

// BenchmarkSVLongAttention4Heads_98x128 matches the long-audio encoder frame
// count and isolates the score/softmax/V fused attention path.
func BenchmarkSVLongAttention4Heads_98x128(b *testing.B) {
	const frames, heads, headDim, hidden = 98, 4, 128, 512
	q := make([]float32, heads*frames*headDim)
	k := make([]float32, heads*frames*headDim)
	v := make([]float32, heads*frames*headDim)
	out := make([]float32, frames*hidden)
	scores := make([]float32, frames*frames)
	for i := range q {
		q[i] = float32((i%17)-8) * 0.01
		k[i] = float32((i%13)-6) * 0.02
		v[i] = float32((i%11)-5) * 0.03
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for h := 0; h < heads; h++ {
			base := h * frames * headDim
			svAttnScoresPackedQ128NS(out, q[base:base+frames*headDim], k[base:base+frames*headDim], v[base:base+frames*headDim], scores, frames, hidden, h*headDim)
		}
	}
}

// BenchmarkSVVeryShortFbankLFR covers the frontend fixed cost for 200 ms PCM.
func BenchmarkSVVeryShortFbankLFR(b *testing.B) {
	const samples = 3_200
	pcm := make([]float32, samples)
	for i := range pcm {
		pcm[i] = float32((i%97)-48) * 0.001
	}
	frames := (samples-svWindowSize)/svHopSize + 1
	fbank := make([]float32, frames*svNumMels)
	lfrFrames := (frames-svLFRm)/svLFRn + 1
	if lfrFrames < 1 {
		lfrFrames = 1
	}
	lfr := make([]float32, lfrFrames*svFeatsDim)
	if !svMelFilterbankInto(pcm, fbank) {
		b.Fatal("unexpected short PCM")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if !svMelFilterbankInto(pcm, fbank) {
			b.Fatal("unexpected short PCM")
		}
		svApplyLFRInto(fbank, frames, lfr)
	}
}

func BenchmarkSenseVoiceTranscribe(b *testing.B) {
	modelPath := filepath.Join("..", "..", "sensevoice-small-q8.gguf")
	m, err := NewSenseVoice(modelPath)
	if err != nil {
		b.Skip(err)
	}
	defer m.Close()
	pcm, err := LoadWAV(filepath.Join("..", "..", "明明白2.wav"))
	if err != nil {
		b.Skip(err)
	}
	// warm
	if _, err := m.Transcribe(pcm); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := m.Transcribe(pcm); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkSenseVoiceTranscribeWorkers makes the tensor-pool tuning trade-off
// measurable on real audio. Run explicitly; it is intentionally not a unit test.
func BenchmarkSenseVoiceTranscribeWorkers(b *testing.B) {
	modelPath := filepath.Join("..", "..", "sensevoice-small-q8.gguf")
	pcm, err := LoadWAV(filepath.Join("..", "..", "明明白2.wav"))
	if err != nil {
		b.Skip(err)
	}
	for _, workers := range []int{1, 4, 8, 12} {
		b.Run("workers="+strconv.Itoa(workers), func(b *testing.B) {
			tensor.SetMatMulMaxParallel(workers)
			defer tensor.SetMatMulMaxParallel(0)
			m, err := NewSenseVoice(modelPath)
			if err != nil {
				b.Skip(err)
			}
			defer m.Close()
			if _, err := m.Transcribe(pcm); err != nil {
				b.Fatal(err)
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := m.Transcribe(pcm); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkSenseVoiceShortTranscribeWorkers covers command-length audio, where
// pool dispatch can outweigh the benefit of using every SIMD worker.
func BenchmarkSenseVoiceShortTranscribeWorkers(b *testing.B) {
	modelPath := filepath.Join("..", "..", "sensevoice-small-q8.gguf")
	pcm, err := LoadWAV(filepath.Join("..", "..", "明明白2.wav"))
	if err != nil {
		b.Skip(err)
	}
	const shortSamples = 16_000
	if len(pcm) < shortSamples {
		b.Skip("fixture is shorter than one second")
	}
	pcm = pcm[:shortSamples]
	for _, workers := range []int{1, 4, 8, 12} {
		b.Run("workers="+strconv.Itoa(workers), func(b *testing.B) {
			tensor.SetMatMulMaxParallel(workers)
			defer tensor.SetMatMulMaxParallel(0)
			m, err := NewSenseVoice(modelPath)
			if err != nil {
				b.Skip(err)
			}
			defer m.Close()
			if _, err := m.Transcribe(pcm); err != nil {
				b.Fatal(err)
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := m.Transcribe(pcm); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkSenseVoiceVeryShortTranscribeWorkers(b *testing.B) {
	modelPath := filepath.Join("..", "..", "sensevoice-small-q8.gguf")
	pcm, err := LoadWAV(filepath.Join("..", "..", "明明白2.wav"))
	if err != nil {
		b.Skip(err)
	}
	const veryShortSamples = 3_200
	if len(pcm) < veryShortSamples {
		b.Skip("fixture is shorter than 200 ms")
	}
	pcm = pcm[:veryShortSamples]
	for _, workers := range []int{1, 4, 8, 12} {
		b.Run("workers="+strconv.Itoa(workers), func(b *testing.B) {
			tensor.SetMatMulMaxParallel(workers)
			defer tensor.SetMatMulMaxParallel(0)
			m, err := NewSenseVoice(modelPath)
			if err != nil {
				b.Skip(err)
			}
			defer m.Close()
			if _, err := m.Transcribe(pcm); err != nil {
				b.Fatal(err)
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := m.Transcribe(pcm); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
