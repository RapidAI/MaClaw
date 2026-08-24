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

func fillRoPE(s *gemmaScratch, hp GemmaHParams, seq int) {
	headDim := hp.HeadDim
	halfDim := headDim / 2
	for pos := 0; pos < seq; pos++ {
		for i := 0; i < halfDim; i++ {
			freq := 1.0 / float32(math.Pow(float64(hp.RopeTheta), float64(2*i)/float64(headDim)))
			angle := float32(pos) * freq
			s.ropeCos[pos*halfDim+i] = float32(math.Cos(float64(angle)))
			s.ropeSin[pos*halfDim+i] = float32(math.Sin(float64(angle)))
		}
	}
	s.ropeSeq = seq
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
