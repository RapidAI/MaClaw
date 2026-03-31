// corelib/asr/moonshine.go — Pure Go Moonshine ASR model (GGUF).
//
// Encoder-decoder transformer with RoPE, ported from RapidSpeech.cpp.
// Architecture: Audio → Conv frontend → Encoder (LayerNorm + RoPE + GELU FFN)
//            → Decoder (LayerNorm + RoPE + SwiGLU FFN + cross-attn)
//            → Token logits → text
package asr

import (
	"encoding/binary"
	"fmt"
	"os"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/embedding/gguf"
)

// HParams holds Moonshine model hyperparameters from GGUF metadata.
type HParams struct {
	EncoderDim   int
	EncoderDepth int
	EncoderHeads int
	EncoderHDim  int // EncoderDim / EncoderHeads
	DecoderDim   int
	DecoderDepth int
	DecoderHeads int
	DecoderHDim  int
	VocabSize    int
	BOSID        int
	EOSID        int
	MaxSeqLen    int
	SampleRate   int
	RopeTheta    float32
	PartialRot   float32 // partial_rotary_factor
}

type encoderLayer struct {
	attnQW, attnKW, attnVW, attnOutW []float32 // [dim, dim]
	attnNormW                         []float32 // [dim]
	ffUpW                             []float32 // [dim, ffDim]
	ffUpB                             []float32 // [ffDim]
	ffDownW                           []float32 // [ffDim, dim]
	ffDownB                           []float32 // [dim]
	ffNormW                           []float32 // [dim]
}

type decoderLayer struct {
	selfQW, selfKW, selfVW, selfOutW []float32
	selfNormW                         []float32
	crossQW, crossKW, crossVW, crossOutW []float32
	crossNormW                            []float32
	ffUpW  []float32 // [dim, 2*intermediate]
	ffUpB  []float32
	ffDownW []float32 // [intermediate, dim]
	ffDownB []float32
	ffNormW []float32
}

type weights struct {
	conv1W, conv1B       []float32
	conv2W, conv2B       []float32
	conv3W, conv3B       []float32
	gnormW, gnormB       []float32
	encLayers            []encoderLayer
	encFinalNormW        []float32
	decLayers            []decoderLayer
	decFinalNormW        []float32
	tokenEmb             []float32 // [vocabSize, dim]
	lmHeadW              []float32 // may be nil (weight tying)
}

// MoonshineModel is a pure Go Moonshine ASR model.
type MoonshineModel struct {
	hp    HParams
	w     weights
	vocab map[int]string
	mu    sync.Mutex
}

// NewMoonshine loads a Moonshine model from a GGUF file.
func NewMoonshine(modelPath string) (*MoonshineModel, error) {
	gf, err := gguf.Open(modelPath)
	if err != nil {
		return nil, fmt.Errorf("asr: open gguf: %w", err)
	}
	defer gf.Close()

	hp := HParams{
		EncoderDim:   gguf.GetMetaI32(gf.Meta, "moonshine.encoder_dim", 288),
		EncoderDepth: gguf.GetMetaI32(gf.Meta, "moonshine.encoder_depth", 6),
		EncoderHeads: gguf.GetMetaI32(gf.Meta, "moonshine.encoder_heads", 8),
		DecoderDim:   gguf.GetMetaI32(gf.Meta, "moonshine.decoder_dim", 288),
		DecoderDepth: gguf.GetMetaI32(gf.Meta, "moonshine.decoder_depth", 6),
		DecoderHeads: gguf.GetMetaI32(gf.Meta, "moonshine.decoder_heads", 8),
		VocabSize:    gguf.GetMetaI32(gf.Meta, "moonshine.vocab_size", 32768),
		BOSID:        gguf.GetMetaI32(gf.Meta, "moonshine.bos_id", 1),
		EOSID:        gguf.GetMetaI32(gf.Meta, "moonshine.eos_id", 2),
		MaxSeqLen:    gguf.GetMetaI32(gf.Meta, "moonshine.max_seq_len", 448),
		SampleRate:   gguf.GetMetaI32(gf.Meta, "moonshine.sample_rate", 16000),
		RopeTheta:    gguf.GetMetaF32(gf.Meta, "moonshine.rope_theta", 10000.0),
		PartialRot:   gguf.GetMetaF32(gf.Meta, "moonshine.partial_rotary_factor", 0.9),
	}
	hp.EncoderHDim = hp.EncoderDim / hp.EncoderHeads
	hp.DecoderHDim = hp.DecoderDim / hp.DecoderHeads

	m := &MoonshineModel{hp: hp}
	if err := m.loadWeights(gf); err != nil {
		return nil, err
	}
	m.loadVocab(gf)
	return m, nil
}

func (m *MoonshineModel) loadWeights(gf *gguf.File) error {
	get := func(name string) ([]float32, error) {
		d, err := gf.ReadTensorF32("model." + name)
		if err != nil {
			return gf.ReadTensorF32(name)
		}
		return d, nil
	}
	tryGet := func(name string) []float32 {
		d, _ := get(name)
		return d
	}

	w := &m.w
	var err error
	w.conv1W, err = get("encoder.conv1.weight")
	if err != nil {
		return fmt.Errorf("asr: %w", err)
	}
	w.conv1B = tryGet("encoder.conv1.bias")
	w.conv2W, _ = get("encoder.conv2.weight")
	w.conv2B = tryGet("encoder.conv2.bias")
	w.conv3W, _ = get("encoder.conv3.weight")
	w.conv3B = tryGet("encoder.conv3.bias")
	w.gnormW = tryGet("encoder.groupnorm.weight")
	w.gnormB = tryGet("encoder.groupnorm.bias")

	w.encLayers = make([]encoderLayer, m.hp.EncoderDepth)
	for i := range w.encLayers {
		p := fmt.Sprintf("encoder.layers.%d.", i)
		l := &w.encLayers[i]
		l.attnQW, _ = get(p + "self_attn.q_proj.weight")
		l.attnKW, _ = get(p + "self_attn.k_proj.weight")
		l.attnVW, _ = get(p + "self_attn.v_proj.weight")
		l.attnOutW, _ = get(p + "self_attn.o_proj.weight")
		l.attnNormW, _ = get(p + "input_layernorm.weight")
		l.ffUpW, _ = get(p + "mlp.fc1.weight")
		l.ffUpB = tryGet(p + "mlp.fc1.bias")
		l.ffDownW, _ = get(p + "mlp.fc2.weight")
		l.ffDownB = tryGet(p + "mlp.fc2.bias")
		l.ffNormW, _ = get(p + "post_attention_layernorm.weight")
	}
	w.encFinalNormW, _ = get("encoder.layer_norm.weight")

	w.decLayers = make([]decoderLayer, m.hp.DecoderDepth)
	for i := range w.decLayers {
		p := fmt.Sprintf("decoder.layers.%d.", i)
		l := &w.decLayers[i]
		l.selfQW, _ = get(p + "self_attn.q_proj.weight")
		l.selfKW, _ = get(p + "self_attn.k_proj.weight")
		l.selfVW, _ = get(p + "self_attn.v_proj.weight")
		l.selfOutW, _ = get(p + "self_attn.o_proj.weight")
		l.selfNormW, _ = get(p + "input_layernorm.weight")
		l.crossQW, _ = get(p + "encoder_attn.q_proj.weight")
		l.crossKW, _ = get(p + "encoder_attn.k_proj.weight")
		l.crossVW, _ = get(p + "encoder_attn.v_proj.weight")
		l.crossOutW, _ = get(p + "encoder_attn.o_proj.weight")
		l.crossNormW, _ = get(p + "post_attention_layernorm.weight")
		l.ffUpW, _ = get(p + "mlp.fc1.weight")
		l.ffUpB = tryGet(p + "mlp.fc1.bias")
		l.ffDownW, _ = get(p + "mlp.fc2.weight")
		l.ffDownB = tryGet(p + "mlp.fc2.bias")
		l.ffNormW, _ = get(p + "final_layernorm.weight")
	}
	w.decFinalNormW, _ = get("decoder.layer_norm.weight")
	if w.decFinalNormW == nil {
		w.decFinalNormW = tryGet("decoder.norm.weight")
	}
	w.tokenEmb, err = get("decoder.embed_tokens.weight")
	if err != nil {
		return fmt.Errorf("asr: token embedding: %w", err)
	}
	w.lmHeadW = tryGet("lm_head.weight")
	return nil
}

func (m *MoonshineModel) loadVocab(gf *gguf.File) {
	tokens := gguf.GetMetaStrArr(gf.Meta, "tokenizer.ggml.tokens")
	if tokens == nil {
		tokens = gguf.GetMetaStrArr(gf.Meta, "tokenizer.tokens")
	}
	m.vocab = make(map[int]string, len(tokens))
	for i, t := range tokens {
		m.vocab[i] = t
	}
}

// Transcribe takes 16kHz mono float32 PCM (normalized to [-1,1]) and returns text.
func (m *MoonshineModel) Transcribe(pcm []float32) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(pcm) == 0 {
		return "", nil
	}

	// Encode
	encOut, encFrames, err := m.encode(pcm)
	if err != nil {
		return "", fmt.Errorf("asr encode: %w", err)
	}

	// Decode
	tokens, err := m.decode(encOut, encFrames)
	if err != nil {
		return "", fmt.Errorf("asr decode: %w", err)
	}

	return m.detokenize(tokens), nil
}

// LoadWAV reads a WAV file and returns 16kHz mono float32 PCM normalized to [-1,1].
// Handles resampling and stereo→mono conversion.
func LoadWAV(path string) ([]float32, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return WAVToFloat32(data)
}

// WAVToFloat32 converts WAV bytes to 16kHz mono float32 PCM normalized to [-1,1].
func WAVToFloat32(data []byte) ([]float32, error) {
	if len(data) < 44 {
		return nil, fmt.Errorf("asr: WAV too short")
	}
	if string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return nil, fmt.Errorf("asr: not a valid WAV")
	}

	// Parse fmt chunk
	channels, sampleRate, bitsPerSample, err := parseFmt(data)
	if err != nil {
		return nil, err
	}

	pcmData, err := extractData(data)
	if err != nil {
		return nil, err
	}

	// Stereo to mono
	if channels == 2 && bitsPerSample == 16 {
		mono := make([]byte, len(pcmData)/2)
		nSamples := len(pcmData) / 4
		for i := 0; i < nSamples; i++ {
			l := int(int16(binary.LittleEndian.Uint16(pcmData[i*4:])))
			r := int(int16(binary.LittleEndian.Uint16(pcmData[i*4+2:])))
			binary.LittleEndian.PutUint16(mono[i*2:], uint16(int16((l+r)/2)))
		}
		pcmData = mono
	}

	// Convert S16LE to float32
	nSamples := len(pcmData) / 2
	samples := make([]float32, nSamples)
	for i := 0; i < nSamples; i++ {
		s := int16(binary.LittleEndian.Uint16(pcmData[i*2:]))
		samples[i] = float32(s) / 32768.0
	}

	// Resample to 16kHz if needed
	if sampleRate != 16000 {
		samples = resampleFloat32(samples, sampleRate, 16000)
	}

	return samples, nil
}

func parseFmt(data []byte) (channels, sampleRate, bitsPerSample int, err error) {
	for i := 12; i+8 < len(data); {
		id := string(data[i : i+4])
		sz := int(binary.LittleEndian.Uint32(data[i+4 : i+8]))
		if id == "fmt " && sz >= 16 && i+8+16 <= len(data) {
			channels = int(binary.LittleEndian.Uint16(data[i+10 : i+12]))
			sampleRate = int(binary.LittleEndian.Uint32(data[i+12 : i+16]))
			bitsPerSample = int(binary.LittleEndian.Uint16(data[i+22 : i+24]))
			return
		}
		i += 8 + sz
		if sz%2 != 0 {
			i++
		}
	}
	return 0, 0, 0, fmt.Errorf("asr: fmt chunk not found")
}

func extractData(data []byte) ([]byte, error) {
	for i := 12; i+8 < len(data); {
		id := string(data[i : i+4])
		sz := int(binary.LittleEndian.Uint32(data[i+4 : i+8]))
		if id == "data" {
			start := i + 8
			end := start + sz
			if end > len(data) {
				end = len(data)
			}
			return data[start:end], nil
		}
		i += 8 + sz
		if sz%2 != 0 {
			i++
		}
	}
	return nil, fmt.Errorf("asr: data chunk not found")
}

// resampleFloat32 resamples float32 PCM using linear interpolation.
func resampleFloat32(in []float32, srcRate, dstRate int) []float32 {
	if srcRate == dstRate {
		return in
	}
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



