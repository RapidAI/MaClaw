package tts

import (
	"fmt"
	"math"
)

// PiperModel is a pure Go Piper VITS inference engine.
type PiperModel struct {
	HP  PiperHParams
	W   *PiperWeights
	Lex *PiperLexicon
	// Duration prediction
	DurMLPW1 []float32
	DurMLPB1 []float32
	DurMLPW2 []float32
	DurMLPB2 float32
	DurCache *DurationCache
	// English pronunciation (CMU Pronouncing Dictionary)
	CMUDict *CMUDict
}

// NewPiper loads a Piper VITS model from a GGUF file.
// lexiconPath is optional — if provided, uses the lexicon for accurate G2P.
func NewPiper(modelPath string, lexiconPath ...string) (*PiperModel, error) {
	hp := DefaultPiperHParams()
	w, err := LoadPiperWeightsGGUF(modelPath, hp)
	if err != nil {
		return nil, fmt.Errorf("piper: load weights: %w", err)
	}
	m := &PiperModel{HP: hp, W: w}
	if len(lexiconPath) > 0 && lexiconPath[0] != "" {
		lex, err := LoadPiperLexicon(lexiconPath[0])
		if err == nil {
			m.Lex = lex
		}
	}
	return m, nil
}

// PiperSynthesizeInput holds input for Piper synthesis.
type PiperSynthesizeInput struct {
	PhonemeIDs  []int64
	NoiseScale  float32 // default 0.667
	NoiseScaleW float32 // default 0.8 (duration noise)
	LengthScale float32 // default 1.0
	SpeakerID   int
	// WordInternalSeps: indices in PhonemeIDs where "_" separators are
	// word-internal (English syllable boundaries). Duration is halved for these.
	WordInternalSeps []int
}

// Synthesize runs the full Piper VITS inference pipeline.
// Returns PCM float32 audio samples at 22050 Hz.
func (m *PiperModel) Synthesize(input PiperSynthesizeInput) ([]float32, error) {
	hp := m.HP
	w := m.W

	if len(input.PhonemeIDs) == 0 {
		return nil, fmt.Errorf("piper: empty phoneme IDs")
	}
	if input.NoiseScale == 0 {
		input.NoiseScale = 0.667
	}
	if input.LengthScale == 0 {
		input.LengthScale = 1.0
	}
	if input.NoiseScaleW == 0 {
		input.NoiseScaleW = 0.8 // ONNX default
	}
	T := len(input.PhonemeIDs)
	hidden := hp.HiddenChannels
	inter := hp.InterChannels

	// Step 1: Phoneme embedding
	// The embedding table is stored in the ONNX graph as a Gather operation.
	// In the GGUF, we stored the sid (speaker embedding) but the phoneme embedding
	// is part of the ONNX computation graph. We need to extract it.
	// For now, use a simple learned embedding from the encoder weights.
	// Actually, in Piper VITS, the embedding is implicit in the encoder's first layer.
	// The ONNX model takes raw phoneme IDs as input and does the embedding internally.
	// Since we don't have the embedding table in the GGUF, we need to handle this.

	// The Piper VITS text encoder structure:
	// 1. nn.Embedding(n_vocab, hidden_channels) — this IS in the ONNX but as a Gather op
	// 2. Encoder (attention layers) — these weights we have
	// 3. proj — we have this

	// The embedding weights are the first Gather operation in the ONNX graph.
	// They should be in the GGUF as enc_p.emb.weight or similar.
	// Let me check if we missed loading them...

	// For the xiao_ya model, the embedding is part of the ONNX graph constants.
	// We need to extract it from the ONNX model separately.
	// For now, let's use a random initialization and fix this in the conversion script.

	// Actually, looking at the ONNX more carefully, the embedding table IS there
	// but stored as an ONNX graph initializer with a generated name.
	// We need to identify it by shape: [n_vocab, hidden_channels] = [256, 192]
	// The 'sid' tensor is [256, 192] but that's the speaker embedding.
	// The phoneme embedding should be a separate [256, 192] tensor.

	// TODO: Fix the GGUF conversion to include the phoneme embedding.
	// For now, use the sid tensor shape to verify dimensions.

	// Step 1: Embedding lookup
	sqrtH := float32(math.Sqrt(float64(hidden)))
	x := make([]float32, hidden*T)

	if w.TextEnc.Emb != nil {
		// Use stored embedding table
		vocabSize := len(w.TextEnc.Emb) / hidden
		for t := 0; t < T; t++ {
			pid := int(input.PhonemeIDs[t])
			if pid >= 0 && pid < vocabSize {
				for h := 0; h < hidden; h++ {
					x[h*T+t] = w.TextEnc.Emb[pid*hidden+h] * sqrtH
				}
			}
		}
	} else {
		return nil, fmt.Errorf("piper: phoneme embedding not found in model weights")
	}

	// Step 2: Text Encoder (reuse MeloTTS encoder layers)
	for i := 0; i < hp.NLayers; i++ {
		x = encoderLayerForward(x, hidden, T, &w.TextEnc.Layers[i], hp.toMeloHP())
	}

	// Step 3: Project to stats: [hidden, T] → [inter*2, T]
	stats := Conv1D(x, hidden, T, w.TextEnc.Proj.Weight, w.TextEnc.Proj.KSize, inter*2, 1,
		(w.TextEnc.Proj.KSize-1)/2, w.TextEnc.Proj.Bias)

	mP := stats[:inter*T]
	logsP := stats[inter*T:]

	// Step 4: Duration Prediction
	var durations []int
	var tMel int
	if m.W.SDP.Pre.Weight != nil && len(m.W.SDP.Flows) > 0 {
		// Full SDP with neural spline flow
		durations, tMel = PiperSDPForward(x, hidden, T, &m.W.SDP, hp, input.NoiseScaleW, input.PhonemeIDs)
		// Apply length_scale
		if input.LengthScale != 0 && input.LengthScale != 1.0 {
			tMel = 0
			for i := range durations {
				d := int(math.Ceil(float64(durations[i]) * float64(input.LengthScale)))
				if d < 1 {
					d = 1
				}
				durations[i] = d
				tMel += d
			}
		}
	} else if m.DurCache != nil {
		// Best: trigram cache from ONNX reference data
		durations, tMel = PiperDurationFromCache(input.PhonemeIDs, m.DurCache)
	} else if m.DurMLPW1 != nil {
		durations, tMel = PiperDurationFromEncoderMLP(mP, inter, T,
			input.PhonemeIDs, m.DurMLPW1, m.DurMLPB1, m.DurMLPW2, m.DurMLPB2)
	} else {
		durations, tMel = PiperDurationFromPhonemes(input.PhonemeIDs)
	}
	if input.LengthScale != 0 && input.LengthScale != 1.0 {
		tMel = 0
		for i := range durations {
			d := int(math.Ceil(float64(durations[i]) * float64(input.LengthScale)))
			if d < 1 {
				d = 1
			}
			durations[i] = d
			tMel += d
		}
	}

	if tMel == 0 {
		return nil, fmt.Errorf("piper: zero mel length from durations")
	}

	// Shorten word-internal separator durations for connected English speech.
	// Keep the separator (model needs it for syllable boundaries) but shorten to ~40%.
	// Too short (1) causes swallowed syllables; too long sounds choppy.
	if len(input.WordInternalSeps) > 0 {
		for _, idx := range input.WordInternalSeps {
			if idx >= 0 && idx < len(durations) && durations[idx] > 2 {
				old := durations[idx]
				shortened := (old*2 + 4) / 5 // ~40%, minimum 2
				if shortened < 2 {
					shortened = 2
				}
				durations[idx] = shortened
				tMel -= old - shortened
			}
		}
	}

	// Step 5: Expand prior by durations
	path, _ := GeneratePath(durations)
	mPExpanded := ExpandByDurations(mP, inter, T, path, tMel)
	logsPExpanded := ExpandByDurations(logsP, inter, T, path, tMel)

	// Step 6: Sample latent
	zP := make([]float32, inter*tMel)
	if input.NoiseScale > 0 {
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
		copy(zP, mPExpanded)
	}

	// Step 7: Flow decoder (reverse) — ResidualCouplingBlock
	z := PiperFlowReverseForward(zP, inter, tMel, &w.Flow, hp)

	// Step 8: HiFi-GAN vocoder
	audio := PiperHiFiGANForward(z, inter, tMel, &w.Vocoder, hp)

	return audio, nil
}

// SynthesizeText is a convenience method that does G2P + synthesis.
func (m *PiperModel) SynthesizeText(text string) ([]float32, error) {
	g2p := PiperTextToPhonemesWithDict(text, m.Lex, m.CMUDict)
	if len(g2p.PhonemeIDs) == 0 {
		return nil, fmt.Errorf("piper: no phonemes from text: %q", text)
	}
	return m.Synthesize(PiperSynthesizeInput{
		PhonemeIDs:       g2p.PhonemeIDs,
		WordInternalSeps: g2p.WordInternalSeps,
	})
}

// SynthesizeToWAV does G2P + synthesis + WAV encoding.
func (m *PiperModel) SynthesizeToWAV(text string) ([]byte, error) {
	audio, err := m.SynthesizeText(text)
	if err != nil {
		return nil, err
	}
	return EncodeWAV(audio, m.HP.SampleRate), nil
}

// toMeloHP converts PiperHParams to MeloTTS HParams for reusing encoder code.
func (hp PiperHParams) toMeloHP() HParams {
	return HParams{
		HiddenChannels: hp.HiddenChannels,
		InterChannels:  hp.InterChannels,
		FilterChannels: hp.FilterChannels,
		NHeads:         hp.NHeads,
		NLayers:        hp.NLayers,
		KernelSize:     hp.KernelSize,
		SampleRate:     hp.SampleRate,
		HopLength:      hp.HopLength,
	}
}
