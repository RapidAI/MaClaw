// Use Python's encoder output + Go flow/vocoder to test if the issue is in encoder.
package main

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"

	"github.com/RapidAI/CodeClaw/corelib/asr"
	"github.com/RapidAI/CodeClaw/corelib/tts"
)

func main() {
	// Load Python's official encoder outputs
	pyMp := loadBin("corelib/tts/testdata/ref_fixed_m_p.bin")
	pyLogsP := loadBin("corelib/tts/testdata/ref_fixed_logs_p.bin")
	pyZ := loadBin("corelib/tts/testdata/ref_fixed_z.bin")

	if pyMp == nil || pyLogsP == nil {
		fmt.Println("Missing Python reference files. Run gen_real_audio_fixed.py first.")
		os.Exit(1)
	}

	// These are expanded m_p/logs_p from Python infer() [192, 33]
	tMel := len(pyMp) / 192
	fmt.Printf("Python m_p: %d elements, T_mel=%d\n", len(pyMp), tMel)
	fmt.Printf("Python z: %d elements, mean=%.4f std=%.4f\n", len(pyZ), mean(pyZ), stdev(pyZ))

	hp := tts.DefaultHParams()
	hp.SampleRate = 44100
	hp.HopLength = 512
	w, err := tts.LoadWeightsGGUF("corelib/tts/testdata/melotts-en-fp32.gguf", hp)
	if err != nil {
		fmt.Printf("Load error: %v\n", err)
		os.Exit(1)
	}

	g := make([]float32, hp.GinChannels)
	copy(g, w.SpeakerEmb[0:hp.GinChannels])

	// Option 1: Use Python's z directly (bypass Go flow)
	fmt.Println("\n=== Option 1: Python z → Go vocoder ===")
	audio1 := tts.HiFiGANForward(pyZ, hp.InterChannels, tMel, g, hp.GinChannels, &w.Vocoder, hp)
	fmt.Printf("Audio: %d samples, max=%.4f\n", len(audio1), maxAbs(audio1))
	os.WriteFile("corelib/tts/testdata/roundtrip_py_z.wav", tts.EncodeWAV(audio1, hp.SampleRate), 0644)
	asrTest("corelib/tts/testdata/roundtrip_py_z.wav", audio1, hp.SampleRate)

	// Option 2: Use Python's m_p/logs_p → Go sample + flow + vocoder
	fmt.Println("\n=== Option 2: Python m_p/logs_p → Go sample+flow+vocoder ===")
	zP := make([]float32, hp.InterChannels*tMel)
	tts.RandnScale(zP, 1.0)
	for j := range zP {
		lp := pyLogsP[j]
		if lp > 10 {
			lp = 10
		} else if lp < -20 {
			lp = -20
		}
		zP[j] = pyMp[j] + zP[j]*0.667*float32(math.Exp(float64(lp)))
	}
	z2 := tts.FlowReverseForward(zP, hp.InterChannels, tMel, g, hp.GinChannels, &w.Flow, hp)
	audio2 := tts.HiFiGANForward(z2, hp.InterChannels, tMel, g, hp.GinChannels, &w.Vocoder, hp)
	fmt.Printf("Audio: %d samples, max=%.4f\n", len(audio2), maxAbs(audio2))
	os.WriteFile("corelib/tts/testdata/roundtrip_py_mp.wav", tts.EncodeWAV(audio2, hp.SampleRate), 0644)
	asrTest("corelib/tts/testdata/roundtrip_py_mp.wav", audio2, hp.SampleRate)
}

func asrTest(name string, audio []float32, sr int) {
	asrGGUF := "RapidSpeech.cpp/models/gguf/moonshine-base-zh.gguf"
	asrModel, err := asr.NewMoonshine(asrGGUF)
	if err != nil {
		fmt.Printf("  ASR load error: %v\n", err)
		return
	}
	defer asrModel.Close()

	pcm16k := resample(audio, sr, 16000)
	text, err := asrModel.Transcribe(pcm16k)
	if err != nil {
		fmt.Printf("  ASR error: %v\n", err)
		return
	}
	fmt.Printf("  ASR result: %q\n", text)
}

func resample(in []float32, srcRate, dstRate int) []float32 {
	outLen := int(int64(len(in)) * int64(dstRate) / int64(srcRate))
	out := make([]float32, outLen)
	ratio := float64(srcRate) / float64(dstRate)
	for i := 0; i < outLen; i++ {
		pos := float64(i) * ratio
		idx := int(pos)
		frac := float32(pos - float64(idx))
		s0 := in[idx]
		s1 := s0
		if idx+1 < len(in) {
			s1 = in[idx+1]
		}
		out[i] = s0*(1-frac) + s1*frac
	}
	return out
}

func loadBin(path string) []float32 {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	n := len(data) / 4
	result := make([]float32, n)
	for i := 0; i < n; i++ {
		result[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[i*4:]))
	}
	return result
}

func mean(x []float32) float32 {
	var s float32
	for _, v := range x {
		s += v
	}
	return s / float32(len(x))
}

func stdev(x []float32) float32 {
	m := mean(x)
	var s float32
	for _, v := range x {
		d := v - m
		s += d * d
	}
	return float32(math.Sqrt(float64(s / float32(len(x)))))
}

func maxAbs(x []float32) float32 {
	var m float32
	for _, v := range x {
		a := float32(math.Abs(float64(v)))
		if a > m {
			m = a
		}
	}
	return m
}
