package mp3

import (
	"math"
	"unsafe"
)

// subbandInitialize calculates the analysis filterbank coefficients and rounds to the  9th decimal
// place accuracy of the filterbank tables in the ISO document. The coefficients are stored in #filter#
func (enc *Encoder) subbandInitialize() {
	var (
		i      int64
		j      int64
		filter float64
	)
	for i = MAX_CHANNELS; func() int64 {
		p := &i
		x := *p
		*p--
		return x
	}() != 0; {
		enc.subband.Off[i] = 0
		enc.subband.X[i] = [HAN_SIZE]int32{}
	}
	for i = SUBBAND_LIMIT; func() int64 {
		p := &i
		x := *p
		*p--
		return x
	}() != 0; {
		for j = 64; func() int64 {
			p := &j
			x := *p
			*p--
			return x
		}() != 0; {
			if (func() float64 {
				filter = math.Cos(float64((i*2+1)*(16-j))*PI64) * 1e+09
				return filter
			}()) >= 0 {
				filter, _ = math.Modf(filter + 0.5)
			} else {
				filter, _ = math.Modf(filter - 0.5)
			}
			enc.subband.Fl[i][j] = int32(filter * (math.MaxInt32 * 1e-09))
		}
	}
}

// Overlapping window on PCM samples 32 16-bit pcm samples are scaled to fractional 2's complement and
// concatenated to the end of the window buffer #x#. The updated window buffer #x# is then windowed by
// the analysis window #shine_enwindow# to produce the windowed sample #z# Calculates the analysis filter bank
// coefficients The windowed samples #z# is filtered by the digital filter matrix #filter# to produce the subband
// samples #s#. This done by first selectively picking out values from the windowed samples, and then
// multiplying them by the filter matrix, producing 32 subband samples.
func (enc *Encoder) windowFilterSubband(buffer **int16, s *[32]int32, ch int64, stride int64) {
	var (
		y   [64]int32
		i   int64
		j   int64
		ptr *int16 = *buffer
	)
	for i = 32; func() int64 {
		p := &i
		x := *p
		*p--
		return x
	}() != 0; {
		enc.subband.X[ch][i+enc.subband.Off[ch]] = int32(int64(int32(*ptr)) << 16)
		ptr = (*int16)(unsafe.Add(unsafe.Pointer(ptr), unsafe.Sizeof(int16(0))*uintptr(stride)))
	}
	*buffer = ptr
	for i = 64; func() int64 {
		p := &i
		x := *p
		*p--
		return x
	}() != 0; {
		var (
			s_value    int32
			s_value_lo uint32
		)
		_ = s_value_lo
		s_value = int32(((int64(enc.subband.X[ch][(enc.subband.Off[ch]+i+(0<<6))&(HAN_SIZE-1)])) * (int64(enWindow[i+(0<<6)]))) >> 32)
		s_value += int32(((int64(enc.subband.X[ch][(enc.subband.Off[ch]+i+(1<<6))&(HAN_SIZE-1)])) * (int64(enWindow[i+(1<<6)]))) >> 32)
		s_value += int32(((int64(enc.subband.X[ch][(enc.subband.Off[ch]+i+(2<<6))&(HAN_SIZE-1)])) * (int64(enWindow[i+(2<<6)]))) >> 32)
		s_value += int32(((int64(enc.subband.X[ch][(enc.subband.Off[ch]+i+(3<<6))&(HAN_SIZE-1)])) * (int64(enWindow[i+(3<<6)]))) >> 32)
		s_value += int32(((int64(enc.subband.X[ch][(enc.subband.Off[ch]+i+(4<<6))&(HAN_SIZE-1)])) * (int64(enWindow[i+(4<<6)]))) >> 32)
		s_value += int32(((int64(enc.subband.X[ch][(enc.subband.Off[ch]+i+(5<<6))&(HAN_SIZE-1)])) * (int64(enWindow[i+(5<<6)]))) >> 32)
		s_value += int32(((int64(enc.subband.X[ch][(enc.subband.Off[ch]+i+(6<<6))&(HAN_SIZE-1)])) * (int64(enWindow[i+(6<<6)]))) >> 32)
		s_value += int32(((int64(enc.subband.X[ch][(enc.subband.Off[ch]+i+(7<<6))&(HAN_SIZE-1)])) * (int64(enWindow[i+(7<<6)]))) >> 32)
		y[i] = s_value
	}
	enc.subband.Off[ch] = (enc.subband.Off[ch] + 480) & (HAN_SIZE - 1)
	for i = SUBBAND_LIMIT; func() int64 {
		p := &i
		x := *p
		*p--
		return x
	}() != 0; {
		var (
			s_value    int32
			s_value_lo uint32
		)
		_ = s_value_lo
		s_value = int32(((int64(enc.subband.Fl[i][63])) * (int64(y[63]))) >> 32)
		for j = 63; j != 0; j -= 7 {
			s_value += int32(((int64(enc.subband.Fl[i][j-1])) * (int64(y[j-1]))) >> 32)
			s_value += int32(((int64(enc.subband.Fl[i][j-2])) * (int64(y[j-2]))) >> 32)
			s_value += int32(((int64(enc.subband.Fl[i][j-3])) * (int64(y[j-3]))) >> 32)
			s_value += int32(((int64(enc.subband.Fl[i][j-4])) * (int64(y[j-4]))) >> 32)
			s_value += int32(((int64(enc.subband.Fl[i][j-5])) * (int64(y[j-5]))) >> 32)
			s_value += int32(((int64(enc.subband.Fl[i][j-6])) * (int64(y[j-6]))) >> 32)
			s_value += int32(((int64(enc.subband.Fl[i][j-7])) * (int64(y[j-7]))) >> 32)
		}
		s[i] = s_value
	}
}
