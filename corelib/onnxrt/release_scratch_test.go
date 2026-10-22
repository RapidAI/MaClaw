package onnxrt

import "testing"

func TestReleaseScratch(t *testing.T) {
	putScratch(make([]float32, scratchBigMin))
	ReleaseScratch()
	scratchBig.Lock()
	n, e := len(scratchBig.bufs), scratchBig.elems
	scratchBig.Unlock()
	if n != 0 || e != 0 {
		t.Fatalf("bufs=%d elems=%d", n, e)
	}
}

func TestReleaseScratchDropsPooledArenas(t *testing.T) {
	a := acquireArena()
	a.put(make([]float32, 1024))
	releaseArena(a)
	ReleaseScratch()
	arenaPool.Lock()
	n := len(arenaPool.list)
	arenaPool.Unlock()
	if n != 0 {
		t.Fatalf("%d arenas still pooled", n)
	}
}
