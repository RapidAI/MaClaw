// Command pcmcheck dumps PCM statistics for waveform regression checks.
package main

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"

	"github.com/RapidAI/CodeClaw/corelib/tts/kokoro"
)

func main() {
	root := os.Getenv("KOKORO_GO_ASSETS")
	if root == "" {
		root = filepath.Join("tts_eval", "kokoro_go_assets")
	}
	model, err := kokoro.LoadModel(kokoro.Assets{
		ConfigPath:  filepath.Join(root, "config.json"),
		WeightsPath: filepath.Join(root, "kokoro-v1_0.koro"),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	voice, err := model.LoadVoice(filepath.Join(root, "voices"), "zm_yunxi")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	texts := []string{
		"həlˈoʊ wɜːld ðɪs ɪz ə tˈɛst əv ðə spˈitʃ ˈɛndʒᵻn wɪð nˈʌmbɚz wʌn tuː θɹiː fɔː fˈaɪv sˈɪks ˈsɛvᵻn.",
		"ˈaɪ wʊd laɪk tʊ ˈoʊpᵻn ðə wˈɪndəʊ ænd lˈɛt sˈʌnʃˌaɪn ˈɪntuː ðə ɹˈuːm bɪfˈɔː lˈʌntʃ.",
	}
	for ti, text := range texts {
		pcm, err := model.SynthesizePhonemes(text, voice, 1)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		sum := sha256.Sum256(unsafeF32Bytes(pcm))
		sumSq, peak, bad := 0.0, float32(0), 0
		for _, v := range pcm {
			if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
				bad++
				continue
			}
			sumSq += float64(v) * float64(v)
			if v > peak {
				peak = v
			}
			if -v > peak {
				peak = -v
			}
		}
		fmt.Printf("text%d len=%d sha=%x rms=%.6f peak=%.4f nonfinite=%d\n",
			ti, len(pcm), sum[:8], math.Sqrt(sumSq/float64(len(pcm))), peak, bad)
	}
}

func unsafeF32Bytes(p []float32) []byte {
	buf := make([]byte, 4*len(p))
	for i, v := range p {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(v))
	}
	return buf
}
