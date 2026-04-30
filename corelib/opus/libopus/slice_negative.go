package libopus

import "unsafe"

// sliceFromNegF32 returns a slice that starts `offset` elements before the
// current data pointer of s. Works even when len(s)==0.
// This replicates C pointer arithmetic: ptr - offset → ptr[-offset:]
func sliceFromNegF32(s []float32, offset int) []float32 {
	type sliceHeader struct {
		Data unsafe.Pointer
		Len  int
		Cap  int
	}
	h := (*sliceHeader)(unsafe.Pointer(&s))
	return unsafe.Slice(
		(*float32)(unsafe.Add(h.Data, -int(unsafe.Sizeof(float32(0)))*offset)),
		len(s)+offset,
	)
}
