package kokoro

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// benchPhonemes is a mid-length English phoneme sequence covering common
// vocab tokens, roughly 10 seconds of synthesized audio.
const benchPhonemes = "ðɪs ɪz ə tˈɛst əv ðə tˈɛkst tᵊ spˈitʃ ˈɛndʒᵻn. wɪ mˈɛʒɚ ðə pɚfˈɔɹməns əv ðə ɪnfɹəns pˈaɪplˌaɪn wɪð ə lˈɔŋɡɚ sˈɛntəns tᵊ ɪmˈɪtˌeɪt ɹˈiːəl ˈjuːsɪdʒ."

func benchAssetsRoot(b *testing.B) string {
	b.Helper()
	if root := os.Getenv("KOKORO_GO_ASSETS"); root != "" {
		return root
	}
	for _, cand := range []string{
		filepath.Join("..", "..", "..", "tts_eval", "kokoro_go_assets_q8"),
		filepath.Join("..", "..", "..", "tts_eval", "kokoro_go_assets"),
	} {
		if _, err := os.Stat(filepath.Join(cand, "kokoro-v1_0.koro")); err == nil {
			return cand
		}
	}
	b.Skip("no kokoro assets found; set KOKORO_GO_ASSETS")
	return ""
}

func loadBenchModel(b *testing.B) (*Model, *TensorFile) {
	b.Helper()
	root := benchAssetsRoot(b)
	model, err := LoadModel(Assets{
		ConfigPath:  filepath.Join(root, "config.json"),
		WeightsPath: filepath.Join(root, "kokoro-v1_0.koro"),
	})
	if err != nil {
		b.Fatal(err)
	}
	voice, err := model.LoadVoice(filepath.Join(root, "voices"), "zm_yunxi")
	if err != nil {
		b.Fatal(err)
	}
	return model, voice
}

// BenchmarkSynthesizePhonemes measures end-to-end Kokoro inference time and
// reports the real-time factor (RTF = inference seconds per audio second).
func BenchmarkSynthesizePhonemes(b *testing.B) {
	model, voice := loadBenchModel(b)
	// Warm up once so lazy buffers/pools are allocated outside the timer.
	if _, err := model.SynthesizePhonemes(benchPhonemes, voice, 1); err != nil {
		b.Fatal(err)
	}
	var lastLen int
	b.ResetTimer()
	start := time.Now()
	for i := 0; i < b.N; i++ {
		pcm, err := model.SynthesizePhonemes(benchPhonemes, voice, 1)
		if err != nil {
			b.Fatal(err)
		}
		lastLen = len(pcm)
	}
	elapsed := time.Since(start)
	audioSec := float64(lastLen) / float64(DefaultSampleRate)
	b.ReportMetric(audioSec, "audio_s")
	b.ReportMetric(elapsed.Seconds()/float64(b.N)/audioSec, "RTF")
}

// BenchmarkSynthesizePhonemesShort stresses the text-encoder/decoder path
// with a short utterance (few frames).
func BenchmarkSynthesizePhonemesShort(b *testing.B) {
	model, voice := loadBenchModel(b)
	if _, err := model.SynthesizePhonemes("həlˈoʊ", voice, 1); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := model.SynthesizePhonemes("həlˈoʊ", voice, 1); err != nil {
			b.Fatal(err)
		}
	}
}

// keep strings import used for future phoneme padding experiments
var _ = strings.Repeat

// BenchmarkSynthesizePhonemesAB alternates the round-2 optimizations on/off
// every iteration inside a single process, so machine-load drift cancels out.
// Reports median seconds/op and RTF for both configurations.
func BenchmarkSynthesizePhonemesAB(b *testing.B) {
	model, voice := loadBenchModel(b)
	if _, err := model.SynthesizePhonemes(benchPhonemes, voice, 1); err != nil {
		b.Fatal(err)
	}
	const iters = 12 // 6 per variant
	samples := make([]struct {
		on  float64
		off float64
	}, iters/2)
	b.ResetTimer()
	for i := 0; i < iters; i++ {
		on := i%2 == 0
		setRound2Opts(on)
		t0 := time.Now()
		if _, err := model.SynthesizePhonemes(benchPhonemes, voice, 1); err != nil {
			b.Fatal(err)
		}
		d := time.Since(t0).Seconds()
		if on {
			samples[i/2].on = d
		} else {
			samples[i/2].off = d
		}
	}
	setRound2Opts(true)
	median := func(vals []float64) float64 {
		sorted := append([]float32(nil), 0)[:0]
		_ = sorted
		s := append([]float64(nil), vals...)
		sort.Float64s(s)
		return s[len(s)/2]
	}
	var ons, offs []float64
	for i := range samples {
		ons = append(ons, samples[i].on)
		offs = append(offs, samples[i].off)
	}
	mon, moff := median(ons), median(offs)
	audioSec := 8.600
	b.ReportMetric(mon, "on_s/op")
	b.ReportMetric(moff, "off_s/op")
	b.ReportMetric(100*(moff-mon)/moff, "delta_pct")
	b.ReportMetric(mon/audioSec, "on_RTF")
	b.ReportMetric(moff/audioSec, "off_RTF")
}
