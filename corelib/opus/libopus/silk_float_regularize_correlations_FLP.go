package libopus

import "unsafe"

func silk_regularize_correlations_FLP(XX *float32, xx *float32, noise float32, D int) {
	var i int
	for i = 0; i < D; i++ {
		*((*float32)(unsafe.Add(unsafe.Pointer((*float32)(unsafe.Add(unsafe.Pointer(XX), unsafe.Sizeof(float32(0))*0))), unsafe.Sizeof(float32(0))*uintptr(i*D+i)))) += noise
	}
	*(*float32)(unsafe.Add(unsafe.Pointer(xx), unsafe.Sizeof(float32(0))*0)) += noise
}
