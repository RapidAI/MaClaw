// corelib/asr/sensevoice.go — Pure Go SenseVoiceSmall inference.
//
// SenseVoiceSmall is Alibaba's encoder-only non-autoregressive ASR model:
// SAN-M encoder (70 blocks total) + CTC greedy decoder.
// Architecture: 80-mel fbank → LFR(7,6) → CMVN → SANM encoder → CTC head.
//
// GGUF compatibility:
//   - cstr/sensevoice-small-GGUF (recommended): Includes proper entry block
//     with 560→512 input projection. Download sensevoice-small-q8_0.gguf.
//   - FunAudioLLM/SenseVoiceSmall-GGUF: Entry block projection is NOT stored
//     as a tensor (it's handled by the FunASR llama.cpp runtime internally).
//     Falls back to lossy truncation → transcription quality will be degraded.
//
// The GGUF file is memory-mapped; call Close() to release.
package asr

import (
	"fmt"
	"math"
	"runtime"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/embedding/gguf"
	"github.com/RapidAI/CodeClaw/corelib/embedding/tensor"
)

// SenseVoiceHParams holds SenseVoiceSmall hyperparameters from GGUF metadata.
type SenseVoiceHParams struct {
	VocabSize    int
	HiddenSize   int // encoder hidden dim (512)
	LinearUnits  int // FFN intermediate (2048)
	NumHeads     int // attention heads (4)
	NumBlocks    int // main encoder blocks (50, includes encoders0)
	NumTPBlocks  int // TP encoder blocks (20)
	FSMNKernel   int // FSMN depthwise conv kernel size (11)
	FeatsDim     int // input feature dim after LFR (560)
}

// svLayerWeights holds weights for one SANM encoder block.
type svLayerWeights struct {
	// Self-attention: either separate Q/K/V or fused QKV
	fusedQKV bool // true = qW holds [3*hidden, inDim], qB holds [3*hidden]
	qW, qB   linearWeight
	kW, kB   linearWeight
	vW, vB   linearWeight
	outW, outB linearWeight
	// FSMN depthwise conv kernel [hiddenSize, kernelSize]
	fsmnW []float32
	// Feed-forward
	ff1W, ff1B linearWeight
	ff2W, ff2B linearWeight
	// LayerNorm
	norm1W, norm1B []float32
	norm2W, norm2B []float32
}

// svWeights holds all model weights.
type svWeights struct {
	embedding []float32 // [numEmbeddings, featsDim] prompt embeddings

	encoder0   svLayerWeights   // entry block (encoders0.0): 560→512
	encoders   []svLayerWeights // main encoder blocks (encoder.encoders.0..N-1)
	tpEncoders []svLayerWeights // TP blocks (encoder.tp_encoders.0..M-1)

	afterNormW, afterNormB []float32 // norm between main and TP
	tpNormW, tpNormB       []float32 // final norm

	ctcW linearWeight // CTC output linear [vocab, hidden]
	ctcB []float32    // CTC output bias [vocab]
}

// SenseVoiceModel is a pure Go SenseVoiceSmall ASR model.
type SenseVoiceModel struct {
	hp    SenseVoiceHParams
	w     svWeights
	vocab []string
	// CMVN statistics
	cmvnMeans []float32
	cmvnIstd  []float32
	mmap      *gguf.MmapFile
	// Pre-allocated encoder buffers (avoid alloc in hot loop)
	encBufs *svEncoderBufs
	mu      sync.Mutex
}

// svEncoderBufs holds pre-allocated buffers for encoder forward pass.
// Ping-pong bufA/bufB eliminate per-layer heap allocations across ~70 SANM blocks.
type svEncoderBufs struct {
	// Layer I/O (ping-pong)
	featsBuf []float32 // [maxFrames * featsDim] entry input
	bufA     []float32 // [maxFrames * hidden]
	bufB     []float32 // [maxFrames * hidden]

	// SANM scratch
	q, k, v   []float32 // [maxFrames * hidden]
	qkv       []float32 // [maxFrames * 3 * hidden] fused QKV
	residual  []float32 // [maxFrames * hidden] (or maxFrames*featsDim for entry residual path)
	residual2 []float32 // [maxFrames * hidden]
	attnOut   []float32 // [maxFrames * hidden]
	projOut   []float32 // [maxFrames * hidden]
	fsmnOut   []float32 // [maxFrames * hidden]
	fsmnTmp   []float32 // [hidden] FSMN elementwise mul scratch
	ffOut     []float32 // [maxFrames * ffDim]
	logits    []float32 // [maxFrames * vocab]

	// Attention scores
	attnScores    []float32 // [maxFrames] single-worker scores
	scoresScratch []float32 // [nWorkers * maxFrames]

	// Cached positional encoding [maxPosFrames * dim]
	posEnc    []float32
	posEncDim int

	// Frontend scratch (reused under model lock)
	fbankOut []float32
	lfrOut   []float32

	maxFrames int
}

// NewSenseVoice loads a SenseVoiceSmall model from a GGUF file.
func NewSenseVoice(modelPath string) (*SenseVoiceModel, error) {
	mf, err := gguf.OpenMmap(modelPath)
	if err != nil {
		return nil, fmt.Errorf("sensevoice: open gguf: %w", err)
	}

	hp := SenseVoiceHParams{
		VocabSize:   gguf.GetMetaI32(mf.Meta, "sv.vocab_size", 25055),
		HiddenSize:  gguf.GetMetaI32(mf.Meta, "sv.output_size", 512),
		LinearUnits: 2048, // inferred from tensor shape
		NumHeads:    gguf.GetMetaI32(mf.Meta, "sv.attention_heads", 4),
		NumBlocks:   gguf.GetMetaI32(mf.Meta, "sv.num_blocks", 50),
		NumTPBlocks: gguf.GetMetaI32(mf.Meta, "sv.tp_blocks", 20),
		FSMNKernel:  gguf.GetMetaI32(mf.Meta, "sv.kernel_size", 11),
		FeatsDim:    gguf.GetMetaI32(mf.Meta, "sv.input_size", svFeatsDim),
	}

	m := &SenseVoiceModel{hp: hp, mmap: mf}
	if err := m.loadWeights(mf); err != nil {
		mf.CloseMmap()
		return nil, err
	}
	m.loadVocab(mf)
	m.loadCMVN(mf)
	return m, nil
}

// Close releases the mmap and all resources.
func (m *SenseVoiceModel) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.mmap != nil {
		m.mmap.CloseMmap()
		m.mmap = nil
	}
	m.encBufs = nil
}

// ensureEncBufs ensures encoder buffers are allocated for the given frame count.
func (m *SenseVoiceModel) ensureEncBufs(maxFrames int) *svEncoderBufs {
	if m.encBufs != nil && m.encBufs.maxFrames >= maxFrames {
		return m.encBufs
	}
	// Grow with headroom so short utterances don't reallocate constantly.
	if maxFrames < 128 {
		maxFrames = 128
	}
	hidden := m.hp.HiddenSize
	ffDim := m.hp.LinearUnits
	featsDim := m.hp.FeatsDim
	if featsDim == 0 {
		featsDim = svFeatsDim
	}
	vocab := m.hp.VocabSize
	// residual must hold the larger of featsDim (entry) and hidden
	resDim := hidden
	if featsDim > resDim {
		resDim = featsDim
	}
	nWorkers := runtime.NumCPU()
	if nWorkers < 4 {
		nWorkers = 4
	}
	// Attention Q-tile needs 8 score rows + 8×headDim Q panel.
	headDim := hidden / m.hp.NumHeads
	if headDim < 1 {
		headDim = 1
	}
	scoreScratchN := nWorkers * maxFrames
	if need := 8*maxFrames + 8*headDim; need > scoreScratchN {
		scoreScratchN = need
	}
	m.encBufs = &svEncoderBufs{
		featsBuf:      make([]float32, maxFrames*featsDim),
		bufA:          make([]float32, maxFrames*hidden),
		bufB:          make([]float32, maxFrames*hidden),
		q:             make([]float32, maxFrames*hidden),
		k:             make([]float32, maxFrames*hidden),
		v:             make([]float32, maxFrames*hidden),
		qkv:           make([]float32, maxFrames*3*hidden),
		residual:      make([]float32, maxFrames*resDim),
		residual2:     make([]float32, maxFrames*hidden),
		attnOut:       make([]float32, maxFrames*hidden),
		projOut:       make([]float32, maxFrames*hidden),
		fsmnOut:       make([]float32, maxFrames*hidden),
		fsmnTmp:       make([]float32, hidden),
		ffOut:         make([]float32, maxFrames*ffDim),
		logits:        make([]float32, maxFrames*vocab),
		attnScores:    make([]float32, maxFrames),
		scoresScratch: make([]float32, scoreScratchN),
		maxFrames:     maxFrames,
	}
	return m.encBufs
}

// Transcribe takes 16kHz mono float32 PCM and returns text.
func (m *SenseVoiceModel) Transcribe(pcm []float32) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(pcm) == 0 {
		return "", nil
	}

	// 1. Frame counts
	numFbankFrames := (len(pcm) - svWindowSize) / svHopSize + 1
	if numFbankFrames <= 0 {
		return "", fmt.Errorf("sensevoice: audio too short for fbank")
	}
	lfrFrames := (numFbankFrames - svLFRm) / svLFRn + 1
	if lfrFrames <= 0 {
		lfrFrames = 1
	}
	totalFrames := 4 + lfrFrames

	// 2. Ensure all scratch (encoder + frontend) in one shot
	bufs := m.ensureEncBufs(totalFrames)
	fbankNeed := numFbankFrames * svNumMels
	if cap(bufs.fbankOut) < fbankNeed {
		bufs.fbankOut = make([]float32, fbankNeed)
	}
	fbank := bufs.fbankOut[:fbankNeed]
	if !svMelFilterbankInto(pcm, fbank) {
		return "", fmt.Errorf("sensevoice: audio too short for fbank")
	}

	lfrNeed := lfrFrames * svFeatsDim
	if cap(bufs.lfrOut) < lfrNeed {
		bufs.lfrOut = make([]float32, lfrNeed)
	}
	lfrFeats := bufs.lfrOut[:lfrNeed]
	lfrFrames = svApplyLFRInto(fbank, numFbankFrames, lfrFeats)

	// 3. CMVN intentionally skipped for this GGUF convention (see loadCMVN comments).
	_ = m.cmvnIstd

	// 4. Encode — returns buffer owned by model until unlock
	encOut := m.encode(lfrFeats, lfrFrames)

	// 5. CTC decode
	tokens := m.ctcGreedyDecode(encOut, lfrFrames+4)

	// 6. Detokenize
	return m.svDetokenize(tokens), nil
}

func (m *SenseVoiceModel) loadWeights(mf *gguf.MmapFile) error {
	hp := m.hp
	hidden := hp.HiddenSize
	ffDim := hp.LinearUnits

	getF32 := func(name string) ([]float32, error) {
		return mf.TensorF32(name)
	}
	tryF32 := func(name string) []float32 {
		d, _ := mf.TensorF32(name)
		return d
	}
	getLinear := func(name string, rows, cols int) linearWeight {
		raw, ti, err := mf.TensorRawBytes(name)
		if err != nil {
			d, _ := getF32(name)
			if d != nil {
				if cols <= 0 && rows > 0 {
					cols = len(d) / rows
				}
				// Keep as F32 if cols not Q8-aligned (must be multiple of 32)
				if cols%32 != 0 {
					return linearWeight{f32: d, rows: rows}
				}
				return linearWeight{q8: tensor.QuantizeToQ8(d, rows, cols), rows: rows}
			}
			return linearWeight{}
		}
		// Infer cols from tensor dimensions if not provided
		if cols <= 0 && rows > 0 {
			ne := ti.NumElements()
			cols = int(ne) / rows
		}
		if ti.Type == gguf.TypeQ8_0 {
			q := &tensor.Q8Tensor{Data: raw, Rows: rows, Cols: cols}
			q.PrepareScales() // f16→f32 once at load; hot dequant skips conversion
			return linearWeight{q8: q, rows: rows}
		}
		d, _ := getF32(name)
		if d != nil {
			if cols <= 0 && rows > 0 {
				cols = len(d) / rows
			}
			// Keep as F32 if cols not Q8-aligned
			if cols%32 != 0 {
				return linearWeight{f32: d, rows: rows}
			}
			return linearWeight{q8: tensor.QuantizeToQ8(d, rows, cols), rows: rows}
		}
		return linearWeight{}
	}

	loadLayer := func(prefix string) svLayerWeights {
		l := svLayerWeights{}
		// Check if QKV is fused (linear_q_k_v) or separate (linear_q, linear_k, linear_v)
		fusedQKVName := prefix + ".self_attn.linear_q_k_v.weight"
		if _, _, err := mf.TensorRawBytes(fusedQKVName); err == nil {
			// Fused QKV: [inDim, 3*hidden] — we load as one and split later
			l.fusedQKV = true
			l.qW = getLinear(fusedQKVName, 3*hidden, -1) // special: fused
			l.qB = linearWeight{f32: tryF32(prefix + ".self_attn.linear_q_k_v.bias"), rows: 1}
			// Mark k/v as empty — they come from the fused weight
			l.kW = linearWeight{}
			l.kB = linearWeight{}
			l.vW = linearWeight{}
			l.vB = linearWeight{}
		} else {
			l.qW = getLinear(prefix+".self_attn.linear_q.weight", hidden, hidden)
			l.qB = linearWeight{f32: tryF32(prefix + ".self_attn.linear_q.bias"), rows: 1}
			l.kW = getLinear(prefix+".self_attn.linear_k.weight", hidden, hidden)
			l.kB = linearWeight{f32: tryF32(prefix + ".self_attn.linear_k.bias"), rows: 1}
			l.vW = getLinear(prefix+".self_attn.linear_v.weight", hidden, hidden)
			l.vB = linearWeight{f32: tryF32(prefix + ".self_attn.linear_v.bias"), rows: 1}
		}
		l.outW = getLinear(prefix+".self_attn.linear_out.weight", hidden, hidden)
		l.outB = linearWeight{f32: tryF32(prefix + ".self_attn.linear_out.bias"), rows: 1}
		l.fsmnW = tryF32(prefix + ".self_attn.fsmn_block.weight")
		l.ff1W = getLinear(prefix+".feed_forward.w_1.weight", ffDim, hidden)
		l.ff1B = linearWeight{f32: tryF32(prefix + ".feed_forward.w_1.bias"), rows: 1}
		l.ff2W = getLinear(prefix+".feed_forward.w_2.weight", hidden, ffDim)
		l.ff2B = linearWeight{f32: tryF32(prefix + ".feed_forward.w_2.bias"), rows: 1}
		l.norm1W = tryF32(prefix + ".norm1.weight")
		l.norm1B = tryF32(prefix + ".norm1.bias")
		l.norm2W = tryF32(prefix + ".norm2.weight")
		l.norm2B = tryF32(prefix + ".norm2.bias")
		return l
	}

	// Embedding for prompt tokens
	m.w.embedding = tryF32("embed.weight")

	// Entry block (encoders0.0) — has 560-dim input, 512-dim output
	// This block's QKV is stored as F32 (560 not Q8-aligned)
	m.w.encoder0 = loadLayer("encoder.encoders0.0")

	// Main encoder blocks — probe to find actual count
	actualBlocks := 0
	for i := 0; i < 100; i++ {
		probe := fmt.Sprintf("encoder.encoders.%d.norm1.weight", i)
		if _, _, err := mf.TensorRawBytes(probe); err != nil {
			break
		}
		actualBlocks++
	}

	m.w.encoders = make([]svLayerWeights, actualBlocks)
	for i := 0; i < actualBlocks; i++ {
		m.w.encoders[i] = loadLayer(fmt.Sprintf("encoder.encoders.%d", i))
	}

	// TP encoder blocks — only load if tensors exist in the GGUF
	m.w.tpEncoders = nil
	if hp.NumTPBlocks > 0 {
		// Check if the first TP block tensor exists
		tpPrefix := fmt.Sprintf("encoder.tp_encoders.0.norm1.weight")
		if _, _, err := mf.TensorRawBytes(tpPrefix); err == nil {
			m.w.tpEncoders = make([]svLayerWeights, hp.NumTPBlocks)
			for i := 0; i < hp.NumTPBlocks; i++ {
				m.w.tpEncoders[i] = loadLayer(fmt.Sprintf("encoder.tp_encoders.%d", i))
			}
		}
	}

	// Norms
	m.w.afterNormW = tryF32("encoder.after_norm.weight")
	m.w.afterNormB = tryF32("encoder.after_norm.bias")
	m.w.tpNormW = tryF32("encoder.tp_norm.weight")
	m.w.tpNormB = tryF32("encoder.tp_norm.bias")

	// CTC head
	m.w.ctcW = getLinear("ctc.ctc_lo.weight", hp.VocabSize, hidden)
	m.w.ctcB = tryF32("ctc.ctc_lo.bias")

	return nil
}

func (m *SenseVoiceModel) loadVocab(mf *gguf.MmapFile) {
	tokens := gguf.GetMetaStrArr(mf.Meta, "sv.vocab")
	if tokens == nil {
		tokens = gguf.GetMetaStrArr(mf.Meta, "tokenizer.ggml.tokens")
	}
	if tokens == nil {
		tokens = gguf.GetMetaStrArr(mf.Meta, "tokenizer.tokens")
	}
	m.vocab = make([]string, len(tokens))
	copy(m.vocab, tokens)
}

func (m *SenseVoiceModel) loadCMVN(mf *gguf.MmapFile) {
	// CMVN in this GGUF:
	// cmvn.scale [560] — the inverse std to multiply after centering
	// cmvn.shift [1] or [560] — if scalar, no centering needed (shift=0 convention)
	scale, _ := mf.TensorF32("cmvn.scale")
	shift, _ := mf.TensorF32("cmvn.shift")

	if scale != nil && len(scale) == m.hp.FeatsDim {
		m.cmvnIstd = scale
		// If shift is a proper vector, use it; otherwise no shift
		if shift != nil && len(shift) == m.hp.FeatsDim {
			m.cmvnMeans = shift
		}
	}
}

// svApplyCMVNShiftScale applies CMVN using shift+scale (FunASR convention):
// x[i] = (x[i] + shift[i%dim]) * scale[i%dim]
func svApplyCMVNShiftScale(x []float32, numFrames, dim int, shift, scale []float32) {
	for f := 0; f < numFrames; f++ {
		off := f * dim
		for d := 0; d < dim; d++ {
			x[off+d] = (x[off+d] + shift[d]) * scale[d]
		}
	}
}

// ctcGreedyDecode applies argmax at each frame, collapses repeats, removes blank (0).
// blankPenalty reduces the blank token's logit to improve recall on borderline frames.
func (m *SenseVoiceModel) ctcGreedyDecode(logits []float32, numFrames int) []int {
	vocab := m.hp.VocabSize
	tokens := make([]int, 0, 32)
	prevToken := -1

	for f := 0; f < numFrames; f++ {
		off := f * vocab
		row := logits[off : off+vocab]
		bestID := 0
		bestVal := row[0]
		// 4-wide unroll for argmax ILP
		i := 1
		for ; i+3 < vocab; i += 4 {
			v0, v1, v2, v3 := row[i], row[i+1], row[i+2], row[i+3]
			if v0 > bestVal {
				bestVal, bestID = v0, i
			}
			if v1 > bestVal {
				bestVal, bestID = v1, i+1
			}
			if v2 > bestVal {
				bestVal, bestID = v2, i+2
			}
			if v3 > bestVal {
				bestVal, bestID = v3, i+3
			}
		}
		for ; i < vocab; i++ {
			if row[i] > bestVal {
				bestVal = row[i]
				bestID = i
			}
		}
		// CTC: collapse repeats and skip blank (token 0)
		if bestID != prevToken {
			if bestID != 0 {
				tokens = append(tokens, bestID)
			}
		}
		prevToken = bestID
	}
	return tokens
}

// ctcBeamSearch performs CTC prefix beam search for better accuracy.
// beamWidth controls the number of active hypotheses (default: 10).
func (m *SenseVoiceModel) ctcBeamSearch(logits []float32, numFrames, beamWidth int) []int {
	vocab := m.hp.VocabSize
	const blankID = 0

	// Initialize with empty prefix
	beams := []svBeam{{tokens: nil, pB: 0, pNB: negInf}} // log-space: pB=log(1)=0, pNB=log(0)=-inf

	for f := 0; f < numFrames; f++ {
		off := f * vocab
		// Convert logits to log-softmax
		logProbs := make([]float64, vocab)
		maxLogit := float64(logits[off])
		for v := 1; v < vocab; v++ {
			if float64(logits[off+v]) > maxLogit {
				maxLogit = float64(logits[off+v])
			}
		}
		var sumExp float64
		for v := 0; v < vocab; v++ {
			logProbs[v] = float64(logits[off+v]) - maxLogit
			sumExp += math.Exp(logProbs[v])
		}
		logSumExp := math.Log(sumExp)
		for v := 0; v < vocab; v++ {
			logProbs[v] -= logSumExp
		}

		// Expand beams
		nextBeams := make(map[string]*svBeam)
		getOrCreate := func(tokens []int) *svBeam {
			key := tokensKey(tokens)
			if b, ok := nextBeams[key]; ok {
				return b
			}
			b := &svBeam{tokens: tokens, pB: negInf, pNB: negInf}
			nextBeams[key] = b
			return b
		}

		for _, b := range beams {
			// Total log-prob of this beam
			pTotal := logAdd(b.pB, b.pNB)

			// 1. Extend with blank
			nb := getOrCreate(b.tokens)
			nb.pB = logAdd(nb.pB, pTotal+logProbs[blankID])

			// 2. Extend with non-blank tokens (only top-K for efficiency)
			topK := beamTopK(logProbs, vocab, beamWidth*2, blankID)
			for _, c := range topK {
				if len(b.tokens) > 0 && b.tokens[len(b.tokens)-1] == c {
					// Same as last token: only non-blank-ending paths can extend
					nb2 := getOrCreate(b.tokens)
					nb2.pNB = logAdd(nb2.pNB, b.pB+logProbs[c])
					// Also allow extending the prefix (repeat after blank)
					extended := append(append([]int{}, b.tokens...), c)
					nb3 := getOrCreate(extended)
					nb3.pNB = logAdd(nb3.pNB, b.pNB+logProbs[c])
				} else {
					// Different token: extend prefix
					newTokens := append(append([]int{}, b.tokens...), c)
					nb2 := getOrCreate(newTokens)
					nb2.pNB = logAdd(nb2.pNB, pTotal+logProbs[c])
				}
			}
		}

		// Prune to top beamWidth
		beams = beams[:0]
		for _, b := range nextBeams {
			b.logP = logAdd(b.pB, b.pNB)
			beams = append(beams, *b)
		}
		svSortBeams(beams)
		if len(beams) > beamWidth {
			beams = beams[:beamWidth]
		}
	}

	if len(beams) == 0 {
		return nil
	}
	return beams[0].tokens
}

type svBeam struct {
	tokens []int
	logP   float64 // combined log probability
	pB     float64 // log prob of paths ending with blank
	pNB    float64 // log prob of paths ending with non-blank
}

const negInf = -1e30

func logAdd(a, b float64) float64 {
	if a < b {
		a, b = b, a
	}
	if b == negInf {
		return a
	}
	return a + math.Log1p(math.Exp(b-a))
}

func tokensKey(tokens []int) string {
	if len(tokens) == 0 {
		return ""
	}
	b := make([]byte, len(tokens)*4)
	for i, t := range tokens {
		b[i*4] = byte(t)
		b[i*4+1] = byte(t >> 8)
		b[i*4+2] = byte(t >> 16)
		b[i*4+3] = byte(t >> 24)
	}
	return string(b)
}

func beamTopK(logProbs []float64, vocab, k, skipID int) []int {
	// Simple partial sort for top-K non-blank tokens
	type kv struct {
		id int
		lp float64
	}
	topk := make([]kv, 0, k)
	for v := 0; v < vocab; v++ {
		if v == skipID {
			continue
		}
		if len(topk) < k {
			topk = append(topk, kv{v, logProbs[v]})
			// Bubble up
			for i := len(topk) - 1; i > 0 && topk[i].lp > topk[i-1].lp; i-- {
				topk[i], topk[i-1] = topk[i-1], topk[i]
			}
		} else if logProbs[v] > topk[k-1].lp {
			topk[k-1] = kv{v, logProbs[v]}
			for i := k - 2; i >= 0 && topk[i+1].lp > topk[i].lp; i-- {
				topk[i], topk[i+1] = topk[i+1], topk[i]
			}
		}
	}
	ids := make([]int, len(topk))
	for i, x := range topk {
		ids[i] = x.id
	}
	return ids
}

func svSortBeams(beams []svBeam) {
	for i := 1; i < len(beams); i++ {
		for j := i; j > 0 && beams[j].logP > beams[j-1].logP; j-- {
			beams[j], beams[j-1] = beams[j-1], beams[j]
		}
	}
}

// svDetokenize converts CTC output token IDs to text.
// Strips special tags (<|xx|>) and handles SentencePiece ▁ tokens.
func (m *SenseVoiceModel) svDetokenize(tokens []int) string {
	var sb strings.Builder
	for _, tid := range tokens {
		if tid < 0 || tid >= len(m.vocab) {
			continue
		}
		tok := m.vocab[tid]

		// Skip special tags like <|zh|>, <|NEUTRAL|>, <|Speech|>, <|withitn|>
		if len(tok) >= 4 && tok[:2] == "<|" && tok[len(tok)-2:] == "|>" {
			continue
		}
		// Skip other special tokens
		if len(tok) >= 2 && tok[0] == '<' && tok[len(tok)-1] == '>' {
			continue
		}

		// Decode byte tokens like <0xNN>
		if len(tok) == 6 && tok[0] == '<' && tok[1] == '0' && tok[2] == 'x' && tok[5] == '>' {
			hi := hexVal(tok[3])
			lo := hexVal(tok[4])
			if hi >= 0 && lo >= 0 {
				sb.WriteByte(byte(hi<<4 | lo))
				continue
			}
		}

		// Replace SentencePiece ▁ (U+2581) with space
		tok = strings.ReplaceAll(tok, "\xe2\x96\x81", " ")
		sb.WriteString(tok)
	}

	result := sb.String()
	result = removeCJKSpaces(result)
	return strings.TrimSpace(result)
}
