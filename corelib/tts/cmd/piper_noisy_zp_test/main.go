package main

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"

	"github.com/RapidAI/CodeClaw/corelib/tts"
)

func main() {
	modelPath := filepath.Join("corelib", "tts", "testdata", "piper-xiao_ya-zh-fp32.gguf")
	model, _ := tts.NewPiper(modelPath)

	// Load ONNX noisy z_p [192, 153]
	data, _ := os.ReadFile(filepath.Join("corelib", "tts", "testdata", "ref_onnx_noisy_zp_今天天气不错.bin"))
	inter := 192
	tMel := len(data) / 4 / inter // 153
	zp := make([]float32, inter*tMel)
	for i := range zp {
		zp[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[i*4 : (i+1)*4]))
	}
	fmt.Printf("Loaded noisy z_p: [%d, %d], RMS=%.4f\n", inter, tMel, rms(zp))

	// Flow decoder
	hp := model.HP
	z := tts.PiperFlowReverseForward(zp, inter, tMel, &model.W.Flow, hp)
	fmt.Printf("z: RMS=%.4f\n", rms(z))

	// Vocoder
	audio := tts.PiperHiFiGANForward(z, inter, tMel, &model.W.Vocoder, hp)
	fmt.Printf("audio: %d samples, %.2fs, RMS=%.4f\n", len(audio), float64(len(audio))/22050, rms(audio))

	wav := tts.EncodeWAV(audio, hp.SampleRate)
	wavPath := filepath.Join("corelib", "tts", "testdata", "go_from_onnx_noisy_zp_今天天气不错.wav")
	os.WriteFile(wavPath, wav, 0644)
	fmt.Printf("Saved: %s\n", wavPath)
}

func rms(x []float32) float64 {
	var s float64
	for _, v := range x { s += float64(v) * float64(v) }
	return math.Sqrt(s / float64(len(x)))
}
