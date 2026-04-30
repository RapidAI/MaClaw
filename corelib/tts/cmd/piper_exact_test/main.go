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
	model, err := tts.NewPiper(modelPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed: %v\n", err)
		os.Exit(1)
	}

	// Load ONNX's exact /Add_output_0 (z_p before flip, [192, 114])
	data, _ := os.ReadFile(filepath.Join("corelib", "tts", "testdata", "ref_onnx_add_output.bin"))
	inter := 192
	tMel := 114
	addOut := make([]float32, inter*tMel)
	for i := range addOut {
		addOut[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[i*4 : (i+1)*4]))
	}

	fmt.Printf("ONNX /Add_output_0: [%d, %d], RMS=%.6f, ch0[:3]=[%.6f, %.6f, %.6f]\n",
		inter, tMel, rms(addOut), addOut[0], addOut[1], addOut[2])

	// Run Go flow decoder on this exact input
	hp := model.HP
	z := tts.PiperFlowReverseForward(addOut, inter, tMel, &model.W.Flow, hp)

	fmt.Printf("Go z: RMS=%.6f, ch0[:3]=[%.6f, %.6f, %.6f]\n",
		rms(z), z[0], z[1], z[2])
	fmt.Printf("ONNX z: RMS=1.585369, ch0[:3]=[0.526267, 0.529666, 0.503981]\n")

	// Run vocoder
	audio := tts.PiperHiFiGANForward(z, inter, tMel, &model.W.Vocoder, hp)
	fmt.Printf("Go audio: %d samples, RMS=%.6f\n", len(audio), rms(audio))

	wav := tts.EncodeWAV(audio, hp.SampleRate)
	wavPath := filepath.Join("corelib", "tts", "testdata", "go_piper_exact_zp_你好世界.wav")
	os.WriteFile(wavPath, wav, 0644)
	fmt.Printf("Saved: %s\n", wavPath)
}

func rms(x []float32) float64 {
	var s float64
	for _, v := range x {
		s += float64(v) * float64(v)
	}
	return math.Sqrt(s / float64(len(x)))
}
