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

	data, _ := os.ReadFile(filepath.Join("corelib", "tts", "testdata", "ref_onnx_zp_你好世界.bin"))
	inter := 192
	tMel := len(data) / 4 / inter
	zp := make([]float32, inter*tMel)
	for i := range zp {
		zp[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[i*4 : (i+1)*4]))
	}

	hp := model.HP
	z := tts.PiperFlowReverseForward(zp, inter, tMel, &model.W.Flow, hp)
	audio := tts.PiperHiFiGANForward(z, inter, tMel, &model.W.Vocoder, hp)

	wav := tts.EncodeWAV(audio, hp.SampleRate)
	wavPath := filepath.Join("corelib", "tts", "testdata", "go_onnx_zp_你好世界.wav")
	os.WriteFile(wavPath, wav, 0644)
	fmt.Printf("Go from ONNX z_p: %d samples, %.2fs\n", len(audio), float64(len(audio))/22050)
}
