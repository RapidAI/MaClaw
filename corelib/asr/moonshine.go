// corelib/asr/moonshine.go — Pure Go Moonshine ASR model (GGUF, mmap-backed).
//
// Encoder-decoder transformer with RoPE, ported from RapidSpeech.cpp.
// Architecture: Audio → Conv frontend → Encoder (LayerNorm + RoPE + GELU FFN)
//
//	→ Decoder (LayerNorm + RoPE + SwiGLU FFN + cross-attn)
//	→ Token logits → text
//
// Memory optimization: the GGUF file is memory-mapped. Tensors stored as Q8_0
// in the GGUF are used via zero-copy (Q8Tensor.Data points into the mmap region).
// F32 tensors are read from the mmap region and either kept as float32 (small
// norm/bias/conv weights) or runtime-quantized to Q8_0 (large projection matrices).
// Call Close() to release the mmap when the model is no longer needed.
package asr

import (
	"encoding/binary"
	"fmt"
	"os"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/embedding/gguf"
	"github.com/RapidAI/CodeClaw/corelib/embedding/tensor"
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
	attnQW, attnKW, attnVW, attnOutW *tensor.Q8Tensor // [dim, dim] quantized
	attnNormW                        []float32        // [dim] — keep f32
	ffUpW                            *tensor.Q8Tensor // [ffDim, dim]
	ffUpB                            []float32        // [ffDim]
	ffDownW                          *tensor.Q8Tensor // [dim, ffDim]
	ffDownB                          []float32        // [dim]
	ffNormW                          []float32        // [dim]
}

type decoderLayer struct {
	selfQW, selfKW, selfVW, selfOutW     *tensor.Q8Tensor
	selfNormW                            []float32
	crossQW, crossKW, crossVW, crossOutW *tensor.Q8Tensor
	crossNormW                           []float32
	ffUpW                                *tensor.Q8Tensor // [2*intermediate, dim]
	ffUpB                                []float32
	ffDownW                              *tensor.Q8Tensor // [dim, intermediate]
	ffDownB                              []float32
	ffNormW                              []float32
}

type weights struct {
	conv1W, conv1B []float32
	conv2W, conv2B []float32
	conv3W, conv3B []float32
	gnormW, gnormB []float32
	encLayers      []encoderLayer
	encFinalNormW  []float32
	decLayers      []decoderLayer
	decFinalNormW  []float32
	tokenEmb       []float32        // [vocabSize, dim] — keep f32 for embedding lookup
	lmHeadW        *tensor.Q8Tensor // [vocabSize, dim] quantized; nil = weight tying
	lmHeadF32      []float32        // fallback when weight-tied (points to tokenEmb)
}

// MoonshineModel is a pure Go Moonshine ASR model.
// The GGUF file is memory-mapped; call Close() to release the mapping.
type MoonshineModel struct {
	hp    HParams
	w     weights
	vocab []string       // indexed by token ID (contiguous)
	mmap  *gguf.MmapFile // kept alive for mmap-backed Q8Tensor data
	mu    sync.Mutex
}

// NewMoonshine loads a Moonshine model from a GGUF file using memory mapping.
// The mmap is kept alive for the lifetime of the model (Q8Tensor.Data may
// point into the mmap region). Call Close() to release.
func NewMoonshine(modelPath string) (*MoonshineModel, error) {
	mf, err := gguf.OpenMmap(modelPath)
	if err != nil {
		return nil, fmt.Errorf("asr: open mmap gguf: %w", err)
	}

	hp := HParams{
		EncoderDim:   gguf.GetMetaI32(mf.Meta, "moonshine.encoder_dim", 288),
		EncoderDepth: gguf.GetMetaI32(mf.Meta, "moonshine.encoder_depth", 6),
		EncoderHeads: gguf.GetMetaI32(mf.Meta, "moonshine.encoder_heads", 8),
		DecoderDim:   gguf.GetMetaI32(mf.Meta, "moonshine.decoder_dim", 288),
		DecoderDepth: gguf.GetMetaI32(mf.Meta, "moonshine.decoder_depth", 6),
		DecoderHeads: gguf.GetMetaI32(mf.Meta, "moonshine.decoder_heads", 8),
		VocabSize:    gguf.GetMetaI32(mf.Meta, "moonshine.vocab_size", 32768),
		BOSID:        gguf.GetMetaI32(mf.Meta, "moonshine.bos_id", 1),
		EOSID:        gguf.GetMetaI32(mf.Meta, "moonshine.eos_id", 2),
		MaxSeqLen:    gguf.GetMetaI32(mf.Meta, "moonshine.max_seq_len", 448),
		SampleRate:   gguf.GetMetaI32(mf.Meta, "moonshine.sample_rate", 16000),
		RopeTheta:    gguf.GetMetaF32(mf.Meta, "moonshine.rope_theta", 10000.0),
		PartialRot:   gguf.GetMetaF32(mf.Meta, "moonshine.partial_rotary_factor", 0.9),
	}
	hp.EncoderHDim = hp.EncoderDim / hp.EncoderHeads
	hp.DecoderHDim = hp.DecoderDim / hp.DecoderHeads

	m := &MoonshineModel{hp: hp, mmap: mf}
	if err := m.loadWeights(mf); err != nil {
		mf.CloseMmap()
		return nil, err
	}
	m.loadVocab(mf)
	return m, nil
}

// Close releases the mmap and all resources.
func (m *MoonshineModel) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.mmap != nil {
		m.mmap.CloseMmap()
		m.mmap = nil
	}
}

func (m *MoonshineModel) loadWeights(mf *gguf.MmapFile) error {
	// getF32 reads a tensor as float32 from the mmap region.
	getF32 := func(name string) ([]float32, error) {
		d, err := mf.TensorF32("model." + name)
		if err != nil {
			return mf.TensorF32(name)
		}
		return d, nil
	}
	tryGetF32 := func(name string) []float32 {
		d, _ := getF32(name)
		return d
	}
	// getQ8 tries to load a weight matrix as Q8_0 zero-copy from mmap.
	// If the tensor is stored as Q8_0 in the GGUF, returns a Q8Tensor
	// pointing directly into the mmap region (zero allocation).
	// If stored as F32, reads and runtime-quantizes to Q8_0 (heap allocation).
	getQ8 := func(name string, rows, cols int) *tensor.Q8Tensor {
		// Try mmap zero-copy for Q8_0 tensors first.
		for _, prefix := range []string{"model.", ""} {
			raw, ti, err := mf.TensorRawBytes(prefix + name)
			if err != nil {
				continue
			}
			if ti.Type == gguf.TypeQ8_0 {
				return &tensor.Q8Tensor{Data: raw, Rows: rows, Cols: cols}
			}
		}
		// Fallback: read as F32 and runtime-quantize.
		d, err := getF32(name)
		if err != nil || len(d) == 0 {
			return nil
		}
		return tensor.QuantizeToQ8(d, rows, cols)
	}

	w := &m.w
	dim := m.hp.EncoderDim
	var err error
	w.conv1W, err = getF32("encoder.conv1.weight")
	if err != nil {
		return fmt.Errorf("asr: %w", err)
	}
	w.conv1B = tryGetF32("encoder.conv1.bias")
	w.conv2W, _ = getF32("encoder.conv2.weight")
	w.conv2B = tryGetF32("encoder.conv2.bias")
	w.conv3W, _ = getF32("encoder.conv3.weight")
	w.conv3B = tryGetF32("encoder.conv3.bias")
	w.gnormW = tryGetF32("encoder.groupnorm.weight")
	w.gnormB = tryGetF32("encoder.groupnorm.bias")

	w.encLayers = make([]encoderLayer, m.hp.EncoderDepth)
	for i := range w.encLayers {
		p := fmt.Sprintf("encoder.layers.%d.", i)
		l := &w.encLayers[i]
		l.attnQW = getQ8(p+"self_attn.q_proj.weight", dim, dim)
		l.attnKW = getQ8(p+"self_attn.k_proj.weight", dim, dim)
		l.attnVW = getQ8(p+"self_attn.v_proj.weight", dim, dim)
		l.attnOutW = getQ8(p+"self_attn.o_proj.weight", dim, dim)
		l.attnNormW, _ = getF32(p + "input_layernorm.weight")
		// Determine FFN dimensions from the weight size
		ffUpF32, _ := getF32(p + "mlp.fc1.weight")
		if len(ffUpF32) == 0 {
			continue // skip layer if weights missing
		}
		ffDim := len(ffUpF32) / dim
		l.ffUpW = tensor.QuantizeToQ8(ffUpF32, ffDim, dim)
		l.ffUpB = tryGetF32(p + "mlp.fc1.bias")
		ffDownF32, _ := getF32(p + "mlp.fc2.weight")
		if len(ffDownF32) > 0 {
			l.ffDownW = tensor.QuantizeToQ8(ffDownF32, dim, ffDim)
		}
		l.ffDownB = tryGetF32(p + "mlp.fc2.bias")
		l.ffNormW, _ = getF32(p + "post_attention_layernorm.weight")
	}
	w.encFinalNormW, _ = getF32("encoder.layer_norm.weight")

	ddim := m.hp.DecoderDim
	w.decLayers = make([]decoderLayer, m.hp.DecoderDepth)
	for i := range w.decLayers {
		p := fmt.Sprintf("decoder.layers.%d.", i)
		l := &w.decLayers[i]
		l.selfQW = getQ8(p+"self_attn.q_proj.weight", ddim, ddim)
		l.selfKW = getQ8(p+"self_attn.k_proj.weight", ddim, ddim)
		l.selfVW = getQ8(p+"self_attn.v_proj.weight", ddim, ddim)
		l.selfOutW = getQ8(p+"self_attn.o_proj.weight", ddim, ddim)
		l.selfNormW, _ = getF32(p + "input_layernorm.weight")
		l.crossQW = getQ8(p+"encoder_attn.q_proj.weight", ddim, ddim)
		l.crossKW = getQ8(p+"encoder_attn.k_proj.weight", ddim, ddim)
		l.crossVW = getQ8(p+"encoder_attn.v_proj.weight", ddim, ddim)
		l.crossOutW = getQ8(p+"encoder_attn.o_proj.weight", ddim, ddim)
		l.crossNormW, _ = getF32(p + "post_attention_layernorm.weight")
		ffUpF32, _ := getF32(p + "mlp.fc1.weight")
		if len(ffUpF32) == 0 {
			continue
		}
		ffDim2x := len(ffUpF32) / ddim
		l.ffUpW = tensor.QuantizeToQ8(ffUpF32, ffDim2x, ddim)
		l.ffUpB = tryGetF32(p + "mlp.fc1.bias")
		ffDownF32, _ := getF32(p + "mlp.fc2.weight")
		intermediate := ffDim2x / 2
		if len(ffDownF32) > 0 {
			l.ffDownW = tensor.QuantizeToQ8(ffDownF32, ddim, intermediate)
		}
		l.ffDownB = tryGetF32(p + "mlp.fc2.bias")
		l.ffNormW, _ = getF32(p + "final_layernorm.weight")
	}
	w.decFinalNormW, _ = getF32("decoder.layer_norm.weight")
	if w.decFinalNormW == nil {
		w.decFinalNormW = tryGetF32("decoder.norm.weight")
	}
	w.tokenEmb, err = getF32("decoder.embed_tokens.weight")
	if err != nil {
		return fmt.Errorf("asr: token embedding: %w", err)
	}
	lmHeadF32 := tryGetF32("lm_head.weight")
	if lmHeadF32 != nil {
		w.lmHeadW = tensor.QuantizeToQ8(lmHeadF32, m.hp.VocabSize, ddim)
	} else {
		w.lmHeadF32 = w.tokenEmb // weight tying — keep f32
	}
	return nil
}

func (m *MoonshineModel) loadVocab(mf *gguf.MmapFile) {
	tokens := gguf.GetMetaStrArr(mf.Meta, "tokenizer.ggml.tokens")
	if tokens == nil {
		tokens = gguf.GetMetaStrArr(mf.Meta, "tokenizer.tokens")
	}
	m.vocab = make([]string, len(tokens))
	copy(m.vocab, tokens)
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
	audioFormat, channels, sampleRate, bitsPerSample, err := parseFmt(data)
	if err != nil {
		return nil, err
	}
	if audioFormat != 1 {
		return nil, fmt.Errorf("asr: unsupported WAV format %d (want PCM)", audioFormat)
	}
	if channels != 1 && channels != 2 {
		return nil, fmt.Errorf("asr: unsupported WAV channels %d (want mono or stereo)", channels)
	}
	if sampleRate <= 0 {
		return nil, fmt.Errorf("asr: invalid WAV sample rate %d", sampleRate)
	}
	if bitsPerSample != 16 {
		return nil, fmt.Errorf("asr: unsupported WAV bit depth %d (want 16-bit PCM)", bitsPerSample)
	}

	pcmData, err := extractData(data)
	if err != nil {
		return nil, err
	}
	blockAlign := channels * (bitsPerSample / 8)
	if blockAlign <= 0 || len(pcmData)%blockAlign != 0 {
		return nil, fmt.Errorf("asr: malformed WAV data size %d for block align %d", len(pcmData), blockAlign)
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

func parseFmt(data []byte) (audioFormat, channels, sampleRate, bitsPerSample int, err error) {
	for i := 12; i+8 < len(data); {
		id := string(data[i : i+4])
		sz := int(binary.LittleEndian.Uint32(data[i+4 : i+8]))
		if id == "fmt " && sz >= 16 && i+8+16 <= len(data) {
			audioFormat = int(binary.LittleEndian.Uint16(data[i+8 : i+10]))
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
	return 0, 0, 0, 0, fmt.Errorf("asr: fmt chunk not found")
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
