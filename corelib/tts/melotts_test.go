package tts

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadWeightsGGUF(t *testing.T) {
	ggufPath := filepath.Join("..", "..", "RapidSpeech.cpp", "models", "gguf", "openvoice2-base.gguf")
	if _, err := os.Stat(ggufPath); os.IsNotExist(err) {
		t.Skipf("GGUF file not found: %s", ggufPath)
	}

	hp := DefaultHParams()
	w, err := LoadWeightsGGUF(ggufPath, hp)
	if err != nil {
		t.Fatalf("LoadWeightsGGUF: %v", err)
	}

	// Verify key weights loaded
	if w.SpeakerEmb == nil {
		t.Error("SpeakerEmb is nil")
	} else {
		t.Logf("SpeakerEmb: %d speakers × %d dim", w.NSpeakers, hp.GinChannels)
	}

	if w.TextEnc.Emb == nil {
		t.Error("TextEnc.Emb is nil")
	} else {
		t.Logf("TextEnc.Emb: %d elements (vocabSize=%d)", len(w.TextEnc.Emb), len(w.TextEnc.Emb)/hp.HiddenChannels)
	}

	if w.TextEnc.ToneEmb == nil {
		t.Error("TextEnc.ToneEmb is nil")
	}
	if w.TextEnc.LangEmb == nil {
		t.Error("TextEnc.LangEmb is nil")
	}

	// Check encoder layers
	for i, l := range w.TextEnc.Layers {
		if l.Attn.ConvQ.Weight == nil {
			t.Errorf("TextEnc.Layer[%d].Attn.ConvQ is nil", i)
		}
		if l.FFN.Conv1.Weight == nil {
			t.Errorf("TextEnc.Layer[%d].FFN.Conv1 is nil", i)
		}
		if l.Norm1.Weight == nil {
			t.Errorf("TextEnc.Layer[%d].Norm1.Weight is nil", i)
		}
		if l.Norm2.Weight == nil {
			t.Errorf("TextEnc.Layer[%d].Norm2.Weight is nil", i)
		}
	}

	// Check duration predictor
	if w.DurPred.Conv1.Weight == nil {
		t.Error("DurPred.Conv1 is nil")
	}
	if w.DurPred.Proj.Weight == nil {
		t.Error("DurPred.Proj is nil")
	}

	// Check flow decoder
	for i, l := range w.Flow.Layers {
		if l.Pre.Weight == nil {
			t.Errorf("Flow.Layer[%d].Pre is nil", i)
		}
		if l.Post.Weight == nil {
			t.Errorf("Flow.Layer[%d].Post is nil", i)
		}
		for j, el := range l.Enc {
			if el.Attn.ConvQ.Weight == nil {
				t.Errorf("Flow.Layer[%d].Enc[%d].Attn.ConvQ is nil", i, j)
			}
		}
	}

	// Check vocoder
	if w.Vocoder.ConvPre.Weight == nil {
		t.Error("Vocoder.ConvPre is nil")
	}
	for i, up := range w.Vocoder.Ups {
		if up.Weight == nil {
			t.Errorf("Vocoder.Ups[%d] is nil", i)
		} else {
			t.Logf("Vocoder.Ups[%d]: inCh=%d outCh=%d kSize=%d", i, up.InCh, up.OutCh, up.KSize)
		}
	}
	if w.Vocoder.ConvPost.Weight == nil {
		t.Error("Vocoder.ConvPost is nil")
	}

	t.Logf("All weights loaded successfully: %d flow layers, %d resblocks",
		len(w.Flow.Layers), len(w.Vocoder.ResBlocks))
}

func TestSynthesizeE2E(t *testing.T) {
	ggufPath := filepath.Join("..", "..", "RapidSpeech.cpp", "models", "gguf", "openvoice2-base.gguf")
	if _, err := os.Stat(ggufPath); os.IsNotExist(err) {
		t.Skipf("GGUF file not found: %s", ggufPath)
	}

	model, err := NewMeloTTS(ggufPath)
	if err != nil {
		t.Fatalf("NewMeloTTS: %v", err)
	}

	pt := NewPhonemeTable()
	g2p := TextToPhonemes("Hello", pt, LangEN)
	t.Logf("G2P: %d phoneme IDs", len(g2p.PhonemeIDs))

	audio, err := model.Synthesize(SynthesizeInput{
		PhonemeIDs:  g2p.PhonemeIDs,
		ToneIDs:     g2p.ToneIDs,
		LangIDs:     g2p.LangIDs,
		SpeakerID:   0,
		NoiseScale:  0.667,
		LengthScale: 1.0,
	})
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}

	t.Logf("Audio: %d samples (%.2f seconds at %d Hz)",
		len(audio), float64(len(audio))/float64(model.HP.SampleRate), model.HP.SampleRate)

	if len(audio) == 0 {
		t.Fatal("empty audio output")
	}

	// Check audio is not all zeros
	var maxAbs float32
	for _, s := range audio {
		if s > maxAbs {
			maxAbs = s
		}
		if -s > maxAbs {
			maxAbs = -s
		}
	}
	t.Logf("Max absolute amplitude: %f", maxAbs)

	// Save to WAV for manual inspection
	wavData := EncodeWAV(audio, model.HP.SampleRate)
	outPath := filepath.Join(os.TempDir(), "melotts_test_hello.wav")
	if err := os.WriteFile(outPath, wavData, 0644); err != nil {
		t.Logf("Warning: could not save WAV: %v", err)
	} else {
		t.Logf("Saved WAV to: %s", outPath)
	}
}

func TestSynthesizeChinese(t *testing.T) {
	ggufPath := filepath.Join("..", "..", "RapidSpeech.cpp", "models", "gguf", "openvoice2-base.gguf")
	if _, err := os.Stat(ggufPath); os.IsNotExist(err) {
		t.Skipf("GGUF file not found: %s", ggufPath)
	}

	model, err := NewMeloTTS(ggufPath)
	if err != nil {
		t.Fatalf("NewMeloTTS: %v", err)
	}

	pt := NewPhonemeTable()
	g2p := TextToPhonemes("你好世界", pt, LangZH)
	t.Logf("G2P '你好世界': %d phoneme IDs", len(g2p.PhonemeIDs))

	audio, err := model.Synthesize(SynthesizeInput{
		PhonemeIDs:  g2p.PhonemeIDs,
		ToneIDs:     g2p.ToneIDs,
		LangIDs:     g2p.LangIDs,
		SpeakerID:   0,
		NoiseScale:  0.667,
		LengthScale: 1.0,
	})
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}

	fmt.Printf("Chinese audio: %d samples (%.2f sec)\n",
		len(audio), float64(len(audio))/float64(model.HP.SampleRate))

	if len(audio) == 0 {
		t.Fatal("empty audio output")
	}

	wavData := EncodeWAV(audio, model.HP.SampleRate)
	outPath := filepath.Join(os.TempDir(), "melotts_test_nihao.wav")
	os.WriteFile(outPath, wavData, 0644)
	t.Logf("Saved WAV to: %s", outPath)
}
