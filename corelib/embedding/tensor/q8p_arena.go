package tensor

// Q8PArena owns the memory for a model's Q8P packed weights. It is mapped
// outside the Go heap where the OS allows it (anonymous mmap / VirtualAlloc):
// hundreds of MB of long-lived weights on the heap would double the GC heap
// goal (GOGC=100) and let short-lived inference garbage grow RSS by about the
// same amount under concurrency. Falls back to a heap slice elsewhere.
type Q8PArena struct {
	buf  []byte
	off  int
	free func()
}

// Q8PPackedBytes is the arena space PackQ8PInto needs for an [N×K] matrix.
func Q8PPackedBytes(N, K int) int { return N*K + N*(K/32)*4 + 2*64 }

// NewQ8PArena reserves n bytes.
func NewQ8PArena(n int) *Q8PArena {
	if b, free := allocOffHeap(n); b != nil {
		return &Q8PArena{buf: b, free: free}
	}
	return &Q8PArena{buf: make([]byte, n)}
}

func (a *Q8PArena) alloc(n int) []byte {
	a.off = (a.off + 63) &^ 63
	if a.off+n > len(a.buf) {
		return nil
	}
	b := a.buf[a.off : a.off+n : a.off+n]
	a.off += n
	return b
}

// Free releases the arena. Every Q8Packed carved from it becomes invalid;
// callers must guarantee no kernel is still reading it.
func (a *Q8PArena) Free() {
	if a == nil {
		return
	}
	if a.free != nil {
		a.free()
		a.free = nil
	}
	a.buf = nil
}

// PackQ8PInto is PackQ8P with storage carved from arena (nil arena → heap).
func PackQ8PInto(t *Q8Tensor, arena *Q8PArena) *Q8Packed {
	if arena == nil {
		return PackQ8P(t)
	}
	N, K := t.Rows, t.Cols
	if !Q8PSupported(N, K) {
		return nil
	}
	qb := arena.alloc(N * K)
	sbb := arena.alloc(N * (K / 32) * 4)
	if qb == nil || sbb == nil {
		return PackQ8P(t)
	}
	return packQ8PTo(t, qb, bytesAsF32(sbb))
}
