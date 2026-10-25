//go:build darwin || linux

package kokoro

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"syscall"
	"testing"
	"time"
)

// TestZZKokoroBench times Kokoro stages per utterance and optionally dumps
// raw float32 PCM (ZZ_KO_OUT dir) for CPU-vs-Metal SNR/cos comparison.
// Env: ZZ_KO_ASSETS (dir with kokoro-v1_0.koro and voices/), ZZ_KO_VOICE.
func TestZZKokoroBench(t *testing.T) {
	root := os.Getenv("ZZ_KO_ASSETS")
	if root == "" {
		t.Skip("ZZ_KO_ASSETS unset")
	}
	t0 := time.Now()
	m, err := LoadModel(Assets{ConfigPath: filepath.Join(root, "config.json"), WeightsPath: filepath.Join(root, "kokoro-v1_0.koro")})
	if err != nil {
		t.Fatal(err)
	}
	vname := os.Getenv("ZZ_KO_VOICE")
	if vname == "" {
		vname = "af_heart"
	}
	voice, err := m.LoadVoice(filepath.Join(root, "voices"), vname)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("RESULT load_ms=%d", time.Since(t0).Milliseconds())
	utts := []string{
		"həlˈoʊ",
		"ðɪs ɪz ə tˈɛst.",
		benchPhonemes,
		"ðə kwˈɪk bɹˈaʊn fˈɑks dʒˈʌmps ˌoʊvɚ ðə lˈeɪzi dˈɑɡ. ʃiː sˈɛlz sˈiː ʃˈɛlz baɪ ðə sˈiːʃɔːɹ, ænd ðə ʃˈɛlz ʃiː sˈɛlz ɑːɹ ʃˈʊɹli sˈiːʃɛlz.",
		benchPhonemes + " " + benchPhonemes,
	}
	out := os.Getenv("ZZ_KO_OUT")
	var total float64
	for i, ph := range utts {
		var st [4][]float64
		var pcm []float32
		for r := 0; r < 4; r++ {
			a := time.Now()
			cond, err := m.BuildConditioning(ph, voice, 1)
			if err != nil {
				t.Fatal(err)
			}
			b := time.Now()
			f0n, err := m.PredictF0N(cond, voice)
			if err != nil {
				t.Fatal(err)
			}
			c := time.Now()
			feat, err := m.DecoderPreGenerator(cond, f0n, voice)
			if err != nil {
				t.Fatal(err)
			}
			d := time.Now()
			pcm, err = m.GeneratorForward(feat, f0n, voice)
			if err != nil {
				t.Fatal(err)
			}
			e := time.Now()
			if r == 0 {
				continue // warm-up
			}
			for k, v := range []time.Duration{b.Sub(a), c.Sub(b), d.Sub(c), e.Sub(d)} {
				st[k] = append(st[k], float64(v.Microseconds())/1000)
			}
		}
		med := func(v []float64) float64 { s := append([]float64(nil), v...); sort.Float64s(s); return s[len(s)/2] }
		sum := med(st[0]) + med(st[1]) + med(st[2]) + med(st[3])
		total += sum
		t.Logf("utt%d audio=%.2fs cond=%.1f f0n=%.1f dec=%.1f gen=%.1f total=%.1f ms", i, float64(len(pcm))/DefaultSampleRate, med(st[0]), med(st[1]), med(st[2]), med(st[3]), sum)
		if out != "" {
			buf := make([]byte, 4*len(pcm))
			for j, v := range pcm {
				binary.LittleEndian.PutUint32(buf[4*j:], math.Float32bits(v))
			}
			os.MkdirAll(out, 0o755)
			os.WriteFile(filepath.Join(out, "utt"+string(rune('0'+i))+".f32"), buf, 0o644)
		}
	}
	var ru syscall.Rusage
	syscall.Getrusage(syscall.RUSAGE_SELF, &ru)
	maxrss := ru.Maxrss
	if runtime.GOOS == "darwin" {
		maxrss /= 1024
	}
	t.Logf("RESULT total_ms=%.1f maxrss_MB=%d", total, maxrss/1024)
}
