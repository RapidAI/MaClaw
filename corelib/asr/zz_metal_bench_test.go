package asr

// SenseVoice CPU vs Metal harness (skipped unless ZZ_SV_MODEL is set):
//
//	ZZ_SV_MODEL=<gguf> ZZ_SV_WAVS=<dir of 16k mono PCM16 wavs> go test -run TestZZSenseVoiceMetal -v

import (
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

func zzReadWav16k(t *testing.T, p string) []float32 {
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	// find "data" chunk
	i := 12
	for i+8 <= len(b) {
		id, n := string(b[i:i+4]), int(binary.LittleEndian.Uint32(b[i+4:i+8]))
		if id == "data" {
			d := b[i+8 : min(len(b), i+8+n)]
			out := make([]float32, len(d)/2)
			for j := range out {
				out[j] = float32(int16(binary.LittleEndian.Uint16(d[2*j:]))) / 32768
			}
			return out
		}
		i += 8 + n + n&1
	}
	t.Fatalf("%s: no data chunk", p)
	return nil
}

func zzRSSMB() string {
	out, err := exec.Command("ps", "-o", "rss=", "-p", strconv.Itoa(os.Getpid())).Output()
	if err != nil {
		return "?"
	}
	kb, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return strconv.Itoa(kb / 1024)
}

func TestZZSenseVoiceMetal(t *testing.T) {
	model := os.Getenv("ZZ_SV_MODEL")
	if model == "" {
		t.Skip("ZZ_SV_MODEL not set")
	}
	files, _ := filepath.Glob(filepath.Join(os.Getenv("ZZ_SV_WAVS"), "*.wav"))
	sort.Strings(files)
	var pcms [][]float32
	var audioS float64
	for _, f := range files {
		p := zzReadWav16k(t, f)
		pcms = append(pcms, p)
		audioS += float64(len(p)) / 16000
	}
	run := func(tag string) []string {
		t0 := time.Now()
		m, err := NewSenseVoice(model)
		if err != nil {
			t.Fatal(err)
		}
		defer m.Close()
		on, why := m.GPUBackend()
		gw, _ := m.GPUMemoryBytes()
		t.Logf("%s: load_ms=%.0f metal=%v reason=%q gpu_weights_MB=%.0f rss_MB=%s", tag, float64(time.Since(t0).Microseconds())/1000, on, why, float64(gw)/1e6, zzRSSMB())
		texts := make([]string, len(pcms))
		var total time.Duration
		var longest time.Duration
		for i, p := range pcms {
			m.Transcribe(p) // warm
			var ds []time.Duration
			for k := 0; k < 3; k++ {
				t1 := time.Now()
				s, err := m.Transcribe(p)
				if err != nil {
					t.Fatal(err)
				}
				ds = append(ds, time.Since(t1))
				texts[i] = s
			}
			sort.Slice(ds, func(a, b int) bool { return ds[a] < ds[b] })
			total += ds[1]
			if len(p) == zzMaxLen(pcms) {
				longest = ds[1]
			}
		}
		_, ga := m.GPUMemoryBytes()
		t.Logf("RESULT %s files=%d audio_s=%.1f total_ms=%.1f RTF=%.4f longest_file_ms=%.1f (%.1fs audio) gpu_acts_MB=%.0f rss_MB=%s", tag, len(pcms), audioS,
			float64(total.Microseconds())/1000, total.Seconds()/audioS, float64(longest.Microseconds())/1000, float64(zzMaxLen(pcms))/16000, float64(ga)/1e6, zzRSSMB())
		return texts
	}
	gpu := run("metal")
	os.Setenv("MACLAW_SENSEVOICE_METAL", "0")
	cpu := run("cpu")
	same := 0
	for i := range gpu {
		if gpu[i] == cpu[i] {
			same++
		} else {
			t.Logf("DIFF %s\n  cpu=%q\n  gpu=%q", filepath.Base(files[i]), cpu[i], gpu[i])
		}
	}
	t.Logf("RESULT identical_transcripts=%d/%d", same, len(gpu))
	if os.Getenv("ZZ_SV_SHOW") != "" {
		for i := range cpu {
			t.Logf("%s: %s", filepath.Base(files[i]), cpu[i])
		}
	}
}

func zzMaxLen(p [][]float32) int {
	m := 0
	for _, x := range p {
		m = max(m, len(x))
	}
	return m
}
