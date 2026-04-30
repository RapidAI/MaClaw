package tts

import (
	"fmt"
	"math"

	"github.com/RapidAI/CodeClaw/corelib/embedding/gguf"
)

// HParams holds MeloTTS model hyperparameters.
type HParams struct {
	HiddenChannels  int // 192
	InterChannels   int // 192
	FilterChannels  int // 768
	NHeads          int // 2
	NLayers         int // 6 (text encoder)
	NFlowLayers     int // 4 (transformer coupling)
	NLayersTransFlow int // 3 (layers per flow FFT)
	KernelSize      int // 3
	VocabSize       int // ~220
	NumLanguages    int // 8
	NumTones        int // 16
	SampleRate      int // 22050
	HopLength       int // 256
	GinChannels     int // 256 (speaker embedding dim)

	// HiFi-GAN vocoder
	UpsampleRates          []int // [8, 8, 2, 2, 2]
	UpsampleKernelSizes    []int // [16, 16, 8, 2, 2]
	UpsampleInitialChannel int   // 512
	ResblockKernelSizes    []int // [3, 7, 11]
	ResblockDilationSizes  [][]int // [[1,3,5],[1,3,5],[1,3,5]]
}

// DefaultHParams returns the default MeloTTS hyperparameters (from config.json).
func DefaultHParams() HParams {
	return HParams{
		HiddenChannels:  192,
		InterChannels:   192,
		FilterChannels:  768,
		NHeads:          2,
		NLayers:         6,
		NFlowLayers:     4,
		NLayersTransFlow: 3,
		KernelSize:      3,
		VocabSize:       256,
		NumLanguages:    8,
		NumTones:        16,
		SampleRate:      22050,
		HopLength:       256,
		GinChannels:     256,
		UpsampleRates:          []int{8, 8, 2, 2, 2},
		UpsampleKernelSizes:    []int{16, 16, 8, 2, 2},
		UpsampleInitialChannel: 512,
		ResblockKernelSizes:    []int{3, 7, 11},
		ResblockDilationSizes:  [][]int{{1, 3, 5}, {1, 3, 5}, {1, 3, 5}},
	}
}

// ── Weight structures ──

// Conv1DWeight holds weights for a Conv1d layer.
type Conv1DWeight struct {
	Weight []float32 // [outCh, inCh, kSize]
	Bias   []float32 // [outCh] or nil
	OutCh  int
	InCh   int
	KSize  int
}

// ConvT1DWeight holds weights for a ConvTranspose1d layer.
type ConvT1DWeight struct {
	Weight []float32 // [inCh, outCh, kSize]
	Bias   []float32 // [outCh] or nil
	InCh   int
	OutCh  int
	KSize  int
}

// LinearWeight holds weights for a linear (fully connected) layer.
type LinearWeight struct {
	Weight []float32 // [outDim, inDim]
	Bias   []float32 // [outDim] or nil
	OutDim int
	InDim  int
}

// LayerNormWeight holds weights for LayerNorm.
type LayerNormWeight struct {
	Weight []float32 // [dim]
	Bias   []float32 // [dim]
}

// ── Text Encoder weights ──

type EncoderAttnLayer struct {
	ConvQ  Conv1DWeight // [hidden, hidden, 1]
	ConvK  Conv1DWeight // [hidden, hidden, 1]
	ConvV  Conv1DWeight // [hidden, hidden, 1]
	ConvO  Conv1DWeight // [hidden, hidden, 1]
	EmbRelK []float32   // [1, 2*window+1, headDim] relative position key
	EmbRelV []float32   // [1, 2*window+1, headDim] relative position value
}

type EncoderFFNLayer struct {
	Conv1 Conv1DWeight // [filter, hidden, kSize]
	Conv2 Conv1DWeight // [hidden, filter, kSize]
}

type EncoderLayer struct {
	Attn    EncoderAttnLayer
	FFN     EncoderFFNLayer
	Norm1   LayerNormWeight
	Norm2   LayerNormWeight
}

type TextEncoderWeights struct {
	Emb         []float32 // [vocabSize, hidden]
	ToneEmb     []float32 // [numTones, hidden]
	LangEmb     []float32 // [numLangs, hidden]
	BertProj    Conv1DWeight // [hidden, 1024, 1]
	JaBertProj  Conv1DWeight // [hidden, 768, 1]
	SpkEmbLinear Conv1DWeight // [hidden, ginChannels, 1] speaker conditioning
	Layers      []EncoderLayer
	Proj        Conv1DWeight // [inter*2, hidden, 1]
}

// ── Duration Predictor weights ──

type DurationPredictorWeights struct {
	Conv1 Conv1DWeight
	Norm1 LayerNormWeight
	Conv2 Conv1DWeight
	Norm2 LayerNormWeight
	Proj  Conv1DWeight // [1, filter, 1]
	Cond  Conv1DWeight // [hidden, gin, 1] speaker conditioning
}

// ── Flow Decoder weights ──

type FlowCouplingLayer struct {
	Pre       Conv1DWeight // [hidden, inter/2, 1]
	Enc       []EncoderLayer // FFT layers
	Post      Conv1DWeight // [inter/2, hidden, 1]
	SpkEmbLinear Conv1DWeight // [hidden, ginChannels, 1] speaker conditioning
}

type FlowWeights struct {
	Layers []FlowCouplingLayer
}

// ── HiFi-GAN Vocoder weights ──

type ResBlock struct {
	// 3 groups of dilated convolutions, each with 3 layers
	Convs1 []Conv1DWeight // len=3, dilated
	Convs2 []Conv1DWeight // len=3, non-dilated
}

type VocoderWeights struct {
	ConvPre  Conv1DWeight
	Ups      []ConvT1DWeight // len=5 (one per upsample rate)
	ResBlocks []ResBlock     // len=15 (3 per upsample level × 5 levels)
	ConvPost Conv1DWeight
	Cond     Conv1DWeight // speaker conditioning
}

// ── Full model weights ──

type Weights struct {
	SpeakerEmb []float32 // [nSpeakers, ginChannels]
	NSpeakers  int
	TextEnc    TextEncoderWeights
	DurPred    DurationPredictorWeights
	Flow       FlowWeights
	Vocoder    VocoderWeights
}

// ── GGUF loading ──

// reconstructWeightNorm reconstructs weight from weight_norm decomposition.
// weight = g * v / ||v|| where g is [outCh, 1, 1] and v is [outCh, inCh, kSize].
// The norm is computed per output channel (row).
func reconstructWeightNorm(g, v []float32) []float32 {
	if len(g) == 0 || len(v) == 0 {
		return nil
	}
	outCh := len(g)
	elemsPerRow := len(v) / outCh
	if elemsPerRow == 0 {
		return nil
	}
	w := make([]float32, len(v))
	for oc := 0; oc < outCh; oc++ {
		// Compute L2 norm of this row of v
		rowStart := oc * elemsPerRow
		rowEnd := rowStart + elemsPerRow
		var norm float32
		for _, val := range v[rowStart:rowEnd] {
			norm += val * val
		}
		norm = float32(math.Sqrt(float64(norm)))
		if norm == 0 {
			norm = 1e-12
		}
		scale := g[oc] / norm
		for i := rowStart; i < rowEnd; i++ {
			w[i] = v[i] * scale
		}
	}
	return w
}

// LoadWeightsGGUF loads MeloTTS weights from a GGUF file.
func LoadWeightsGGUF(path string, hp HParams) (*Weights, error) {
	mf, err := gguf.OpenMmap(path)
	if err != nil {
		return nil, fmt.Errorf("tts: open gguf: %w", err)
	}
	// Note: we keep mf open — weights reference mmap data.
	// Caller should keep the returned Weights alive as long as needed.
	// For fp32 weights, TensorF32 copies data so mf can be closed.

	w := &Weights{}

	// Helper to load a tensor, trying with and without prefix
	getF32 := func(name string) []float32 {
		d, err := mf.TensorF32(name)
		if err != nil {
			return nil
		}
		return d
	}

	loadConv1D := func(prefix string) Conv1DWeight {
		w := getF32(prefix + ".weight")
		b := getF32(prefix + ".bias")
		// Handle weight normalization: weight = g * v / ||v||
		if w == nil {
			wg := getF32(prefix + ".weight_g")
			wv := getF32(prefix + ".weight_v")
			if wg != nil && wv != nil {
				w = reconstructWeightNorm(wg, wv)
			}
		}
		if w == nil {
			return Conv1DWeight{}
		}
		// Infer dimensions from GGUF tensor info
		ti := mf.Tensors[prefix+".weight"]
		if ti == nil {
			ti = mf.Tensors[prefix+".weight_v"] // use weight_v dims
		}
		if ti == nil {
			return Conv1DWeight{Weight: w, Bias: b}
		}
		// convert_openvoice2.py stores PyTorch shape directly:
		// Conv1d weight: [outCh, inCh, kSize]
		var outCh, inCh, kSize int
		switch ti.NDims {
		case 3:
			outCh = int(ti.Dims[0])
			inCh = int(ti.Dims[1])
			kSize = int(ti.Dims[2])
		case 2:
			// Linear layer or embedding: [outDim, inDim]
			outCh = int(ti.Dims[0])
			inCh = int(ti.Dims[1])
			kSize = 1
		case 1:
			outCh = int(ti.Dims[0])
			inCh = 1
			kSize = 1
		}
		return Conv1DWeight{Weight: w, Bias: b, OutCh: outCh, InCh: inCh, KSize: kSize}
	}

	loadConvT1D := func(prefix string) ConvT1DWeight {
		w := getF32(prefix + ".weight")
		b := getF32(prefix + ".bias")
		// Handle weight normalization: weight = g * v / ||v||
		if w == nil {
			wg := getF32(prefix + ".weight_g")
			wv := getF32(prefix + ".weight_v")
			if wg != nil && wv != nil {
				w = reconstructWeightNorm(wg, wv)
			}
		}
		if w == nil {
			return ConvT1DWeight{}
		}
		ti := mf.Tensors[prefix+".weight"]
		if ti == nil {
			ti = mf.Tensors[prefix+".weight_v"] // use weight_v dims
		}
		if ti == nil {
			return ConvT1DWeight{Weight: w, Bias: b}
		}
		var inCh, outCh, kSize int
		if ti.NDims == 3 {
			// convert_openvoice2.py stores PyTorch shape directly:
			// ConvTranspose1d weight: [inCh, outCh, kSize]
			inCh = int(ti.Dims[0])
			outCh = int(ti.Dims[1])
			kSize = int(ti.Dims[2])
		}
		return ConvT1DWeight{Weight: w, Bias: b, InCh: inCh, OutCh: outCh, KSize: kSize}
	}

	loadLayerNorm := func(prefix string) LayerNormWeight {
		// GGUF uses gamma/beta (MeloTTS convention) instead of weight/bias
		w := getF32(prefix + ".weight")
		b := getF32(prefix + ".bias")
		if w == nil {
			w = getF32(prefix + ".gamma")
		}
		if b == nil {
			b = getF32(prefix + ".beta")
		}
		return LayerNormWeight{Weight: w, Bias: b}
	}

	// ── Speaker embedding ──
	w.SpeakerEmb = getF32("emb_g.weight")
	if w.SpeakerEmb != nil {
		w.NSpeakers = len(w.SpeakerEmb) / hp.GinChannels
	}

	// ── Text Encoder (enc_p → text_encoder) ──
	te := &w.TextEnc
	te.Emb = getF32("text_encoder.emb.weight")
	te.ToneEmb = getF32("text_encoder.tone_emb.weight")
	if te.ToneEmb == nil {
		te.ToneEmb = getF32("emb_tone.weight")
	}
	te.LangEmb = getF32("text_encoder.language_emb.weight")
	te.BertProj = loadConv1D("text_encoder.bert_proj")
	te.JaBertProj = loadConv1D("text_encoder.ja_bert_proj")
	te.SpkEmbLinear = loadConv1D("text_encoder.encoder.spk_emb_linear")

	te.Layers = make([]EncoderLayer, hp.NLayers)
	for i := 0; i < hp.NLayers; i++ {
		p := fmt.Sprintf("text_encoder.encoder.attn_layers.%d", i)
		te.Layers[i].Attn = EncoderAttnLayer{
			ConvQ:   loadConv1D(p + ".conv_q"),
			ConvK:   loadConv1D(p + ".conv_k"),
			ConvV:   loadConv1D(p + ".conv_v"),
			ConvO:   loadConv1D(p + ".conv_o"),
			EmbRelK: getF32(p + ".emb_rel_k"),
			EmbRelV: getF32(p + ".emb_rel_v"),
		}
		// Norm layers use norm_layers_1.{i} (post-attn) and norm_layers_2.{i} (post-FFN)
		pn := fmt.Sprintf("text_encoder.encoder.norm_layers_1.%d", i)
		te.Layers[i].Norm1 = loadLayerNorm(pn)

		pf := fmt.Sprintf("text_encoder.encoder.ffn_layers.%d", i)
		te.Layers[i].FFN = EncoderFFNLayer{
			Conv1: loadConv1D(pf + ".conv_1"),
			Conv2: loadConv1D(pf + ".conv_2"),
		}
		pn2 := fmt.Sprintf("text_encoder.encoder.norm_layers_2.%d", i)
		te.Layers[i].Norm2 = loadLayerNorm(pn2)
	}
	te.Proj = loadConv1D("text_encoder.proj")

	// ── Duration Predictor (dp → duration_predictor) ──
	dp := &w.DurPred
	dp.Conv1 = loadConv1D("duration_predictor.conv_1")
	dp.Norm1 = loadLayerNorm("duration_predictor.norm_1")
	dp.Conv2 = loadConv1D("duration_predictor.conv_2")
	dp.Norm2 = loadLayerNorm("duration_predictor.norm_2")
	dp.Proj = loadConv1D("duration_predictor.proj")
	dp.Cond = loadConv1D("duration_predictor.cond")

	// ── Flow Decoder (flow → flow_decoder) ──
	fl := &w.Flow
	fl.Layers = make([]FlowCouplingLayer, hp.NFlowLayers)
	for i := 0; i < hp.NFlowLayers; i++ {
		// TransformerCouplingLayer index: flows[i*2] (skip Flip at i*2+1)
		fi := i * 2
		p := fmt.Sprintf("flow_decoder.flows.%d", fi)
		layer := &fl.Layers[i]
		layer.Pre = loadConv1D(p + ".pre")
		layer.Post = loadConv1D(p + ".post")
		layer.SpkEmbLinear = loadConv1D(p + ".enc.spk_emb_linear")

		// FFT encoder layers within each coupling layer
		nFFT := hp.NLayersTransFlow
		layer.Enc = make([]EncoderLayer, nFFT)
		for j := 0; j < nFFT; j++ {
			pa := fmt.Sprintf("%s.enc.attn_layers.%d", p, j)
			layer.Enc[j].Attn = EncoderAttnLayer{
				ConvQ:   loadConv1D(pa + ".conv_q"),
				ConvK:   loadConv1D(pa + ".conv_k"),
				ConvV:   loadConv1D(pa + ".conv_v"),
				ConvO:   loadConv1D(pa + ".conv_o"),
				EmbRelK: getF32(pa + ".emb_rel_k"),
				EmbRelV: getF32(pa + ".emb_rel_v"),
			}
			pn := fmt.Sprintf("%s.enc.norm_layers_1.%d", p, j)
			layer.Enc[j].Norm1 = loadLayerNorm(pn)

			pf := fmt.Sprintf("%s.enc.ffn_layers.%d", p, j)
			layer.Enc[j].FFN = EncoderFFNLayer{
				Conv1: loadConv1D(pf + ".conv_1"),
				Conv2: loadConv1D(pf + ".conv_2"),
			}
			pn2 := fmt.Sprintf("%s.enc.norm_layers_2.%d", p, j)
			layer.Enc[j].Norm2 = loadLayerNorm(pn2)
		}
	}

	// ── HiFi-GAN Vocoder (dec → vocoder) ──
	voc := &w.Vocoder
	voc.ConvPre = loadConv1D("vocoder.conv_pre")
	voc.Cond = loadConv1D("vocoder.cond")

	nUps := len(hp.UpsampleRates)
	voc.Ups = make([]ConvT1DWeight, nUps)
	for i := 0; i < nUps; i++ {
		voc.Ups[i] = loadConvT1D(fmt.Sprintf("vocoder.ups.%d", i))
	}

	nResKernels := len(hp.ResblockKernelSizes)
	voc.ResBlocks = make([]ResBlock, nUps*nResKernels)
	for i := 0; i < nUps; i++ {
		for j := 0; j < nResKernels; j++ {
			idx := i*nResKernels + j
			rb := &voc.ResBlocks[idx]
			rb.Convs1 = make([]Conv1DWeight, 3)
			rb.Convs2 = make([]Conv1DWeight, 3)
			for k := 0; k < 3; k++ {
				rb.Convs1[k] = loadConv1D(fmt.Sprintf("vocoder.resblocks.%d.convs1.%d", idx, k))
				rb.Convs2[k] = loadConv1D(fmt.Sprintf("vocoder.resblocks.%d.convs2.%d", idx, k))
			}
		}
	}
	voc.ConvPost = loadConv1D("vocoder.conv_post")

	mf.CloseMmap()
	return w, nil
}
