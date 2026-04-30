package libopus

import "github.com/RapidAI/CodeClaw/corelib/opus/internal/libc"

const LAPLACE_LOG_MINP = 0
const LAPLACE_MINP = 1
const LAPLACE_NMIN = 16

func ec_laplace_get_freq1(fs0 uint, decay int) uint {
	var ft uint
	ft = 32768 - (1<<0)*(2*16) - fs0
	return ft * uint(int32(16384-decay)) >> 15
}
func ec_laplace_encode(enc *ec_enc, value *int, fs uint, decay int) {
	var (
		fl  uint
		val int = *value
	)
	fl = 0
	if val != 0 {
		var (
			s int
			i int
		)
		s = -int(libc.BoolToInt(val < 0))
		val = (val + s) ^ s
		fl = fs
		fs = ec_laplace_get_freq1(fs, decay)
		for i = 1; fs > 0 && i < val; i++ {
			fs *= 2
			fl += fs + 2*(1<<0)
			fs = (fs * uint(int32(decay))) >> 15
		}
		if fs == 0 {
			var (
				di      int
				ndi_max int
			)
			ndi_max = int((32768 - fl + (1 << 0) - 1) >> 0)
			ndi_max = (ndi_max - s) >> 1
			if (val - i) < (ndi_max - 1) {
				di = val - i
			} else {
				di = ndi_max - 1
			}
			fl += uint((di*2 + 1 + s) * (1 << 0))
			if (1 << 0) < (32768 - fl) {
				fs = 1 << 0
			} else {
				fs = 32768 - fl
			}
			*value = (i + di + s) ^ s
		} else {
			fs += 1 << 0
			fl += fs & uint(^s)
		}
	}
	ec_encode_bin(enc, fl, fl+fs, 15)
}
func ec_laplace_decode(dec *ec_dec, fs uint, decay int) int {
	var (
		val int = 0
		fl  uint
		fm  uint
	)
	fm = ec_decode_bin(dec, 15)
	fl = 0
	if fm >= fs {
		val++
		fl = fs
		fs = ec_laplace_get_freq1(fs, decay) + (1 << 0)
		for fs > (1<<0) && fm >= fl+fs*2 {
			fs *= 2
			fl += fs
			fs = ((fs - 2*(1<<0)) * uint(int32(decay))) >> 15
			fs += 1 << 0
			val++
		}
		if fs <= (1 << 0) {
			var di int
			di = int((fm - fl) >> (0 + 1))
			val += di
			fl += uint(di * 2 * (1 << 0))
		}
		if fm < fl+fs {
			val = -val
		} else {
			fl += fs
		}
	}
	ec_dec_update(dec, fl, func() uint {
		if (fl + fs) < 32768 {
			return fl + fs
		}
		return 32768
	}(), 32768)
	return val
}
