package embedding

import (
	"math"
	"testing"
)

func TestScratchArenaBounds(t *testing.T) {
	hp := GemmaHParams{Dim: 768, KVDim: 256, FFDim: 1152, HeadDim: 256, RopeTheta: 1e6}
	for _, seq := range []int{64, 256} {
		s := newGemmaScratch(hp, seq)
		miB := float64(s.bytes()) / (1024 * 1024)
		limit := 1.3
		if seq == 256 {
			limit = 5.0
		}
		if miB > limit {
			t.Errorf("seq=%d scratch %.3f MiB exceeds %.1f", seq, miB, limit)
		}
		act := seq * 4608 * 4
		today := seq * 7680 * 4
		if act >= today {
			t.Errorf("activation body did not shrink")
		}
		t.Logf("seq=%d total=%.3f MiB activation=%.3f MiB", seq, miB, float64(act)/(1024*1024))
	}
}

func TestScratchOverlayLayoutAliases(t *testing.T) {
	hp := GemmaHParams{Dim: 768, KVDim: 256, FFDim: 1152, HeadDim: 256, RopeTheta: 1e6}
	s := newGemmaScratch(hp, 64)
	if &s.q[0] != &s.ffGate[0] {
		t.Fatal("q must overlay ffGate prefix")
	}
	if &s.projOut[0] != &s.ffDown[0] {
		t.Fatal("projOut must alias ffDown residual")
	}
	if &s.x[0] == &s.normed[0] {
		t.Fatal("x must not overlay normed")
	}
	if &s.yTile[0] == &s.projOut[0] || &s.yTile[0] == &s.x[0] || &s.yTile[0] == &s.attnOut[0] {
		t.Fatal("yTile must be independent")
	}
}

func TestScratchOverlayPoisonDoesNotChangeX(t *testing.T) {
	hp := GemmaHParams{Dim: 768, KVDim: 256, FFDim: 1152, HeadDim: 256, RopeTheta: 1e6}
	s := newGemmaScratch(hp, 64)
	for i := range s.x {
		s.x[i] = float32(i%9) * 0.1
	}
	copyX := append([]float32(nil), s.x...)
	nan := math.Float32frombits(0x7fc00000)
	for i := range s.yTile {
		s.yTile[i] = nan
	}
	for i := range s.scores {
		s.scores[i] = nan
	}
	for i := range copyX {
		if s.x[i] != copyX[i] {
			t.Fatalf("poison yTile/scores mutated x at %d", i)
		}
	}
}
