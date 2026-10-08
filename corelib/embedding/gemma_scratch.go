package embedding

import (
	"math"
)

const (
	scratchBucket16  = 16
	scratchBucket64  = 64
	scratchBucket256 = 256
	scratchBucket512 = 512
	yTileRows        = 8
)

func scratchBucket(seq int) int {
	switch {
	case seq <= scratchBucket16:
		return scratchBucket16
	case seq <= scratchBucket64:
		return scratchBucket64
	case seq <= scratchBucket256:
		return scratchBucket256
	case seq <= scratchBucket512:
		return scratchBucket512
	default:
		return seq
	}
}

func scratchReusable(seqCap int) bool {
	return seqCap <= scratchBucket512
}

func scratchPhaseFloats(hp GemmaHParams, S int) int {
	attn := S * (2*hp.Dim + 2*hp.KVDim)
	ffn := S * 2 * hp.FFDim
	if ffn > attn {
		return ffn
	}
	return attn
}

// scratchArenaFloats is the C.3 layout size for seqCap S (activation + rope + yTile + small).
func scratchArenaFloats(hp GemmaHParams, S int) int {
	if S <= 0 {
		return 0
	}
	halfDim := hp.HeadDim / 2
	return S*2*hp.Dim + scratchPhaseFloats(hp, S) + S*hp.Dim + yTileRows*hp.Dim + 2*S*halfDim + S + 2*hp.Dim
}

func (s *gemmaScratch) bytes() int {
	if s == nil {
		return 0
	}
	return len(s.arena) * 4
}

func bindGemmaScratch(s *gemmaScratch, hp GemmaHParams, S int) {
	dim := hp.Dim
	kvDim := hp.KVDim
	ffDim := hp.FFDim
	halfDim := hp.HeadDim / 2
	n := scratchArenaFloats(hp, S)
	if cap(s.arena) < n {
		s.arena = make([]float32, n)
	} else {
		s.arena = s.arena[:n]
	}
	a := s.arena
	act := S * 2 * dim
	phaseN := scratchPhaseFloats(hp, S)
	s.x = a[0 : S*dim]
	s.normed = a[S*dim : act]
	s.q = a[act : act+S*dim]
	s.k = a[act+S*dim : act+S*dim+S*kvDim]
	s.v = a[act+S*dim+S*kvDim : act+S*dim+2*S*kvDim]
	s.attnOut = a[act+S*(dim+2*kvDim) : act+S*(2*dim+2*kvDim)]
	s.ffGate = a[act : act+S*ffDim]
	s.ffUp = a[act+S*ffDim : act+S*2*ffDim]
	residual := act + phaseN
	s.projOut = a[residual : residual+S*dim]
	s.ffDown = s.projOut
	yOff := residual + S*dim
	s.yTile = a[yOff : yOff+yTileRows*dim]
	ropeOff := yOff + yTileRows*dim
	s.ropeCos = a[ropeOff : ropeOff+S*halfDim]
	s.ropeSin = a[ropeOff+S*halfDim : ropeOff+2*S*halfDim]
	scoreOff := ropeOff + 2*S*halfDim
	s.scores = a[scoreOff : scoreOff+S]
	small := scoreOff + S
	s.rowBuf = a[small : small+dim]
	s.poolOut = a[small+dim : small+2*dim]
	s.seqCap = S
}

// ropeFreq is headDim/2 inverse frequencies for one RoPE theta.
// Cached on the scratch, not in the arena.
type ropeFreq struct {
	theta   float32
	headDim int
	inv     []float32
}

func (f *ropeFreq) invFreq(theta float32, headDim int) []float32 {
	half := headDim / 2
	if half <= 0 {
		return nil
	}
	if f.theta == theta && f.headDim == headDim && len(f.inv) == half {
		return f.inv
	}
	if cap(f.inv) < half {
		f.inv = make([]float32, half)
	} else {
		f.inv = f.inv[:half]
	}
	hd := float64(headDim)
	th := float64(theta)
	for i := 0; i < half; i++ {
		// Same float32 rounding as the old per-position pow.
		f.inv[i] = 1.0 / float32(math.Pow(th, float64(2*i)/hd))
	}
	f.theta = theta
	f.headDim = headDim
	return f.inv
}

func fillRoPETable(dstCos, dstSin, inv []float32, seq int) {
	halfDim := len(inv)
	for pos := 0; pos < seq; pos++ {
		p := float32(pos)
		base := pos * halfDim
		for i := 0; i < halfDim; i++ {
			angle := p * inv[i]
			dstCos[base+i] = float32(math.Cos(float64(angle)))
			dstSin[base+i] = float32(math.Sin(float64(angle)))
		}
	}
}

func fillRoPE(s *gemmaScratch, hp GemmaHParams, seq int) {
	s.ropeSeq = seq
	s.ropeKind = 0
	s.ropeHave = [3]bool{}
	ropeForLayer(s, hp, 0)
}

// ropeForLayer returns cos/sin for this layer. Local and global thetas are
// each filled once: local reuses the arena table, global is parked beside it.
// EmbeddingGemma switches theta eight times per forward (five local layers,
// then one global). Refilling on every switch was 995 µs at the seq-64 bucket
// and 10.7 ms at seq 578. Inverse frequencies stay cached per theta.
func ropeForLayer(sc *gemmaScratch, hp GemmaHParams, layer int) (cos, sin []float32) {
	kind := 2
	theta := hp.RopeThetaLocal
	freq := &sc.ropeLocal
	if hp.globalLayer(layer) {
		kind = 1
		theta = hp.RopeTheta
		freq = &sc.ropeGlobal
	}
	if theta <= 0 {
		if kind == 1 {
			theta = 1e6
		} else {
			theta = 10000
		}
	}
	if sc.ropeHave[kind] {
		sc.ropeKind = kind
		return sc.ropeKeepCos[kind], sc.ropeKeepSin[kind]
	}
	half := hp.HeadDim / 2
	n := sc.ropeSeq * half
	var dstC, dstS []float32
	if kind == 2 && n > 0 && len(sc.ropeCos) == n {
		dstC, dstS = sc.ropeCos, sc.ropeSin
	} else if n > 0 && cap(sc.ropeKeepCos[kind]) >= n {
		dstC = sc.ropeKeepCos[kind][:n]
		dstS = sc.ropeKeepSin[kind][:n]
	} else if n > 0 {
		dstC = make([]float32, n)
		dstS = make([]float32, n)
	}
	if n > 0 {
		fillRoPETable(dstC, dstS, freq.invFreq(theta, hp.HeadDim), sc.ropeSeq)
	}
	sc.ropeKeepCos[kind] = dstC
	sc.ropeKeepSin[kind] = dstS
	sc.ropeHave[kind] = true
	sc.ropeKind = kind
	return dstC, dstS
}

func newGemmaScratch(hp GemmaHParams, seq int) *gemmaScratch {
	S := scratchBucket(seq)
	s := &gemmaScratch{}
	bindGemmaScratch(s, hp, S)
	fillRoPE(s, hp, S)
	return s
}

func poolIndex(seqCap int) int {
	switch seqCap {
	case scratchBucket16:
		return 0
	case scratchBucket64:
		return 1
	case scratchBucket256:
		return 2
	case scratchBucket512:
		return 3
	default:
		return -1
	}
}

func (g *GemmaEmbedder) getScratchFromPool(seq int) *gemmaScratch {
	S := scratchBucket(seq)
	if idx := poolIndex(S); idx >= 0 {
		if v := g.scratchPools[idx].Get(); v != nil {
			s := v.(*gemmaScratch)
			if s.seqCap == S {
				return s
			}
		}
	}
	return newGemmaScratch(g.hp, seq)
}

func (g *GemmaEmbedder) putScratchToPool(s *gemmaScratch) {
	if g == nil || s == nil || !scratchReusable(s.seqCap) {
		return
	}
	if idx := poolIndex(s.seqCap); idx >= 0 {
		g.scratchPools[idx].Put(s)
	}
}
