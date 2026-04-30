package silk

import "unsafe"

// sliceFromNeg returns a slice that starts `offset` elements before s[0].
// The caller MUST guarantee that the memory at s[0]-offset is valid
// (i.e., s was derived from a larger allocation with at least `offset`
// elements before s[0]).
//
// This replicates C pointer arithmetic: ptr - offset → ptr[-offset:]
func sliceFromNeg(s []float32, offset int) []float32 {
	return unsafe.Slice(
		(*float32)(unsafe.Add(unsafe.Pointer(&s[0]), -int(unsafe.Sizeof(float32(0)))*offset)),
		len(s)+offset,
	)
}

// elemFromNeg returns the element at s[-offset], i.e. `offset` elements
// before s[0]. The caller MUST guarantee valid backing memory.
func elemFromNeg(s []float32, offset int) float32 {
	return *(*float32)(unsafe.Add(unsafe.Pointer(&s[0]), -int(unsafe.Sizeof(float32(0)))*offset))
}

// sliceFromNegInt8 returns a slice that starts `offset` elements before the
// current data pointer of s. Works even when len(s)==0 (pointer past end of
// backing array), as long as there are `offset` valid elements before it.
func sliceFromNegInt8(s []int8, offset int) []int8 {
	// Get the data pointer from the slice header, works even for empty slices.
	type sliceHeader struct {
		Data unsafe.Pointer
		Len  int
		Cap  int
	}
	h := (*sliceHeader)(unsafe.Pointer(&s))
	return unsafe.Slice(
		(*int8)(unsafe.Add(h.Data, -int(unsafe.Sizeof(int8(0)))*offset)),
		len(s)+offset,
	)
}

// sliceFromNegInt16 returns a slice that starts `offset` elements before the
// current data pointer of s. Works even when len(s)==0.
func sliceFromNegInt16(s []int16, offset int) []int16 {
	type sliceHeader struct {
		Data unsafe.Pointer
		Len  int
		Cap  int
	}
	h := (*sliceHeader)(unsafe.Pointer(&s))
	return unsafe.Slice(
		(*int16)(unsafe.Add(h.Data, -int(unsafe.Sizeof(int16(0)))*offset)),
		len(s)+offset,
	)
}

// sliceFromNegInt32 returns a slice that starts `offset` elements before the
// current data pointer of s. Works even when len(s)==0.
func sliceFromNegInt32(s []int32, offset int) []int32 {
	type sliceHeader struct {
		Data unsafe.Pointer
		Len  int
		Cap  int
	}
	h := (*sliceHeader)(unsafe.Pointer(&s))
	return unsafe.Slice(
		(*int32)(unsafe.Add(h.Data, -int(unsafe.Sizeof(int32(0)))*offset)),
		len(s)+offset,
	)
}
