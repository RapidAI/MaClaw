package tts

import (
	"fmt"
	"math"
)

// MeloTTSModel is a pure Go MeloTTS inference engine.
type MeloTTSModel struct {
	HP HParams
	W  *Weights
}

// NewMeloTTS loads a MeloTTS model from a GGUF file.
func NewMeloTTS(modelPath string) (*MeloTTSModel, error) {
	hp := DefaultHParams()
	w, err := LoadWeightsGGUF(modelPath, hp)
	if err != nil {
		return nil, fmt.Errorf("tts: load weights: %w", err)
	}
	return &MeloTTSModel{HP: hp, W: w}, nil
}

// SynthesizeInput holds the preprocessed input for synthesis.
type SynthesizeInput struct {
	PhonemeIDs []int
	ToneIDs    []int
	LangIDs    []int
	SpeakerID  int
	NoiseScale float32 // default 0.667
	LengthScale float32 // default 1.0
}

// Synthesize runs the full TTS inference pipeline.
// Returns PCM float32 audio samples at model sample rate (22050 Hz).
func (m *MeloTTSModel) Synthesize(input SynthesizeInput) ([]float32, error) {
	hp := m.HP
	w := m.W

	if len(input.PhonemeIDs) == 0 {
		return nil, fmt.Errorf("tts: empty phoneme IDs")
	}
	if input.NoiseScale == 0 {
		input.NoiseScale = 0.667
	}
	if input.LengthScale == 0 {
		input.LengthScale = 1.0
	}

	// Step 1: Speaker embedding
	var g []float32
	if w.SpeakerEmb != nil && input.SpeakerID < w.NSpeakers {
		g = make([]float32, hp.GinChannels)
		copy(g, w.SpeakerEmb[input.SpeakerID*hp.GinChannels:(input.SpeakerID+1)*hp.GinChannels])
	}

	// Step 2: Text Encoder
	x, mP, logsP, T := TextEncoderForward(
		input.PhonemeIDs, input.ToneIDs, input.LangIDs,
		g, hp.GinChannels,
		nil, nil, // BERT embeddings (nil = zeros)
		&w.TextEnc, hp,
	)
	_ = x // encoder hidden states (used by duration predictor)

	// Step 3: Duration Prediction (deterministic only, sdp_ratio=0)
	logw := DurationPredictorForward(x, hp.HiddenChannels, T, g, hp.GinChannels, &w.DurPred)
	durations, tMel := ComputeDurations(logw, input.LengthScale)

	// Step 4: Expand prior by durations
	path, _ := GeneratePath(durations)
	mPExpanded := ExpandByDurations(mP, hp.InterChannels, T, path, tMel)
	logsPExpanded := ExpandByDurations(logsP, hp.InterChannels, T, path, tMel)

	// Step 5: Sample latent
	zP := make([]float32, hp.InterChannels*tMel)
	if input.NoiseScale > 0 {
		// Use real random noise (standard normal)
		RandnScale(zP, 1.0)
		for i := range zP {
			lp := logsPExpanded[i]
			if lp > 10 {
				lp = 10
			} else if lp < -20 {
				lp = -20
			}
			zP[i] = mPExpanded[i] + zP[i]*input.NoiseScale*float32(math.Exp(float64(lp)))
		}
	} else {
		// Deterministic: z_p = m_p
		copy(zP, mPExpanded)
	}

	// Step 6: Flow decoder (reverse)
	z := FlowReverseForward(zP, hp.InterChannels, tMel, g, hp.GinChannels, &w.Flow, hp)

	// Apply mask (all 1s for single utterance)
	// z = z * y_mask — no-op

	// Step 7: HiFi-GAN vocoder
	audio := HiFiGANForward(z, hp.InterChannels, tMel, g, hp.GinChannels, &w.Vocoder, hp)

	return audio, nil
}

// SynthesizeToWAV runs synthesis and returns a WAV file byte slice.
func (m *MeloTTSModel) SynthesizeToWAV(input SynthesizeInput) ([]byte, error) {
	audio, err := m.Synthesize(input)
	if err != nil {
		return nil, err
	}
	return EncodeWAV(audio, m.HP.SampleRate), nil
}
