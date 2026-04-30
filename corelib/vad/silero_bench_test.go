package vad

import (
	"testing"
)

func BenchmarkDetect(b *testing.B) {
	m, err := Load()
	if err != nil {
		b.Fatal(err)
	}
	state := m.NewState()
	pcm := make([]float32, m.hp.WindowSize)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Detect(pcm, state)
	}
}

func BenchmarkFilterSpeech10s(b *testing.B) {
	m, err := Load()
	if err != nil {
		b.Fatal(err)
	}
	// 10 seconds of audio at 16kHz
	pcm := make([]float32, 16000*10)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.FilterSpeech(pcm)
	}
}

// Micro-benchmarks for individual operators

func BenchmarkConv1dStride_STFT(b *testing.B) {
	m, err := Load()
	if err != nil {
		b.Fatal(err)
	}
	input := make([]float32, 512)
	T := (512 - 256) / 128 + 1
	dst := make([]float32, 258*T)
	b.Run("scalar", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			conv1dStrideInto(dst, input, m.w.stft.W, 258, 256, 128)
		}
	})
	b.Run("simd", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			conv1dStrideSimd(dst, input, m.w.stft.W, 258, 256, 128)
		}
	})
}

func BenchmarkConv1dPad1_Enc0(b *testing.B) {
	m, err := Load()
	if err != nil {
		b.Fatal(err)
	}
	T := 3
	input := make([]float32, 129*T)
	dst := make([]float32, 128*T)
	enc := &m.w.encoder[0]
	b.Run("scalar", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			conv1dPad1Into(dst, input, 129, T, enc.W, enc.B, 128, 129, 3)
		}
	})
}

func BenchmarkLSTMCell(b *testing.B) {
	m, err := Load()
	if err != nil {
		b.Fatal(err)
	}
	x := make([]float32, 128)
	h := make([]float32, 128)
	c := make([]float32, 128)
	gates := make([]float32, 512)
	b.Run("scalar", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			lstmCellInPlace(x, h, c, gates, m.w.lstmWIH, m.w.lstmWHH, m.w.lstmBIH, m.w.lstmBHH, 128, 128)
		}
	})
	b.Run("simd", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			lstmCellSimd(x, h, c, gates, m.w.lstmWIH, m.w.lstmWHH, m.w.lstmBIH, m.w.lstmBHH, 128, 128)
		}
	})
}
