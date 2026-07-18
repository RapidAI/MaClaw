// corelib/vad/silero.go — Pure Go Silero VAD v5 inference with embedded weights.
//
// Architecture (16kHz model, matches the reference v5 forward pass):
//   PCM [512 samples] + 64-sample rolling context
//   → ReflectionPad1d(128) → STFT Conv1D [258, 1, 256] stride=128
//   → drop first (padding-dominated) frame → magnitude [129, 4]
//   → Encoder Conv1D ×4 (kernel=3, padding=1, ReLU; strides 1,2,2,1)
//   → LSTM (hidden=128, 1 layer), one step per window
//   → ReLU → Linear [128→1] + Sigmoid → speech probability [0,1]
//
// Weights are embedded as a compressed binary blob (~1MB) via go:embed.
// No external model file needed.
//
// Thread safety: Model is immutable after Load(). Detect is safe for concurrent
// use with different *State instances. State is NOT safe for concurrent use —
// each goroutine must use its own State (or serialize access).
package vad

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"sync"

	_ "embed"
)

//go:embed silero_weights.bin
var compressedWeights []byte

// HParams holds Silero VAD hyperparameters.
type HParams struct {
	SampleRate   int
	WindowSize   int     // 512 samples = 32ms at 16kHz
	StftSize     int     // 256 (FFT size)
	StftStride   int     // 128 (hop size)
	FreqBins     int     // 129 (StftSize/2 + 1)
	HiddenSize   int     // LSTM hidden size = 128
	Threshold    float32 // speech probability threshold
	MinSilenceMs int
	MinSpeechMs  int
}

// DefaultHParams returns the default Silero VAD v5 hyperparameters.
func DefaultHParams() HParams {
	return HParams{
		SampleRate:   16000,
		WindowSize:   512,
		StftSize:     256,
		StftStride:   128,
		FreqBins:     129,
		HiddenSize:   128,
		Threshold:    0.5,
		MinSilenceMs: 100,
		MinSpeechMs:  250,
	}
}

// State holds the runtime state for streaming VAD inference.
// NOT safe for concurrent use — each goroutine needs its own State.
type State struct {
	H          []float32 // LSTM hidden [HiddenSize]
	C          []float32 // LSTM cell [HiddenSize]
	Context    []float32 // trailing StftSize/4 samples of the previous window
	SpeechProb float32
	// State machine
	IsSpeaking     bool
	SilenceSamples int
	SpeechSamples  int

	// scratch buffers — pre-allocated to avoid per-call heap allocations.
	// Lazily initialized on first Detect call.
	scratch *scratchBuffers
}

// scratchBuffers holds pre-allocated working memory for Detect.
// Sized for the fixed model architecture: 64-sample context + 512-sample window,
// reflection-padded to 832 → 5 STFT frames → 4 encoder frames after dropping
// the first (padding-dominated) frame.
type scratchBuffers struct {
	stftIn     []float32    // [context+window] = 576
	stftPadded []float32    // [576 + 2*128] = 832
	stftOut    []float32    // [258 * 5]
	mag        []float32    // [129 * 4]
	conv       [4][]float32 // encoder conv outputs [128*4, 64*2, 64*1, 128*1]
	xt         []float32    // [128] LSTM input per time step
	gates      []float32    // [4 * hidden]
	hTmp       []float32    // [hidden]
	cTmp       []float32    // [hidden]
	outH       []float32    // [hidden] ReLU(h) for the output branch
	inCols     [][]float32  // [T][maxInCh] — gather buffers for SIMD conv
}

// convLayer holds weights for a single Conv1D + bias layer.
type convLayer struct {
	W              []float32 // [outCh, inCh, kSize] — original layout
	B              []float32 // [outCh]
	OutCh, InCh, K int
	// WT is the transposed weight layout [outCh, kSize, inCh] for SIMD-friendly
	// dot products over the inCh dimension. Pre-computed at load time.
	WT []float32
}

type weights struct {
	stft    convLayer    // STFT basis [258, 1, 256]
	encoder [4]convLayer // 4 encoder conv blocks
	lstmWIH []float32   // [4*hidden, inputSize]
	lstmWHH []float32   // [4*hidden, hidden]
	lstmBIH []float32   // [4*hidden]
	lstmBHH []float32   // [4*hidden]
	outW    []float32   // [128] (squeezed from [1, 128, 1])
	outB    float32
}

// Model is a pure Go Silero VAD model with embedded weights.
// Immutable after construction — safe for concurrent use.
type Model struct {
	hp HParams
	w  weights
}

var (
	globalModel     *Model
	globalModelOnce sync.Once
	globalModelErr  error
)

// Load returns the singleton Silero VAD model, loading from embedded weights
// on first call. Thread-safe.
func Load() (*Model, error) {
	globalModelOnce.Do(func() {
		m := &Model{hp: DefaultHParams()}
		if err := m.loadEmbeddedWeights(); err != nil {
			globalModelErr = fmt.Errorf("vad: load weights: %w", err)
			return
		}
		globalModel = m
	})
	return globalModel, globalModelErr
}

func (m *Model) loadEmbeddedWeights() error {
	r, err := zlib.NewReader(bytes.NewReader(compressedWeights))
	if err != nil {
		return fmt.Errorf("decompress: %w", err)
	}
	defer r.Close()
	data, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("read: %w", err)
	}

	tensors := make(map[string][]float32)
	off := 0
	for off < len(data) {
		if off+4 > len(data) {
			break
		}
		nameLen := int(binary.LittleEndian.Uint32(data[off:]))
		off += 4
		if off+nameLen > len(data) {
			break
		}
		name := string(data[off : off+nameLen])
		off += nameLen
		if off+4 > len(data) {
			break
		}
		nFloats := int(binary.LittleEndian.Uint32(data[off:]))
		off += 4
		nBytes := nFloats * 4
		if off+nBytes > len(data) {
			break
		}
		floats := make([]float32, nFloats)
		for i := 0; i < nFloats; i++ {
			floats[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[off+i*4:]))
		}
		off += nBytes
		tensors[name] = floats
	}

	w := &m.w

	if d, ok := tensors["stft.forward_basis_buffer"]; ok {
		w.stft = convLayer{W: d, OutCh: 258, InCh: 1, K: 256}
	} else {
		return fmt.Errorf("missing stft.forward_basis_buffer")
	}

	encShapes := [][3]int{
		{128, 129, 3},
		{64, 128, 3},
		{64, 64, 3},
		{128, 64, 3},
	}
	for i, shape := range encShapes {
		wKey := fmt.Sprintf("encoder.%d.reparam_conv.weight", i)
		bKey := fmt.Sprintf("encoder.%d.reparam_conv.bias", i)
		d, ok := tensors[wKey]
		if !ok {
			return fmt.Errorf("missing %s", wKey)
		}
		w.encoder[i] = convLayer{W: d, B: tensors[bKey], OutCh: shape[0], InCh: shape[1], K: shape[2]}
		// Pre-transpose weights from [outCh, inCh, K] to [outCh, K, inCh]
		// so that for fixed (oc, k), the inCh dimension is contiguous for SIMD dot products.
		w.encoder[i].WT = transposeConvWeights(d, shape[0], shape[1], shape[2])
	}

	var ok bool
	w.lstmWIH, ok = tensors["decoder.rnn.weight_ih"]
	if !ok {
		return fmt.Errorf("missing decoder.rnn.weight_ih")
	}
	w.lstmWHH, ok = tensors["decoder.rnn.weight_hh"]
	if !ok {
		return fmt.Errorf("missing decoder.rnn.weight_hh")
	}
	w.lstmBIH = tensors["decoder.rnn.bias_ih"]
	w.lstmBHH = tensors["decoder.rnn.bias_hh"]

	outW, ok := tensors["decoder.decoder.2.weight"]
	if !ok {
		return fmt.Errorf("missing decoder.decoder.2.weight")
	}
	w.outW = outW
	if outB, ok := tensors["decoder.decoder.2.bias"]; ok && len(outB) > 0 {
		w.outB = outB[0]
	}

	return nil
}

// NewState creates a fresh VAD state for streaming inference.
func (m *Model) NewState() *State {
	return &State{
		H:       make([]float32, m.hp.HiddenSize),
		C:       make([]float32, m.hp.HiddenSize),
		Context: make([]float32, m.hp.StftSize/4),
	}
}

// ensureScratch lazily allocates scratch buffers sized for this model.
func (s *State) ensureScratch(hp *HParams) *scratchBuffers {
	if s.scratch != nil {
		return s.scratch
	}
	ctxSize := hp.StftSize / 4
	pad := hp.StftSize / 2
	inLen := hp.WindowSize + ctxSize                        // 576
	stftT := (inLen+2*pad-hp.StftSize)/hp.StftStride + 1    // 5
	encT := stftT - 1                                       // first STFT frame dropped
	hidden := hp.HiddenSize
	sc := &scratchBuffers{
		stftIn:     make([]float32, inLen),
		stftPadded: make([]float32, inLen+2*pad),
		stftOut:    make([]float32, 258*stftT),
		mag:        make([]float32, hp.FreqBins*encT),
		xt:         make([]float32, hidden),
		gates:      make([]float32, 4*hidden),
		hTmp:       make([]float32, hidden),
		cTmp:       make([]float32, hidden),
		outH:       make([]float32, hidden),
	}
	// Encoder conv outputs follow strides 1,2,2,1: 4 → 2 → 1 → 1 frames.
	outChs := [4]int{128, 64, 64, 128}
	strides := [4]int{1, 2, 2, 1}
	t := encT
	for i := 0; i < 4; i++ {
		outLen := (t-1)/strides[i] + 1
		sc.conv[i] = make([]float32, outChs[i]*outLen)
		t = outLen
	}
	// Gather buffers for SIMD conv: one per input frame, max inCh = 129 (FreqBins)
	maxInCh := hp.FreqBins
	sc.inCols = make([][]float32, encT)
	for i := range sc.inCols {
		sc.inCols[i] = make([]float32, maxInCh)
	}
	s.scratch = sc
	return sc
}

// Detect runs VAD on a single window of audio and returns the speech probability.
// pcm must contain at least WindowSize (512) samples of 16kHz mono float32 PCM.
// State is updated in-place for streaming use.
//
// The forward pass follows the reference Silero VAD v5 model exactly:
// 64-sample context prepend → ReflectionPad1d(128) → STFT conv → drop first
// frame → magnitude [129, 4] → encoder convs (k=3, pad=1, strides 1,2,2,1) →
// single LSTM step → ReLU → 1x1 conv → sigmoid.
func (m *Model) Detect(pcm []float32, state *State) (float32, error) {
	hp := &m.hp
	ws := hp.WindowSize
	if len(pcm) < ws {
		return 0, fmt.Errorf("vad: input too short (%d < %d)", len(pcm), ws)
	}

	ctxSize := hp.StftSize / 4 // 64
	pad := hp.StftSize / 2     // 128

	sc := state.ensureScratch(hp)
	freqBins := hp.FreqBins
	hidden := hp.HiddenSize

	// Step 1: context + window, then ReflectionPad1d(128) on both sides.
	src := sc.stftIn
	copy(src[:ctxSize], state.Context)
	copy(src[ctxSize:], pcm[:ws])
	n := len(src)
	padded := sc.stftPadded
	for i := 0; i < pad; i++ {
		padded[i] = src[pad-i]       // left: src[128..1] mirrored
		padded[pad+n+i] = src[n-2-i] // right: src[n-2..n-129] mirrored
	}
	copy(padded[pad:pad+n], src)

	// Step 2: STFT conv [258, 5] → drop first frame → magnitude [129, 4]
	encT := (n+2*pad-hp.StftSize)/hp.StftStride + 1 - 1 // 4
	conv1dStrideSimd(sc.stftOut, padded, m.w.stft.W, m.w.stft.OutCh, m.w.stft.K, hp.StftStride)
	magnitudeFromSTFT(sc.mag, sc.stftOut, freqBins, encT, 1)

	// Step 3: Encoder conv blocks (4 layers, kernel=3, padding=1, ReLU).
	// Strides 1,2,2,1 collapse time: 4 → 2 → 1 → 1 frame.
	strides := [4]int{1, 2, 2, 1}
	h := sc.mag
	hCh := freqBins
	hLen := encT
	for i := 0; i < 4; i++ {
		enc := &m.w.encoder[i]
		outLen := (hLen-1)/strides[i] + 1
		conv1dPad1Simd(sc.conv[i], h, hCh, hLen, enc.W, enc.B, enc.OutCh, enc.InCh, enc.K, strides[i], enc.WT, sc.inCols)
		reluInPlace(sc.conv[i][:enc.OutCh*outLen])
		h = sc.conv[i]
		hCh = enc.OutCh
		hLen = outLen
	}

	// Step 4: single LSTM step on the collapsed frame
	copy(sc.hTmp, state.H)
	copy(sc.cTmp, state.C)
	for c := 0; c < hCh; c++ {
		sc.xt[c] = h[c*hLen]
	}
	lstmCellSimd(sc.xt, sc.hTmp, sc.cTmp, sc.gates,
		m.w.lstmWIH, m.w.lstmWHH, m.w.lstmBIH, m.w.lstmBHH,
		hCh, hidden)

	// Step 5: output branch — ReLU then 1x1 conv (dot) + sigmoid.
	copy(sc.outH, sc.hTmp)
	reluInPlace(sc.outH)
	var logit float32
	for j := 0; j < hidden; j++ {
		logit += m.w.outW[j] * sc.outH[j]
	}
	logit += m.w.outB
	prob := sigmoid(logit)

	// Update state (raw LSTM state, pre-ReLU) and the rolling context.
	copy(state.H, sc.hTmp)
	copy(state.C, sc.cTmp)
	copy(state.Context, pcm[ws-ctxSize:ws])
	state.SpeechProb = prob

	// State machine
	srPerMs := hp.SampleRate / 1000
	if prob >= hp.Threshold {
		state.SpeechSamples += ws
		state.SilenceSamples = 0
		if state.SpeechSamples >= hp.MinSpeechMs*srPerMs {
			state.IsSpeaking = true
		}
	} else {
		state.SilenceSamples += ws
		if state.IsSpeaking && state.SilenceSamples >= hp.MinSilenceMs*srPerMs {
			state.IsSpeaking = false
			state.SpeechSamples = 0
		}
	}

	return prob, nil
}

// FilterSpeech removes silence from audio based on VAD.
// Preserves short silence gaps between non-adjacent speech segments
// to prevent word concatenation artifacts in ASR.
func (m *Model) FilterSpeech(pcm []float32) []float32 {
	state := m.NewState()
	ws := m.hp.WindowSize
	nWindows := len(pcm) / ws
	if nWindows == 0 {
		return nil
	}

	// First pass: per-window speech probabilities
	probs := make([]float32, nWindows)
	for i := 0; i < nWindows; i++ {
		p, _ := m.Detect(pcm[i*ws:(i+1)*ws], state)
		probs[i] = p
	}

	// Second pass: collect speech windows with gap preservation
	// Pre-allocate result with estimated capacity
	var result []float32
	prevWasSpeech := false
	for i := 0; i < nWindows; i++ {
		if probs[i] >= m.hp.Threshold {
			if !prevWasSpeech && len(result) > 0 {
				// Insert ~8ms silence gap between non-adjacent speech segments
				result = append(result, make([]float32, ws/4)...)
			}
			result = append(result, pcm[i*ws:(i+1)*ws]...)
			prevWasSpeech = true
		} else {
			prevWasSpeech = false
		}
	}
	return result
}

// ──────────────────────────────────────────────────────────────
// Operators (in-place variants to minimize allocations)
// ──────────────────────────────────────────────────────────────

// conv1dStrideInto performs 1D convolution with stride, writing into pre-allocated dst.
// input: [inputLen], weight: [outCh, 1, kSize], dst: [outCh * outLen]
func conv1dStrideInto(dst, input, weight []float32, outCh, kSize, stride int) {
	inputLen := len(input)
	outLen := (inputLen - kSize) / stride + 1
	for oc := 0; oc < outCh; oc++ {
		wOff := oc * kSize
		dstOff := oc * outLen
		for t := 0; t < outLen; t++ {
			var sum float32
			inOff := t * stride
			for k := 0; k < kSize; k++ {
				sum += weight[wOff+k] * input[inOff+k]
			}
			dst[dstOff+t] = sum
		}
	}
}

// conv1dPad1Into performs multi-channel 1D convolution with padding=1 into pre-allocated dst.
// Optimized: splits into left-edge, middle (no bounds check), right-edge.
func conv1dPad1Into(dst, input []float32, inCh, inLen int, weight, bias []float32, outCh, wInCh, kSize int) {
	pad := (kSize - 1) / 2 // =1 for kSize=3

	for oc := 0; oc < outCh; oc++ {
		biasVal := float32(0)
		if bias != nil && oc < len(bias) {
			biasVal = bias[oc]
		}
		dstOff := oc * inLen

		// Left edge (t=0): k=0 is out of bounds (pad)
		{
			var sum float32
			for ic := 0; ic < wInCh && ic < inCh; ic++ {
				wBase := (oc*wInCh + ic) * kSize
				iBase := ic * inLen
				for k := pad; k < kSize; k++ {
					sum += weight[wBase+k] * input[iBase+k-pad]
				}
			}
			dst[dstOff] = sum + biasVal
		}

		// Middle (t=1..inLen-2): no bounds check needed
		for t := 1; t < inLen-1; t++ {
			var sum float32
			for ic := 0; ic < wInCh && ic < inCh; ic++ {
				wBase := (oc*wInCh + ic) * kSize
				iBase := ic*inLen + t - pad
				for k := 0; k < kSize; k++ {
					sum += weight[wBase+k] * input[iBase+k]
				}
			}
			dst[dstOff+t] = sum + biasVal
		}

		// Right edge (t=inLen-1): k=kSize-1 is out of bounds
		if inLen > 1 {
			var sum float32
			t := inLen - 1
			for ic := 0; ic < wInCh && ic < inCh; ic++ {
				wBase := (oc*wInCh + ic) * kSize
				iBase := ic * inLen
				for k := 0; k < kSize-pad; k++ {
					sum += weight[wBase+k] * input[iBase+t-pad+k]
				}
			}
			dst[dstOff+t] = sum + biasVal
		}
	}
}

// lstmCellInPlace performs a single LSTM step, updating h and c in-place.
// gates is a pre-allocated [4*hidden] scratch buffer.
func lstmCellInPlace(x, h, c, gates []float32,
	wIH, wHH, bIH, bHH []float32,
	inputSize, hidden int) {

	h4 := 4 * hidden
	for i := 0; i < h4; i++ {
		var sum float32
		rowIH := i * inputSize
		for j := 0; j < inputSize; j++ {
			sum += wIH[rowIH+j] * x[j]
		}
		rowHH := i * hidden
		for j := 0; j < hidden; j++ {
			sum += wHH[rowHH+j] * h[j]
		}
		if bIH != nil {
			sum += bIH[i]
		}
		if bHH != nil {
			sum += bHH[i]
		}
		gates[i] = sum
	}

	for j := 0; j < hidden; j++ {
		iGate := sigmoid(gates[j])
		fGate := sigmoid(gates[hidden+j])
		gGate := tanhf(gates[2*hidden+j])
		oGate := sigmoid(gates[3*hidden+j])
		c[j] = fGate*c[j] + iGate*gGate
		h[j] = oGate * tanhf(c[j])
	}
}

func reluInPlace(x []float32) {
	for i := range x {
		if x[i] < 0 {
			x[i] = 0
		}
	}
}

// transposeConvWeights converts [outCh, inCh, K] to [outCh, K, inCh].
// This makes the inCh dimension contiguous for each (oc, k) pair,
// enabling SIMD dot products in conv1dPad1Simd.
func transposeConvWeights(w []float32, outCh, inCh, K int) []float32 {
	wt := make([]float32, len(w))
	for oc := 0; oc < outCh; oc++ {
		for ic := 0; ic < inCh; ic++ {
			for k := 0; k < K; k++ {
				// src: [oc, ic, k] = oc*inCh*K + ic*K + k
				// dst: [oc, k, ic] = oc*K*inCh + k*inCh + ic
				wt[oc*K*inCh+k*inCh+ic] = w[oc*inCh*K+ic*K+k]
			}
		}
	}
	return wt
}

func sigmoid(x float32) float32 {
	return float32(1.0 / (1.0 + math.Exp(-float64(x))))
}

func tanhf(x float32) float32 {
	return float32(math.Tanh(float64(x)))
}
