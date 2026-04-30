package libopus

import (
	"github.com/RapidAI/CodeClaw/corelib/opus/internal/libc"
	"unsafe"
)

type mdct_lookup struct {
	N        int
	Maxshift int
	Kfft     [4]*kiss_fft_state
	Trig     *float32
}

func clt_mdct_forward_c(l *mdct_lookup, in *float32, out *float32, window *opus_val16, overlap int, shift int, stride int, arch int) {
	var (
		i     int
		N     int
		N2    int
		N4    int
		f     *float32
		f2    *kiss_fft_cpx
		st    *kiss_fft_state = l.Kfft[shift]
		trig  *float32
		scale opus_val16
	)
	_ = arch
	scale = st.Scale
	N = l.N
	trig = l.Trig
	for i = 0; i < shift; i++ {
		N >>= 1
		trig = (*float32)(unsafe.Add(unsafe.Pointer(trig), unsafe.Sizeof(float32(0))*uintptr(N)))
	}
	N2 = N >> 1
	N4 = N >> 2
	f = (*float32)(libc.Malloc(N2 * int(unsafe.Sizeof(float32(0)))))
	f2 = (*kiss_fft_cpx)(libc.Malloc(N4 * int(unsafe.Sizeof(kiss_fft_cpx{}))))
	{
		var (
			xp1 *float32    = (*float32)(unsafe.Add(unsafe.Pointer(in), unsafe.Sizeof(float32(0))*uintptr(overlap>>1)))
			xp2 *float32    = (*float32)(unsafe.Add(unsafe.Pointer((*float32)(unsafe.Add(unsafe.Pointer((*float32)(unsafe.Add(unsafe.Pointer(in), unsafe.Sizeof(float32(0))*uintptr(N2)))), -int(unsafe.Sizeof(float32(0))*1)))), unsafe.Sizeof(float32(0))*uintptr(overlap>>1)))
			yp  *float32    = f
			wp1 *opus_val16 = (*opus_val16)(unsafe.Add(unsafe.Pointer(window), unsafe.Sizeof(opus_val16(0))*uintptr(overlap>>1)))
			wp2 *opus_val16 = (*opus_val16)(unsafe.Add(unsafe.Pointer((*opus_val16)(unsafe.Add(unsafe.Pointer(window), unsafe.Sizeof(opus_val16(0))*uintptr(overlap>>1)))), -int(unsafe.Sizeof(opus_val16(0))*1)))
		)
		for i = 0; i < ((overlap + 3) >> 2); i++ {
			*func() *float32 {
				p := &yp
				x := *p
				*p = (*float32)(unsafe.Add(unsafe.Pointer(*p), unsafe.Sizeof(float32(0))*1))
				return x
			}() = float32(((*wp2) * opus_val16(*(*float32)(unsafe.Add(unsafe.Pointer(xp1), unsafe.Sizeof(float32(0))*uintptr(N2))))) + (*wp1)*opus_val16(*xp2))
			*func() *float32 {
				p := &yp
				x := *p
				*p = (*float32)(unsafe.Add(unsafe.Pointer(*p), unsafe.Sizeof(float32(0))*1))
				return x
			}() = float32(((*wp1) * opus_val16(*xp1)) - (*wp2)*opus_val16(*(*float32)(unsafe.Add(unsafe.Pointer(xp2), -int(unsafe.Sizeof(float32(0))*uintptr(N2))))))
			xp1 = (*float32)(unsafe.Add(unsafe.Pointer(xp1), unsafe.Sizeof(float32(0))*2))
			xp2 = (*float32)(unsafe.Add(unsafe.Pointer(xp2), -int(unsafe.Sizeof(float32(0))*2)))
			wp1 = (*opus_val16)(unsafe.Add(unsafe.Pointer(wp1), unsafe.Sizeof(opus_val16(0))*2))
			wp2 = (*opus_val16)(unsafe.Add(unsafe.Pointer(wp2), -int(unsafe.Sizeof(opus_val16(0))*2)))
		}
		wp1 = window
		wp2 = (*opus_val16)(unsafe.Add(unsafe.Pointer((*opus_val16)(unsafe.Add(unsafe.Pointer(window), unsafe.Sizeof(opus_val16(0))*uintptr(overlap)))), -int(unsafe.Sizeof(opus_val16(0))*1)))
		for ; i < N4-((overlap+3)>>2); i++ {
			*func() *float32 {
				p := &yp
				x := *p
				*p = (*float32)(unsafe.Add(unsafe.Pointer(*p), unsafe.Sizeof(float32(0))*1))
				return x
			}() = *xp2
			*func() *float32 {
				p := &yp
				x := *p
				*p = (*float32)(unsafe.Add(unsafe.Pointer(*p), unsafe.Sizeof(float32(0))*1))
				return x
			}() = *xp1
			xp1 = (*float32)(unsafe.Add(unsafe.Pointer(xp1), unsafe.Sizeof(float32(0))*2))
			xp2 = (*float32)(unsafe.Add(unsafe.Pointer(xp2), -int(unsafe.Sizeof(float32(0))*2)))
		}
		for ; i < N4; i++ {
			*func() *float32 {
				p := &yp
				x := *p
				*p = (*float32)(unsafe.Add(unsafe.Pointer(*p), unsafe.Sizeof(float32(0))*1))
				return x
			}() = float32(-((*wp1) * opus_val16(*(*float32)(unsafe.Add(unsafe.Pointer(xp1), -int(unsafe.Sizeof(float32(0))*uintptr(N2))))))) + float32((*wp2)*opus_val16(*xp2))
			*func() *float32 {
				p := &yp
				x := *p
				*p = (*float32)(unsafe.Add(unsafe.Pointer(*p), unsafe.Sizeof(float32(0))*1))
				return x
			}() = float32(((*wp2) * opus_val16(*xp1)) + (*wp1)*opus_val16(*(*float32)(unsafe.Add(unsafe.Pointer(xp2), unsafe.Sizeof(float32(0))*uintptr(N2)))))
			xp1 = (*float32)(unsafe.Add(unsafe.Pointer(xp1), unsafe.Sizeof(float32(0))*2))
			xp2 = (*float32)(unsafe.Add(unsafe.Pointer(xp2), -int(unsafe.Sizeof(float32(0))*2)))
			wp1 = (*opus_val16)(unsafe.Add(unsafe.Pointer(wp1), unsafe.Sizeof(opus_val16(0))*2))
			wp2 = (*opus_val16)(unsafe.Add(unsafe.Pointer(wp2), -int(unsafe.Sizeof(opus_val16(0))*2)))
		}
	}
	{
		var (
			yp *float32 = f
			t  *float32 = (*float32)(unsafe.Add(unsafe.Pointer(trig), unsafe.Sizeof(float32(0))*0))
		)
		for i = 0; i < N4; i++ {
			var (
				yc kiss_fft_cpx
				t0 float32
				t1 float32
				re float32
				im float32
				yr float32
				yi float32
			)
			t0 = *(*float32)(unsafe.Add(unsafe.Pointer(t), unsafe.Sizeof(float32(0))*uintptr(i)))
			t1 = *(*float32)(unsafe.Add(unsafe.Pointer(t), unsafe.Sizeof(float32(0))*uintptr(N4+i)))
			re = *func() *float32 {
				p := &yp
				x := *p
				*p = (*float32)(unsafe.Add(unsafe.Pointer(*p), unsafe.Sizeof(float32(0))*1))
				return x
			}()
			im = *func() *float32 {
				p := &yp
				x := *p
				*p = (*float32)(unsafe.Add(unsafe.Pointer(*p), unsafe.Sizeof(float32(0))*1))
				return x
			}()
			yr = (re * t0) - im*t1
			yi = (im * t0) + re*t1
			yc.R = yr
			yc.I = yi
			yc.R = float32(scale * opus_val16(yc.R))
			yc.I = float32(scale * opus_val16(yc.I))
			*(*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(f2), unsafe.Sizeof(kiss_fft_cpx{})*uintptr(*(*int16)(unsafe.Add(unsafe.Pointer(st.Bitrev), unsafe.Sizeof(int16(0))*uintptr(i)))))) = yc
		}
	}
	opus_fft_impl(st, f2)
	{
		var (
			fp  *kiss_fft_cpx = f2
			yp1 *float32      = out
			yp2 *float32      = (*float32)(unsafe.Add(unsafe.Pointer(out), unsafe.Sizeof(float32(0))*uintptr(stride*(N2-1))))
			t   *float32      = (*float32)(unsafe.Add(unsafe.Pointer(trig), unsafe.Sizeof(float32(0))*0))
		)
		for i = 0; i < N4; i++ {
			var (
				yr float32
				yi float32
			)
			yr = (fp.I * (*(*float32)(unsafe.Add(unsafe.Pointer(t), unsafe.Sizeof(float32(0))*uintptr(N4+i))))) - fp.R*(*(*float32)(unsafe.Add(unsafe.Pointer(t), unsafe.Sizeof(float32(0))*uintptr(i))))
			yi = (fp.R * (*(*float32)(unsafe.Add(unsafe.Pointer(t), unsafe.Sizeof(float32(0))*uintptr(N4+i))))) + fp.I*(*(*float32)(unsafe.Add(unsafe.Pointer(t), unsafe.Sizeof(float32(0))*uintptr(i))))
			*yp1 = yr
			*yp2 = yi
			fp = (*kiss_fft_cpx)(unsafe.Add(unsafe.Pointer(fp), unsafe.Sizeof(kiss_fft_cpx{})*1))
			yp1 = (*float32)(unsafe.Add(unsafe.Pointer(yp1), unsafe.Sizeof(float32(0))*uintptr(stride*2)))
			yp2 = (*float32)(unsafe.Add(unsafe.Pointer(yp2), -int(unsafe.Sizeof(float32(0))*uintptr(stride*2))))
		}
	}
}
func clt_mdct_backward_c(l *mdct_lookup, in *float32, out *float32, window *opus_val16, overlap int, shift int, stride int, arch int) {
	var (
		i    int
		N    int
		N2   int
		N4   int
		trig *float32
	)
	_ = arch
	N = l.N
	trig = l.Trig
	for i = 0; i < shift; i++ {
		N >>= 1
		trig = (*float32)(unsafe.Add(unsafe.Pointer(trig), unsafe.Sizeof(float32(0))*uintptr(N)))
	}
	N2 = N >> 1
	N4 = N >> 2
	{
		var (
			xp1    *float32 = in
			xp2    *float32 = (*float32)(unsafe.Add(unsafe.Pointer(in), unsafe.Sizeof(float32(0))*uintptr(stride*(N2-1))))
			yp     *float32 = (*float32)(unsafe.Add(unsafe.Pointer(out), unsafe.Sizeof(float32(0))*uintptr(overlap>>1)))
			t      *float32 = (*float32)(unsafe.Add(unsafe.Pointer(trig), unsafe.Sizeof(float32(0))*0))
			bitrev *int16   = l.Kfft[shift].Bitrev
		)
		for i = 0; i < N4; i++ {
			var (
				rev int
				yr  float32
				yi  float32
			)
			rev = int(*func() *int16 {
				p := &bitrev
				x := *p
				*p = (*int16)(unsafe.Add(unsafe.Pointer(*p), unsafe.Sizeof(int16(0))*1))
				return x
			}())
			yr = ((*xp2) * (*(*float32)(unsafe.Add(unsafe.Pointer(t), unsafe.Sizeof(float32(0))*uintptr(i))))) + (*xp1)*(*(*float32)(unsafe.Add(unsafe.Pointer(t), unsafe.Sizeof(float32(0))*uintptr(N4+i))))
			yi = ((*xp1) * (*(*float32)(unsafe.Add(unsafe.Pointer(t), unsafe.Sizeof(float32(0))*uintptr(i))))) - (*xp2)*(*(*float32)(unsafe.Add(unsafe.Pointer(t), unsafe.Sizeof(float32(0))*uintptr(N4+i))))
			*(*float32)(unsafe.Add(unsafe.Pointer(yp), unsafe.Sizeof(float32(0))*uintptr(rev*2+1))) = yr
			*(*float32)(unsafe.Add(unsafe.Pointer(yp), unsafe.Sizeof(float32(0))*uintptr(rev*2))) = yi
			xp1 = (*float32)(unsafe.Add(unsafe.Pointer(xp1), unsafe.Sizeof(float32(0))*uintptr(stride*2)))
			xp2 = (*float32)(unsafe.Add(unsafe.Pointer(xp2), -int(unsafe.Sizeof(float32(0))*uintptr(stride*2))))
		}
	}
	opus_fft_impl(l.Kfft[shift], (*kiss_fft_cpx)(unsafe.Pointer((*float32)(unsafe.Add(unsafe.Pointer(out), unsafe.Sizeof(float32(0))*uintptr(overlap>>1))))))
	{
		var (
			yp0 *float32 = (*float32)(unsafe.Add(unsafe.Pointer(out), unsafe.Sizeof(float32(0))*uintptr(overlap>>1)))
			yp1 *float32 = (*float32)(unsafe.Add(unsafe.Pointer((*float32)(unsafe.Add(unsafe.Pointer((*float32)(unsafe.Add(unsafe.Pointer(out), unsafe.Sizeof(float32(0))*uintptr(overlap>>1)))), unsafe.Sizeof(float32(0))*uintptr(N2)))), -int(unsafe.Sizeof(float32(0))*2)))
			t   *float32 = (*float32)(unsafe.Add(unsafe.Pointer(trig), unsafe.Sizeof(float32(0))*0))
		)
		for i = 0; i < (N4+1)>>1; i++ {
			var (
				re float32
				im float32
				yr float32
				yi float32
				t0 float32
				t1 float32
			)
			re = *(*float32)(unsafe.Add(unsafe.Pointer(yp0), unsafe.Sizeof(float32(0))*1))
			im = *(*float32)(unsafe.Add(unsafe.Pointer(yp0), unsafe.Sizeof(float32(0))*0))
			t0 = *(*float32)(unsafe.Add(unsafe.Pointer(t), unsafe.Sizeof(float32(0))*uintptr(i)))
			t1 = *(*float32)(unsafe.Add(unsafe.Pointer(t), unsafe.Sizeof(float32(0))*uintptr(N4+i)))
			yr = (re * t0) + im*t1
			yi = (re * t1) - im*t0
			re = *(*float32)(unsafe.Add(unsafe.Pointer(yp1), unsafe.Sizeof(float32(0))*1))
			im = *(*float32)(unsafe.Add(unsafe.Pointer(yp1), unsafe.Sizeof(float32(0))*0))
			*(*float32)(unsafe.Add(unsafe.Pointer(yp0), unsafe.Sizeof(float32(0))*0)) = yr
			*(*float32)(unsafe.Add(unsafe.Pointer(yp1), unsafe.Sizeof(float32(0))*1)) = yi
			t0 = *(*float32)(unsafe.Add(unsafe.Pointer(t), unsafe.Sizeof(float32(0))*uintptr(N4-i-1)))
			t1 = *(*float32)(unsafe.Add(unsafe.Pointer(t), unsafe.Sizeof(float32(0))*uintptr(N2-i-1)))
			yr = (re * t0) + im*t1
			yi = (re * t1) - im*t0
			*(*float32)(unsafe.Add(unsafe.Pointer(yp1), unsafe.Sizeof(float32(0))*0)) = yr
			*(*float32)(unsafe.Add(unsafe.Pointer(yp0), unsafe.Sizeof(float32(0))*1)) = yi
			yp0 = (*float32)(unsafe.Add(unsafe.Pointer(yp0), unsafe.Sizeof(float32(0))*2))
			yp1 = (*float32)(unsafe.Add(unsafe.Pointer(yp1), -int(unsafe.Sizeof(float32(0))*2)))
		}
	}
	{
		var (
			xp1 *float32    = (*float32)(unsafe.Add(unsafe.Pointer((*float32)(unsafe.Add(unsafe.Pointer(out), unsafe.Sizeof(float32(0))*uintptr(overlap)))), -int(unsafe.Sizeof(float32(0))*1)))
			yp1 *float32    = out
			wp1 *opus_val16 = window
			wp2 *opus_val16 = (*opus_val16)(unsafe.Add(unsafe.Pointer((*opus_val16)(unsafe.Add(unsafe.Pointer(window), unsafe.Sizeof(opus_val16(0))*uintptr(overlap)))), -int(unsafe.Sizeof(opus_val16(0))*1)))
		)
		for i = 0; i < overlap/2; i++ {
			var (
				x1 float32
				x2 float32
			)
			x1 = *xp1
			x2 = *yp1
			*func() *float32 {
				p := &yp1
				x := *p
				*p = (*float32)(unsafe.Add(unsafe.Pointer(*p), unsafe.Sizeof(float32(0))*1))
				return x
			}() = float32(((*wp2) * opus_val16(x2)) - (*wp1)*opus_val16(x1))
			*func() *float32 {
				p := &xp1
				x := *p
				*p = (*float32)(unsafe.Add(unsafe.Pointer(*p), -int(unsafe.Sizeof(float32(0))*1)))
				return x
			}() = float32(((*wp1) * opus_val16(x2)) + (*wp2)*opus_val16(x1))
			wp1 = (*opus_val16)(unsafe.Add(unsafe.Pointer(wp1), unsafe.Sizeof(opus_val16(0))*1))
			wp2 = (*opus_val16)(unsafe.Add(unsafe.Pointer(wp2), -int(unsafe.Sizeof(opus_val16(0))*1)))
		}
	}
}
