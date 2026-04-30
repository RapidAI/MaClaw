package tts

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/embedding/tensor"
)

func TestCompareVocoderStages(t *testing.T) {
	if _, err := os.Stat(testGGUF); os.IsNotExist(err) {
		t.Skip("test GGUF not found")
	}

	hp := DefaultHParams()
	w, err := LoadWeightsGGUF(testGGUF, hp)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	pyZFinal := loadRef(t, "ref_flow_04_z_final")
	pySpeakerEmb := loadRef(t, "ref_00_speaker_emb")
	pyAudioFinal := loadRef(t, "ref_voc_audio_final")

	tMel := 31
	ch := hp.UpsampleInitialChannel
	T := tMel

	// conv_pre + speaker cond
	x := Conv1D(pyZFinal, hp.InterChannels, T,
		w.Vocoder.ConvPre.Weight, w.Vocoder.ConvPre.KSize, ch, 1,
		(w.Vocoder.ConvPre.KSize-1)/2, w.Vocoder.ConvPre.Bias)

	if pySpeakerEmb != nil && w.Vocoder.Cond.Weight != nil {
		gProj := Conv1D(pySpeakerEmb, hp.GinChannels, 1,
			w.Vocoder.Cond.Weight, 1, ch, 1, 0, w.Vocoder.Cond.Bias)
		for c := 0; c < ch; c++ {
			gVal := gProj[c]
			for ti := 0; ti < T; ti++ {
				x[c*T+ti] += gVal
			}
		}
	}

	nResKernels := len(hp.ResblockKernelSizes)

	for i, upRate := range hp.UpsampleRates {
		pyStage := loadRef(t, fmt.Sprintf("ref_voc_stage_%d", i))

		LeakyReLU(x, lreluSlope)
		up := &w.Vocoder.Ups[i]
		newCh := ch / 2
		padding := (up.KSize - upRate) / 2
		x = ConvTranspose1D(x, ch, T, up.Weight, up.KSize, newCh, upRate, padding, up.Bias)
		ch = newCh
		T = T * upRate

		var sum []float32
		for j := 0; j < nResKernels; j++ {
			rbIdx := i*nResKernels + j
			rb := &w.Vocoder.ResBlocks[rbIdx]
			dilations := hp.ResblockDilationSizes[j]
			xClone := make([]float32, len(x))
			copy(xClone, x)
			xClone = ResBlock1Forward(xClone, ch, T, rb, dilations)
			if sum == nil {
				sum = xClone
			} else {
				tensor.Add(sum, sum, xClone)
			}
		}
		scale := 1.0 / float32(nResKernels)
		tensor.Scale(sum, scale)
		x = sum

		if len(x) == len(pyStage) {
			maxD, _ := report(t, fmt.Sprintf("stage_%d", i), x, pyStage)
			if maxD > 1.0 {
				t.Errorf("stage_%d diff: %.4f", i, maxD)
				return
			}
		} else {
			t.Errorf("stage_%d size: Go=%d Py=%d", i, len(x), len(pyStage))
			return
		}
	}

	// Final
	LeakyReLU(x, lreluSlope)
	audio := Conv1D(x, ch, T, w.Vocoder.ConvPost.Weight, w.Vocoder.ConvPost.KSize, 1, 1,
		(w.Vocoder.ConvPost.KSize-1)/2, w.Vocoder.ConvPost.Bias)
	tensor.Tanh(audio)

	if len(audio) == len(pyAudioFinal) {
		maxD, _ := report(t, "audio_final", audio, pyAudioFinal)
		if maxD > 0.01 {
			t.Errorf("audio diff: %.6f", maxD)
		}
	}

	wavData := EncodeWAV(audio, hp.SampleRate)
	os.WriteFile(filepath.Join("testdata", "go_voc_staged.wav"), wavData, 0644)
	t.Logf("Saved go_voc_staged.wav")
}
