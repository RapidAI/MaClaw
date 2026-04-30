package tts

import (
	"fmt"

	"github.com/RapidAI/CodeClaw/corelib/embedding/gguf"
)

// PiperHParams holds Piper VITS model hyperparameters.
type PiperHParams struct {
	HiddenChannels int // 192
	InterChannels  int // 192
	FilterChannels int // 768
	NHeads         int // 2
	NLayers        int // 6 (text encoder)
	KernelSize     int // 3
	SampleRate     int // 22050
	HopLength      int // 256
	GinChannels    int // 192 (speaker embedding dim, from sid shape)

	// Flow decoder (ResidualCouplingBlock)
	NFlowLayers   int // 4
	WNLayers      int // 4 (WaveNet layers per coupling)
	WNKernelSize  int // 5
	WNDilation    int // 1 (base dilation, doubles each layer)

	// HiFi-GAN vocoder
	UpsampleRates          []int // [8, 8, 4]
	UpsampleKernelSizes    []int // [16, 16, 8]
	UpsampleInitialChannel int   // 256
	ResblockKernelSizes    []int // [3, 5, 7]

	// Duration predictor
	DPFlowLayers int // 4 (stochastic DP flow layers at indices 0,3,5,7)
	DPDDSLayers  int // 3 (DDSConv layers)
}

// DefaultPiperHParams returns default hyperparameters for xiao_ya model.
func DefaultPiperHParams() PiperHParams {
	return PiperHParams{
		HiddenChannels: 192,
		InterChannels:  192,
		FilterChannels: 768,
		NHeads:         2,
		NLayers:        6,
		KernelSize:     3,
		SampleRate:     22050,
		HopLength:      256,
		GinChannels:    192,

		NFlowLayers:  4,
		WNLayers:     4,
		WNKernelSize: 5,
		WNDilation:   1,

		UpsampleRates:          []int{8, 8, 4},
		UpsampleKernelSizes:    []int{16, 16, 8},
		UpsampleInitialChannel: 256,
		ResblockKernelSizes:    []int{3, 5, 7},

		DPFlowLayers: 4,
		DPDDSLayers:  3,
	}
}

// ── Piper-specific weight structures ──

// DDSConvWeights holds weights for Dilated Depth-Separable Conv block.
type DDSConvWeights struct {
	ConvsSep []Conv1DWeight    // depthwise separable convs [nLayers]
	Convs1x1 []Conv1DWeight   // 1x1 pointwise convs [nLayers]
	Norms1   []LayerNormWeight // pre-depthwise norms [nLayers]
	Norms2   []LayerNormWeight // pre-pointwise norms [nLayers]
}

// SDPFlowLayerWeights holds weights for one StochasticDurationPredictor flow layer.
type SDPFlowLayerWeights struct {
	Pre  Conv1DWeight
	Proj Conv1DWeight
	Convs DDSConvWeights
}

// StochasticDPWeights holds all StochasticDurationPredictor weights.
type StochasticDPWeights struct {
	Pre   Conv1DWeight // dp.pre [192, 192, 1]
	Proj  Conv1DWeight // dp.proj [192, 192, 1]
	Convs DDSConvWeights // dp.convs (main DDSConv)
	Flows []SDPFlowLayerWeights // dp.flows.{3,5,7} (coupling layers)
	FlowM []float32 // dp.flows.0.m [2, 1] (log-scale for first flow)
}

// WaveNetLayerWeights holds weights for one WaveNet layer.
type WaveNetLayerWeights struct {
	InLayer     Conv1DWeight // dilated conv [2*hidden, hidden, kSize]
	ResSkipLayer Conv1DWeight // 1x1 conv [2*hidden or hidden, hidden, 1]
}

// ResidualCouplingLayerWeights holds weights for one ResidualCouplingBlock layer.
type ResidualCouplingLayerWeights struct {
	Pre  Conv1DWeight // [hidden, inter/2, 1]
	Post Conv1DWeight // [inter/2, hidden, 1]
	WN   []WaveNetLayerWeights // WaveNet layers
}

// PiperFlowWeights holds all flow decoder weights.
type PiperFlowWeights struct {
	Layers []ResidualCouplingLayerWeights
}

// PiperResBlock holds weights for a Piper HiFi-GAN ResBlock.
// Piper uses ResBlock2 (2 conv layers per block, not 6).
type PiperResBlock struct {
	Convs []Conv1DWeight // [2] convolutions
}

// PiperVocoderWeights holds Piper HiFi-GAN vocoder weights.
type PiperVocoderWeights struct {
	ConvPre  Conv1DWeight
	Ups      []ConvT1DWeight
	ResBlocks []PiperResBlock
	ConvPost Conv1DWeight
}

// PiperWeights holds all Piper VITS model weights.
type PiperWeights struct {
	SID     []float32 // speaker embedding [256, 192]
	TextEnc TextEncoderWeights // reuse MeloTTS text encoder (same architecture)
	SDP     StochasticDPWeights
	Flow    PiperFlowWeights
	Vocoder PiperVocoderWeights
}

// LoadPiperWeightsGGUF loads Piper VITS weights from a GGUF file.
func LoadPiperWeightsGGUF(path string, hp PiperHParams) (*PiperWeights, error) {
	mf, err := gguf.OpenMmap(path)
	if err != nil {
		return nil, fmt.Errorf("piper: open gguf: %w", err)
	}

	w := &PiperWeights{}

	getF32 := func(name string) []float32 {
		d, err := mf.TensorF32(name)
		if err != nil {
			return nil
		}
		return d
	}

	loadConv1D := func(prefix string) Conv1DWeight {
		wt := getF32(prefix + ".weight")
		b := getF32(prefix + ".bias")
		if wt == nil {
			return Conv1DWeight{}
		}
		ti := mf.Tensors[prefix+".weight"]
		if ti == nil {
			return Conv1DWeight{Weight: wt, Bias: b}
		}
		var outCh, inCh, kSize int
		switch ti.NDims {
		case 3:
			outCh = int(ti.Dims[0])
			inCh = int(ti.Dims[1])
			kSize = int(ti.Dims[2])
		case 2:
			outCh = int(ti.Dims[0])
			inCh = int(ti.Dims[1])
			kSize = 1
		case 1:
			outCh = int(ti.Dims[0])
			inCh = 1
			kSize = 1
		}
		return Conv1DWeight{Weight: wt, Bias: b, OutCh: outCh, InCh: inCh, KSize: kSize}
	}

	loadConvT1D := func(prefix string) ConvT1DWeight {
		wt := getF32(prefix + ".weight")
		b := getF32(prefix + ".bias")
		if wt == nil {
			return ConvT1DWeight{}
		}
		ti := mf.Tensors[prefix+".weight"]
		if ti == nil {
			return ConvT1DWeight{Weight: wt, Bias: b}
		}
		var inCh, outCh, kSize int
		if ti.NDims == 3 {
			inCh = int(ti.Dims[0])
			outCh = int(ti.Dims[1])
			kSize = int(ti.Dims[2])
		}
		return ConvT1DWeight{Weight: wt, Bias: b, InCh: inCh, OutCh: outCh, KSize: kSize}
	}

	loadLayerNorm := func(prefix string) LayerNormWeight {
		wt := getF32(prefix + ".weight")
		b := getF32(prefix + ".bias")
		if wt == nil {
			wt = getF32(prefix + ".gamma")
		}
		if b == nil {
			b = getF32(prefix + ".beta")
		}
		return LayerNormWeight{Weight: wt, Bias: b}
	}

	loadDDSConv := func(prefix string, nLayers int) DDSConvWeights {
		dds := DDSConvWeights{
			ConvsSep: make([]Conv1DWeight, nLayers),
			Convs1x1: make([]Conv1DWeight, nLayers),
			Norms1:   make([]LayerNormWeight, nLayers),
			Norms2:   make([]LayerNormWeight, nLayers),
		}
		for i := 0; i < nLayers; i++ {
			dds.ConvsSep[i] = loadConv1D(fmt.Sprintf("%s.convs_sep.%d", prefix, i))
			dds.Convs1x1[i] = loadConv1D(fmt.Sprintf("%s.convs_1x1.%d", prefix, i))
			dds.Norms1[i] = loadLayerNorm(fmt.Sprintf("%s.norms_1.%d", prefix, i))
			dds.Norms2[i] = loadLayerNorm(fmt.Sprintf("%s.norms_2.%d", prefix, i))
		}
		return dds
	}

	// ── Speaker / Phoneme embedding ──
	// In Piper xiao_ya, the 'sid' tensor [256, 192] is used as the phoneme embedding
	// (via enc_p.emb.Gather). Single-speaker model repurposes speaker ID as phoneme emb.
	w.SID = getF32("sid")
	// Use sid as the phoneme embedding table
	w.TextEnc.Emb = w.SID

	// ── Text Encoder (enc_p) ──
	// Piper uses the same encoder architecture as MeloTTS but without BERT/tone/lang embeddings.
	// The embedding is stored as part of the ONNX graph (not as a named weight).
	// We'll handle it in the forward pass.
	te := &w.TextEnc
	te.Layers = make([]EncoderLayer, hp.NLayers)
	for i := 0; i < hp.NLayers; i++ {
		p := fmt.Sprintf("enc_p.encoder.attn_layers.%d", i)
		te.Layers[i].Attn = EncoderAttnLayer{
			ConvQ:   loadConv1D(p + ".conv_q"),
			ConvK:   loadConv1D(p + ".conv_k"),
			ConvV:   loadConv1D(p + ".conv_v"),
			ConvO:   loadConv1D(p + ".conv_o"),
			EmbRelK: getF32(p + ".emb_rel_k"),
			EmbRelV: getF32(p + ".emb_rel_v"),
		}
		pn := fmt.Sprintf("enc_p.encoder.norm_layers_1.%d", i)
		te.Layers[i].Norm1 = loadLayerNorm(pn)

		pf := fmt.Sprintf("enc_p.encoder.ffn_layers.%d", i)
		te.Layers[i].FFN = EncoderFFNLayer{
			Conv1: loadConv1D(pf + ".conv_1"),
			Conv2: loadConv1D(pf + ".conv_2"),
		}
		pn2 := fmt.Sprintf("enc_p.encoder.norm_layers_2.%d", i)
		te.Layers[i].Norm2 = loadLayerNorm(pn2)
	}
	te.Proj = loadConv1D("enc_p.proj")

	// ── Stochastic Duration Predictor ──
	sdp := &w.SDP
	sdp.Pre = loadConv1D("dp.pre")
	sdp.Proj = loadConv1D("dp.proj")
	sdp.Convs = loadDDSConv("dp.convs", hp.DPDDSLayers)
	sdp.FlowM = getF32("dp.flows.0.m")

	// SDP flow layers at indices 3, 5, 7
	flowIndices := []int{3, 5, 7}
	sdp.Flows = make([]SDPFlowLayerWeights, len(flowIndices))
	for i, fi := range flowIndices {
		p := fmt.Sprintf("dp.flows.%d", fi)
		sdp.Flows[i] = SDPFlowLayerWeights{
			Pre:  loadConv1D(p + ".pre"),
			Proj: loadConv1D(p + ".proj"),
			Convs: loadDDSConv(p+".convs", hp.DPDDSLayers),
		}
	}

	// ── Flow Decoder (ResidualCouplingBlock) ──
	fl := &w.Flow
	fl.Layers = make([]ResidualCouplingLayerWeights, hp.NFlowLayers)
	flowLayerIndices := []int{0, 2, 4, 6}
	for i, fi := range flowLayerIndices {
		p := fmt.Sprintf("flow.flows.%d", fi)
		layer := &fl.Layers[i]
		layer.Pre = loadConv1D(p + ".pre")
		layer.Post = loadConv1D(p + ".post")

		// WaveNet layers
		layer.WN = make([]WaveNetLayerWeights, hp.WNLayers)
		for j := 0; j < hp.WNLayers; j++ {
			// Weight from renamed onnx::Conv_* tensors
			inW := loadConv1D(fmt.Sprintf("%s.enc.in_layers.%d", p, j))
			// Bias from named tensors
			inB := getF32(fmt.Sprintf("%s.enc.in_layers.%d.bias", p, j))
			if inW.Bias == nil && inB != nil {
				inW.Bias = inB
			}
			// If weight not found from renamed tensor, try loading directly
			if inW.Weight == nil {
				inW.Bias = inB
				inW.OutCh = hp.HiddenChannels * 2
				inW.InCh = hp.HiddenChannels
				inW.KSize = hp.WNKernelSize
			}
			layer.WN[j].InLayer = inW

			rsW := loadConv1D(fmt.Sprintf("%s.enc.res_skip_layers.%d", p, j))
			rsB := getF32(fmt.Sprintf("%s.enc.res_skip_layers.%d.bias", p, j))
			if rsW.Bias == nil && rsB != nil {
				rsW.Bias = rsB
			}
			if rsW.Weight == nil {
				rsW.Bias = rsB
				if j == hp.WNLayers-1 {
					rsW.OutCh = hp.HiddenChannels
				} else {
					rsW.OutCh = hp.HiddenChannels * 2
				}
				rsW.InCh = hp.HiddenChannels
				rsW.KSize = 1
			}
			layer.WN[j].ResSkipLayer = rsW
		}
	}

	// ── HiFi-GAN Vocoder (dec) ──
	voc := &w.Vocoder
	voc.ConvPre = loadConv1D("dec.conv_pre")
	voc.ConvPost = loadConv1D("dec.conv_post")

	nUps := len(hp.UpsampleRates)
	voc.Ups = make([]ConvT1DWeight, nUps)
	for i := 0; i < nUps; i++ {
		voc.Ups[i] = loadConvT1D(fmt.Sprintf("dec.ups.%d", i))
	}

	nResKernels := len(hp.ResblockKernelSizes)
	voc.ResBlocks = make([]PiperResBlock, nUps*nResKernels)
	for i := 0; i < nUps; i++ {
		for j := 0; j < nResKernels; j++ {
			idx := i*nResKernels + j
			rb := &voc.ResBlocks[idx]
			rb.Convs = make([]Conv1DWeight, 2) // Piper ResBlock2: 2 convs
			rb.Convs[0] = loadConv1D(fmt.Sprintf("dec.resblocks.%d.convs.0", idx))
			rb.Convs[1] = loadConv1D(fmt.Sprintf("dec.resblocks.%d.convs.1", idx))
		}
	}

	mf.CloseMmap()
	return w, nil
}
