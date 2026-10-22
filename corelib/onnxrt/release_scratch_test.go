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
