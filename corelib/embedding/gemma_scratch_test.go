package embedding

import "testing"

func officialGemmaHP() GemmaHParams {
	return GemmaHParams{Dim: 768, KVDim: 256, HeadDim: 256, FFDim: 1152}
}

func TestScratchArenaOfficialSize(t *testing.T) {
	hp := officialGemmaHP()
	for _, seq := range []int{1, 16, 17, 64, 256} {
		S := scratchBucket(seq)
		got := scratchArenaFloats(hp, S)
		want := S*4865 + 1536 + 6144
		if got != want {
			t.Fatalf("seq=%d bucket=%d arena=%d want %d", seq, S, got, want)
		}
		s := newGemmaScratch(hp, seq)
		if len(s.arena) != want || s.seqCap != S || len(s.x) != S*hp.Dim || len(s.ffDown) != S*hp.Dim {
			t.Fatalf("official bind seq=%d cap=%d arena=%d x=%d", seq, s.seqCap, len(s.arena), len(s.x))
		}
		if len(s.ffDown) == 0 || &s.ffDown[0] != &s.projOut[0] {
			t.Fatal("projOut must alias ffDown")
		}
	}
}

func TestScratchArenaOddHParamsDoesNotPanic(t *testing.T) {
	hp := GemmaHParams{Dim: 1024, KVDim: 512, HeadDim: 256, FFDim: 2048}
	s := newGemmaScratch(hp, 17)
	if s.seqCap != 64 || len(s.x) != 64*1024 || len(s.yTile) != yTileRows*1024 {
		t.Fatalf("odd-hp scratch = seqCap=%d x=%d yTile=%d", s.seqCap, len(s.x), len(s.yTile))
	}
	for i := range s.x {
		s.x[i] = 1
	}
	for i := range s.normed {
		s.normed[i] = 2
	}
	for i := range s.projOut {
		s.projOut[i] = 3
	}
	if s.x[0] != 1 || s.normed[0] != 2 || s.projOut[0] != 3 {
		t.Fatal("x/normed/projOut must not alias")
	}
}
func TestScratchPoolDoesNotCrossHParams(t *testing.T) {
	a := &GemmaEmbedder{hp: officialGemmaHP()}
	b := &GemmaEmbedder{hp: GemmaHParams{Dim: 1024, KVDim: 512, HeadDim: 256, FFDim: 2048}}
	sa := a.getScratchFromPool(17)
	a.putScratchToPool(sa)
	sb := b.getScratchFromPool(17)
	if len(sb.x) != 64*1024 {
		t.Fatalf("pooled scratch crossed models: x=%d", len(sb.x))
	}
}