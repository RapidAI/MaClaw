package libopus

const celtPI = 3.141592653

func fast_atan2f(y float32, x float32) float32 {
	var (
		x2 float32
		y2 float32
	)
	x2 = x * x
	y2 = y * y
	if x2+y2 < 1e-18 {
		return 0
	}
	if x2 < y2 {
		var den float32 = (y2 + cB*x2) * (y2 + cC*x2)
		return -x*y*(y2+cA*x2)/den + float32(func() float64 {
			if y < 0 {
				return -(celtPI / 2)
			}
			return celtPI / 2
		}())
	} else {
		var den float32 = (x2 + cB*y2) * (x2 + cC*y2)
		return x*y*(x2+cA*y2)/den + float32(func() float64 {
			if y < 0 {
				return -(celtPI / 2)
			}
			return celtPI / 2
		}()) - float32(func() float64 {
			if x*y < 0 {
				return -(celtPI / 2)
			}
			return celtPI / 2
		}())
	}
}
func celt_maxabs16(x []opus_val16, len_ int) opus_val32 {
	var (
		i      int
		maxval opus_val16 = 0
		minval opus_val16 = 0
	)
	for i = 0; i < len_; i++ {
		if maxval > (x[i]) {
			maxval = maxval
		} else {
			maxval = x[i]
		}
		if minval < (x[i]) {
			minval = minval
		} else {
			minval = x[i]
		}
	}
	if maxval > (-minval) {
		return opus_val32(maxval)
	}
	return opus_val32(-minval)
}
func isqrt32(_val uint32) uint {
	var (
		b      uint
		g      uint
		bshift int
	)
	g = 0
	bshift = (ec_ilog(_val) - 1) >> 1
	b = uint(1 << bshift)
	for {
		{
			var t uint32
			t = uint32(int32(((int(uint32(g)) << 1) + int(b)) << bshift))
			if int(t) <= int(_val) {
				g += b
				_val -= t
			}
			b >>= 1
			bshift--
		}
		if bshift < 0 {
			break
		}
	}
	return g
}
