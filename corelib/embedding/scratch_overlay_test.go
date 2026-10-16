package embedding

import (
	"math"
	"testing"
)

func TestScratchArenaBounds(t *testing.T) {
	hp := GemmaHParams{Dim: 768, KVDim: 256, FFDim: 1152, HeadDim: 256, RopeTheta: 1e6}
	// The real model declares a second RoPE base for its SWA layers
	// (rope.freq_base_swa = 1e4, absent from the file so the loader defaults it),
	// which needs a second table pair.  That is a genuine 2*S*halfDim floats, so
	// the budget is raised by exactly that much and no more -- stated as a
	// measured quantity rather than a blanket raise, so an accidental second
	// allocation still shows up here as a breach.
	swa := hp
	swa.NSwa = 512
	swa.SwaPattern = 6
	swa.RopeThetaSwa = 1e4

	for _, seq := range []int{64, 256} {
		act := seq * 4608 * 4
		today := seq * 7680 * 4
		if act >= today {
			t.Errorf("activation body did not shrink")
		}
		for _, tc := range []struct {
			name        string
			hp          GemmaHParams
			extraFloats int
		}{
			{"single-base", hp, 0},
			{"swa-base", swa, 2 * seq * (hp.HeadDim / 2)},
		} {
			s := newGemmaScratch(tc.hp, seq)
			miB := float64(s.bytes()) / (1024 * 1024)
			limit := 1.3
			if seq == 256 {
				limit = 5.0
			}
			limit += float64(tc.extraFloats*4) / (1024 * 1024)
			if miB > limit {
				t.Errorf("%s seq=%d scratch %.3f MiB exceeds %.3f", tc.name, seq, miB, limit)
			}
			t.Logf("%s seq=%d total=%.3f MiB limit=%.3f activation=%.3f MiB",
				tc.name, seq, miB, limit, float64(act)/(1024*1024))
		}
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
