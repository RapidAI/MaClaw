package asr

import (
"fmt"
"math"
"os"
"path/filepath"
"testing"
"time"
)

func TestM4A2(t *testing.T) {
modelPath := findModel(t)
wavPath := filepath.Join("..", "..", "明明白2.wav")
if _, err := os.Stat(wavPath); err != nil { t.Skip("not found") }
pcm, err := LoadWAV(wavPath)
if err != nil { t.Fatal(err) }
dur := float64(len(pcm)) / 16000.0
var sumSq, peak float64
for _, s := range pcm { v := float64(s); sumSq += v*v; if a := math.Abs(v); a > peak { peak = a } }
rmsVal := math.Sqrt(sumSq / float64(len(pcm)))
t.Logf("PCM: %.2fs RMS=%.5f Peak=%.4f", dur, rmsVal, peak)
// Gentle normalize
if rmsVal < 0.015 && rmsVal > 0 {
gain := 0.025 / rmsVal
if pg := 0.95/peak; pg < gain { gain = pg }
if gain > 3.0 { gain = 3.0 }
if gain > 1.1 {
t.Logf("normalize gain=%.2fx", gain)
g := float32(gain)
for i, s := range pcm { v := s*g; if v > 1 { v=1 } else if v < -1 { v=-1 }; pcm[i] = v }
}
}
m, err := NewMoonshine(modelPath)
if err != nil { t.Fatal(err) }
defer m.Close()
start := time.Now()
text, err := m.Transcribe(pcm)
if err != nil { t.Fatal(err) }
fmt.Printf("\nExpected: 明明白白我的心 渴望一份真感情\nGot:      %s\nTime:     %dms\n\n", text, time.Since(start).Milliseconds())
t.Logf("result: %q", text)
}