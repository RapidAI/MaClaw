package libopus

import "unsafe"

const MAXFACTORS = 8

type kiss_fft_cpx struct {
	R float32
	I float32
}
type kiss_twiddle_cpx struct {
	R float32
	I float32
}
type arch_fft_state struct {
	Is_supported int
	Priv         unsafe.Pointer
}
type kiss_fft_state struct {
	Nfft     int
	Scale    opus_val16
	Shift    int
	Factors  [16]int16
	Bitrev   *int16
	Twiddles *kiss_twiddle_cpx
	Arch_fft *arch_fft_state
}

func kf_bfly2(Fout *kiss_fft_cpx, m int, N int) {
	var (
		Fout2 *kiss_fft_cpx
		i     int
	)
	_ = m
	{
		var tw opus_val16
		tw = opus_val16(0.7071067812)
		for i = 0; i < N; i++ {
			var t kiss_fft_cpx
			Fout2 = (*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*4))
			t = *(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout2), unsafe.Sizeof(kiss_fft_cpx{})*0))
			for {
				(*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout2), unsafe.Sizeof(kiss_fft_cpx{})*0))).R = (*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*0))).R - t.R
				(*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout2), unsafe.Sizeof(kiss_fft_cpx{})*0))).I = (*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*0))).I - t.I
				if true {
					break
				}
			}
			for {
				(*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*0))).R += t.R
				(*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*0))).I += t.I
				if true {
					break
				}
			}
			t.R = ((*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout2), unsafe.Sizeof(kiss_fft_cpx{})*1))).R + (*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout2), unsafe.Sizeof(kiss_fft_cpx{})*1))).I) * float32(tw)
			t.I = ((*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout2), unsafe.Sizeof(kiss_fft_cpx{})*1))).I - (*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout2), unsafe.Sizeof(kiss_fft_cpx{})*1))).R) * float32(tw)
			for {
				(*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout2), unsafe.Sizeof(kiss_fft_cpx{})*1))).R = (*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*1))).R - t.R
				(*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout2), unsafe.Sizeof(kiss_fft_cpx{})*1))).I = (*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*1))).I - t.I
				if true {
					break
				}
			}
			for {
				(*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*1))).R += t.R
				(*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*1))).I += t.I
				if true {
					break
				}
			}
			t.R = (*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout2), unsafe.Sizeof(kiss_fft_cpx{})*2))).I
			t.I = -(*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout2), unsafe.Sizeof(kiss_fft_cpx{})*2))).R
			for {
				(*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout2), unsafe.Sizeof(kiss_fft_cpx{})*2))).R = (*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*2))).R - t.R
				(*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout2), unsafe.Sizeof(kiss_fft_cpx{})*2))).I = (*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*2))).I - t.I
				if true {
					break
				}
			}
			for {
				(*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*2))).R += t.R
				(*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*2))).I += t.I
				if true {
					break
				}
			}
			t.R = ((*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout2), unsafe.Sizeof(kiss_fft_cpx{})*3))).I - (*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout2), unsafe.Sizeof(kiss_fft_cpx{})*3))).R) * float32(tw)
			t.I = (-((*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout2), unsafe.Sizeof(kiss_fft_cpx{})*3))).I + (*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout2), unsafe.Sizeof(kiss_fft_cpx{})*3))).R)) * float32(tw)
			for {
				(*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout2), unsafe.Sizeof(kiss_fft_cpx{})*3))).R = (*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*3))).R - t.R
				(*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout2), unsafe.Sizeof(kiss_fft_cpx{})*3))).I = (*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*3))).I - t.I
				if true {
					break
				}
			}
			for {
				(*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*3))).R += t.R
				(*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*3))).I += t.I
				if true {
					break
				}
			}
			Fout = (*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*8))
		}
	}
}
func kf_bfly4(Fout *kiss_fft_cpx, fstride uint64, st *kiss_fft_state, m int, N int, mm int) {
	var i int
	if m == 1 {
		for i = 0; i < N; i++ {
			var (
				scratch0 kiss_fft_cpx
				scratch1 kiss_fft_cpx
			)
			for {
				scratch0.R = (*Fout).R - (*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*2))).R
				scratch0.I = (*Fout).I - (*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*2))).I
				if true {
					break
				}
			}
			for {
				(*Fout).R += (*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*2))).R
				(*Fout).I += (*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*2))).I
				if true {
					break
				}
			}
			for {
				scratch1.R = (*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*1))).R + (*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*3))).R
				scratch1.I = (*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*1))).I + (*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*3))).I
				if true {
					break
				}
			}
			for {
				(*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*2))).R = (*Fout).R - scratch1.R
				(*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*2))).I = (*Fout).I - scratch1.I
				if true {
					break
				}
			}
			for {
				(*Fout).R += scratch1.R
				(*Fout).I += scratch1.I
				if true {
					break
				}
			}
			for {
				scratch1.R = (*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*1))).R - (*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*3))).R
				scratch1.I = (*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*1))).I - (*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*3))).I
				if true {
					break
				}
			}
			(*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*1))).R = scratch0.R + scratch1.I
			(*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*1))).I = scratch0.I - scratch1.R
			(*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*3))).R = scratch0.R - scratch1.I
			(*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*3))).I = scratch0.I + scratch1.R
			Fout = (*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*4))
		}
	} else {
		var (
			j        int
			scratch  [6]kiss_fft_cpx
			tw1      *kiss_twiddle_cpx
			tw2      *kiss_twiddle_cpx
			tw3      *kiss_twiddle_cpx
			m2       int           = m * 2
			m3       int           = m * 3
			Fout_beg *kiss_fft_cpx = Fout
		)
		for i = 0; i < N; i++ {
			Fout = (*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout_beg), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(i*mm)))
			tw3 = func() *kiss_twiddle_cpx {
				tw2 = func() *kiss_twiddle_cpx {
					tw1 = st.Twiddles
					return tw1
				}()
				return tw2
			}()
			for j = 0; j < m; j++ {
				for {
					(scratch[0]).R = (*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(m)))).R*(*tw1).R - (*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(m)))).I*(*tw1).I
					(scratch[0]).I = (*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(m)))).R*(*tw1).I + (*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(m)))).I*(*tw1).R
					if true {
						break
					}
				}
				for {
					(scratch[1]).R = (*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(m2)))).R*(*tw2).R - (*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(m2)))).I*(*tw2).I
					(scratch[1]).I = (*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(m2)))).R*(*tw2).I + (*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(m2)))).I*(*tw2).R
					if true {
						break
					}
				}
				for {
					(scratch[2]).R = (*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(m3)))).R*(*tw3).R - (*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(m3)))).I*(*tw3).I
					(scratch[2]).I = (*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(m3)))).R*(*tw3).I + (*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(m3)))).I*(*tw3).R
					if true {
						break
					}
				}
				for {
					(scratch[5]).R = (*Fout).R - (scratch[1]).R
					(scratch[5]).I = (*Fout).I - (scratch[1]).I
					if true {
						break
					}
				}
				for {
					(*Fout).R += (scratch[1]).R
					(*Fout).I += (scratch[1]).I
					if true {
						break
					}
				}
				for {
					(scratch[3]).R = (scratch[0]).R + (scratch[2]).R
					(scratch[3]).I = (scratch[0]).I + (scratch[2]).I
					if true {
						break
					}
				}
				for {
					(scratch[4]).R = (scratch[0]).R - (scratch[2]).R
					(scratch[4]).I = (scratch[0]).I - (scratch[2]).I
					if true {
						break
					}
				}
				for {
					(*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(m2)))).R = (*Fout).R - (scratch[3]).R
					(*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(m2)))).I = (*Fout).I - (scratch[3]).I
					if true {
						break
					}
				}
				tw1 = (*kiss_twiddle_cpx)(unsafe.Add(unsafe.Pointer(tw1), unsafe.Sizeof(kiss_twiddle_cpx{})*uintptr(fstride)))
				tw2 = (*kiss_twiddle_cpx)(unsafe.Add(unsafe.Pointer(tw2), unsafe.Sizeof(kiss_twiddle_cpx{})*uintptr(fstride*2)))
				tw3 = (*kiss_twiddle_cpx)(unsafe.Add(unsafe.Pointer(tw3), unsafe.Sizeof(kiss_twiddle_cpx{})*uintptr(fstride*3)))
				for {
					(*Fout).R += (scratch[3]).R
					(*Fout).I += (scratch[3]).I
					if true {
						break
					}
				}
				(*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(m)))).R = scratch[5].R + scratch[4].I
				(*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(m)))).I = scratch[5].I - scratch[4].R
				(*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(m3)))).R = scratch[5].R - scratch[4].I
				(*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(m3)))).I = scratch[5].I + scratch[4].R
				Fout = (*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*1))
			}
		}
	}
}
func kf_bfly3(Fout *kiss_fft_cpx, fstride uint64, st *kiss_fft_state, m int, N int, mm int) {
	var (
		i        int
		k        uint64
		m2       uint64 = uint64(m * 2)
		tw1      *kiss_twiddle_cpx
		tw2      *kiss_twiddle_cpx
		scratch  [5]kiss_fft_cpx
		epi3     kiss_twiddle_cpx
		Fout_beg *kiss_fft_cpx = Fout
	)
	epi3 = *(*kiss_twiddle_cpx)(unsafe.Add(unsafe.Pointer(st.Twiddles), unsafe.Sizeof(kiss_twiddle_cpx{})*uintptr(fstride*uint64(m))))
	for i = 0; i < N; i++ {
		Fout = (*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout_beg), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(i*mm)))
		tw1 = func() *kiss_twiddle_cpx {
			tw2 = st.Twiddles
			return tw2
		}()
		k = uint64(m)
		for {
			for {
				(scratch[1]).R = (*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(m)))).R*(*tw1).R - (*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(m)))).I*(*tw1).I
				(scratch[1]).I = (*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(m)))).R*(*tw1).I + (*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(m)))).I*(*tw1).R
				if true {
					break
				}
			}
			for {
				(scratch[2]).R = (*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(m2)))).R*(*tw2).R - (*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(m2)))).I*(*tw2).I
				(scratch[2]).I = (*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(m2)))).R*(*tw2).I + (*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(m2)))).I*(*tw2).R
				if true {
					break
				}
			}
			for {
				(scratch[3]).R = (scratch[1]).R + (scratch[2]).R
				(scratch[3]).I = (scratch[1]).I + (scratch[2]).I
				if true {
					break
				}
			}
			for {
				(scratch[0]).R = (scratch[1]).R - (scratch[2]).R
				(scratch[0]).I = (scratch[1]).I - (scratch[2]).I
				if true {
					break
				}
			}
			tw1 = (*kiss_twiddle_cpx)(unsafe.Add(unsafe.Pointer(tw1), unsafe.Sizeof(kiss_twiddle_cpx{})*uintptr(fstride)))
			tw2 = (*kiss_twiddle_cpx)(unsafe.Add(unsafe.Pointer(tw2), unsafe.Sizeof(kiss_twiddle_cpx{})*uintptr(fstride*2)))
			(*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(m)))).R = Fout.R - scratch[3].R*0.5
			(*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(m)))).I = Fout.I - scratch[3].I*0.5
			for {
				(scratch[0]).R *= epi3.I
				(scratch[0]).I *= epi3.I
				if true {
					break
				}
			}
			for {
				(*Fout).R += (scratch[3]).R
				(*Fout).I += (scratch[3]).I
				if true {
					break
				}
			}
			(*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(m2)))).R = (*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(m)))).R + scratch[0].I
			(*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(m2)))).I = (*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(m)))).I - scratch[0].R
			(*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(m)))).R = (*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(m)))).R - scratch[0].I
			(*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(m)))).I = (*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(m)))).I + scratch[0].R
			Fout = (*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout), unsafe.Sizeof(kiss_fft_cpx{})*1))
			if func() uint64 {
				p := &k
				*p--
				return *p
			}() == 0 {
				break
			}
		}
	}
}
func kf_bfly5(Fout *kiss_fft_cpx, fstride uint64, st *kiss_fft_state, m int, N int, mm int) {
	var (
		Fout0    *kiss_fft_cpx
		Fout1    *kiss_fft_cpx
		Fout2    *kiss_fft_cpx
		Fout3    *kiss_fft_cpx
		Fout4    *kiss_fft_cpx
		i        int
		u        int
		scratch  [13]kiss_fft_cpx
		tw       *kiss_twiddle_cpx
		ya       kiss_twiddle_cpx
		yb       kiss_twiddle_cpx
		Fout_beg *kiss_fft_cpx = Fout
	)
	ya = *(*kiss_twiddle_cpx)(unsafe.Add(unsafe.Pointer(st.Twiddles), unsafe.Sizeof(kiss_twiddle_cpx{})*uintptr(fstride*uint64(m))))
	yb = *(*kiss_twiddle_cpx)(unsafe.Add(unsafe.Pointer(st.Twiddles), unsafe.Sizeof(kiss_twiddle_cpx{})*uintptr(fstride*2*uint64(m))))
	tw = st.Twiddles
	for i = 0; i < N; i++ {
		Fout = (*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout_beg), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(i*mm)))
		Fout0 = Fout
		Fout1 = (*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout0), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(m)))
		Fout2 = (*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout0), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(m*2)))
		Fout3 = (*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout0), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(m*3)))
		Fout4 = (*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout0), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(m*4)))
		for u = 0; u < m; u++ {
			scratch[0] = *Fout0
			for {
				(scratch[1]).R = (*Fout1).R*(*(*kiss_twiddle_cpx)(unsafe.Add(unsafe.Pointer(tw), unsafe.Sizeof(kiss_twiddle_cpx{})*uintptr(u*int(fstride))))).R - (*Fout1).I*(*(*kiss_twiddle_cpx)(unsafe.Add(unsafe.Pointer(tw), unsafe.Sizeof(kiss_twiddle_cpx{})*uintptr(u*int(fstride))))).I
				(scratch[1]).I = (*Fout1).R*(*(*kiss_twiddle_cpx)(unsafe.Add(unsafe.Pointer(tw), unsafe.Sizeof(kiss_twiddle_cpx{})*uintptr(u*int(fstride))))).I + (*Fout1).I*(*(*kiss_twiddle_cpx)(unsafe.Add(unsafe.Pointer(tw), unsafe.Sizeof(kiss_twiddle_cpx{})*uintptr(u*int(fstride))))).R
				if true {
					break
				}
			}
			for {
				(scratch[2]).R = (*Fout2).R*(*(*kiss_twiddle_cpx)(unsafe.Add(unsafe.Pointer(tw), unsafe.Sizeof(kiss_twiddle_cpx{})*uintptr(u*2*int(fstride))))).R - (*Fout2).I*(*(*kiss_twiddle_cpx)(unsafe.Add(unsafe.Pointer(tw), unsafe.Sizeof(kiss_twiddle_cpx{})*uintptr(u*2*int(fstride))))).I
				(scratch[2]).I = (*Fout2).R*(*(*kiss_twiddle_cpx)(unsafe.Add(unsafe.Pointer(tw), unsafe.Sizeof(kiss_twiddle_cpx{})*uintptr(u*2*int(fstride))))).I + (*Fout2).I*(*(*kiss_twiddle_cpx)(unsafe.Add(unsafe.Pointer(tw), unsafe.Sizeof(kiss_twiddle_cpx{})*uintptr(u*2*int(fstride))))).R
				if true {
					break
				}
			}
			for {
				(scratch[3]).R = (*Fout3).R*(*(*kiss_twiddle_cpx)(unsafe.Add(unsafe.Pointer(tw), unsafe.Sizeof(kiss_twiddle_cpx{})*uintptr(u*3*int(fstride))))).R - (*Fout3).I*(*(*kiss_twiddle_cpx)(unsafe.Add(unsafe.Pointer(tw), unsafe.Sizeof(kiss_twiddle_cpx{})*uintptr(u*3*int(fstride))))).I
				(scratch[3]).I = (*Fout3).R*(*(*kiss_twiddle_cpx)(unsafe.Add(unsafe.Pointer(tw), unsafe.Sizeof(kiss_twiddle_cpx{})*uintptr(u*3*int(fstride))))).I + (*Fout3).I*(*(*kiss_twiddle_cpx)(unsafe.Add(unsafe.Pointer(tw), unsafe.Sizeof(kiss_twiddle_cpx{})*uintptr(u*3*int(fstride))))).R
				if true {
					break
				}
			}
			for {
				(scratch[4]).R = (*Fout4).R*(*(*kiss_twiddle_cpx)(unsafe.Add(unsafe.Pointer(tw), unsafe.Sizeof(kiss_twiddle_cpx{})*uintptr(u*4*int(fstride))))).R - (*Fout4).I*(*(*kiss_twiddle_cpx)(unsafe.Add(unsafe.Pointer(tw), unsafe.Sizeof(kiss_twiddle_cpx{})*uintptr(u*4*int(fstride))))).I
				(scratch[4]).I = (*Fout4).R*(*(*kiss_twiddle_cpx)(unsafe.Add(unsafe.Pointer(tw), unsafe.Sizeof(kiss_twiddle_cpx{})*uintptr(u*4*int(fstride))))).I + (*Fout4).I*(*(*kiss_twiddle_cpx)(unsafe.Add(unsafe.Pointer(tw), unsafe.Sizeof(kiss_twiddle_cpx{})*uintptr(u*4*int(fstride))))).R
				if true {
					break
				}
			}
			for {
				(scratch[7]).R = (scratch[1]).R + (scratch[4]).R
				(scratch[7]).I = (scratch[1]).I + (scratch[4]).I
				if true {
					break
				}
			}
			for {
				(scratch[10]).R = (scratch[1]).R - (scratch[4]).R
				(scratch[10]).I = (scratch[1]).I - (scratch[4]).I
				if true {
					break
				}
			}
			for {
				(scratch[8]).R = (scratch[2]).R + (scratch[3]).R
				(scratch[8]).I = (scratch[2]).I + (scratch[3]).I
				if true {
					break
				}
			}
			for {
				(scratch[9]).R = (scratch[2]).R - (scratch[3]).R
				(scratch[9]).I = (scratch[2]).I - (scratch[3]).I
				if true {
					break
				}
			}
			Fout0.R = Fout0.R + (scratch[7].R + scratch[8].R)
			Fout0.I = Fout0.I + (scratch[7].I + scratch[8].I)
			scratch[5].R = scratch[0].R + ((scratch[7].R * ya.R) + scratch[8].R*yb.R)
			scratch[5].I = scratch[0].I + ((scratch[7].I * ya.R) + scratch[8].I*yb.R)
			scratch[6].R = (scratch[10].I * ya.I) + scratch[9].I*yb.I
			scratch[6].I = -((scratch[10].R * ya.I) + scratch[9].R*yb.I)
			for {
				(*Fout1).R = (scratch[5]).R - (scratch[6]).R
				(*Fout1).I = (scratch[5]).I - (scratch[6]).I
				if true {
					break
				}
			}
			for {
				(*Fout4).R = (scratch[5]).R + (scratch[6]).R
				(*Fout4).I = (scratch[5]).I + (scratch[6]).I
				if true {
					break
				}
			}
			scratch[11].R = scratch[0].R + ((scratch[7].R * yb.R) + scratch[8].R*ya.R)
			scratch[11].I = scratch[0].I + ((scratch[7].I * yb.R) + scratch[8].I*ya.R)
			scratch[12].R = (scratch[9].I * ya.I) - scratch[10].I*yb.I
			scratch[12].I = (scratch[10].R * yb.I) - scratch[9].R*ya.I
			for {
				(*Fout2).R = (scratch[11]).R + (scratch[12]).R
				(*Fout2).I = (scratch[11]).I + (scratch[12]).I
				if true {
					break
				}
			}
			for {
				(*Fout3).R = (scratch[11]).R - (scratch[12]).R
				(*Fout3).I = (scratch[11]).I - (scratch[12]).I
				if true {
					break
				}
			}
			Fout0 = (*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout0), unsafe.Sizeof(kiss_fft_cpx{})*1))
			Fout1 = (*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout1), unsafe.Sizeof(kiss_fft_cpx{})*1))
			Fout2 = (*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout2), unsafe.Sizeof(kiss_fft_cpx{})*1))
			Fout3 = (*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout3), unsafe.Sizeof(kiss_fft_cpx{})*1))
			Fout4 = (*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(Fout4), unsafe.Sizeof(kiss_fft_cpx{})*1))
		}
	}
}
func opus_fft_impl(st *kiss_fft_state, fout *kiss_fft_cpx) {
	var (
		m2      int
		m       int
		p       int
		L       int
		fstride [8]int
		i       int
		shift   int
	)
	if st.Shift > 0 {
		shift = st.Shift
	} else {
		shift = 0
	}
	fstride[0] = 1
	L = 0
	for {
		p = int(st.Factors[L*2])
		m = int(st.Factors[L*2+1])
		fstride[L+1] = fstride[L] * p
		L++
		if m == 1 {
			break
		}
	}
	m = int(st.Factors[L*2-1])
	for i = L - 1; i >= 0; i-- {
		if i != 0 {
			m2 = int(st.Factors[i*2-1])
		} else {
			m2 = 1
		}
		switch st.Factors[i*2] {
		case 2:
			kf_bfly2(fout, m, fstride[i])
		case 4:
			kf_bfly4(fout, uint64(fstride[i]<<shift), st, m, fstride[i], m2)
		case 3:
			kf_bfly3(fout, uint64(fstride[i]<<shift), st, m, fstride[i], m2)
		case 5:
			kf_bfly5(fout, uint64(fstride[i]<<shift), st, m, fstride[i], m2)
		}
		m = m2
	}
}
func opus_fft_c(st *kiss_fft_state, fin *kiss_fft_cpx, fout *kiss_fft_cpx) {
	var (
		i     int
		scale opus_val16
	)
	scale = st.Scale
	for i = 0; i < st.Nfft; i++ {
		var x kiss_fft_cpx = *(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(fin), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(i)))
		(*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(fout), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(*(*int16)(unsafe.Add(unsafe.Pointer(st.Bitrev), unsafe.Sizeof(int16(0))*uintptr(i))))))).R = float32(scale * opus_val16(x.R))
		(*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(fout), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(*(*int16)(unsafe.Add(unsafe.Pointer(st.Bitrev), unsafe.Sizeof(int16(0))*uintptr(i))))))).I = float32(scale * opus_val16(x.I))
	}
	opus_fft_impl(st, fout)
}
func opus_ifft_c(st *kiss_fft_state, fin *kiss_fft_cpx, fout *kiss_fft_cpx) {
	var i int
	for i = 0; i < st.Nfft; i++ {
		*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(fout), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(*(*int16)(unsafe.Add(unsafe.Pointer(st.Bitrev), unsafe.Sizeof(int16(0))*uintptr(i)))))) = *(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(fin), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(i)))
	}
	for i = 0; i < st.Nfft; i++ {
		(*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(fout), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(i)))).I = -(*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(fout), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(i)))).I
	}
	opus_fft_impl(st, fout)
	for i = 0; i < st.Nfft; i++ {
		(*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(fout), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(i)))).I = -(*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(fout), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(i)))).I
	}
}
