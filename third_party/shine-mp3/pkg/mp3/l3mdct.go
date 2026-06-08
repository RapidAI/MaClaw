package mp3

import (
	"math"
)

// This is table B.9: coefficients for aliasing reduction
func MDCT_CA(coefficient float64) int32 {
	return int32(coefficient / math.Sqrt(1.0+(coefficient*coefficient)) * float64(math.MaxInt32))
}
func MDCT_CS(coefficient float64) int32 {
	return int32(1.0 / math.Sqrt(1.0+(coefficient*coefficient)) * float64(math.MaxInt32))
}

var (
	MDCT_CA0 = MDCT_CA(-0.6)
	MDCT_CA1 = MDCT_CA(-0.535)
	MDCT_CA2 = MDCT_CA(-0.33)
	MDCT_CA3 = MDCT_CA(-0.185)
	MDCT_CA4 = MDCT_CA(-0.095)
	MDCT_CA5 = MDCT_CA(-0.041)
	MDCT_CA6 = MDCT_CA(-0.0142)
	MDCT_CA7 = MDCT_CA(-0.0037)

	MDCT_CS0 = MDCT_CS(-0.6)
	MDCT_CS1 = MDCT_CS(-0.535)
	MDCT_CS2 = MDCT_CS(-0.33)
	MDCT_CS3 = MDCT_CS(-0.185)
	MDCT_CS4 = MDCT_CS(-0.095)
	MDCT_CS5 = MDCT_CS(-0.041)
	MDCT_CS6 = MDCT_CS(-0.0142)
	MDCT_CS7 = MDCT_CS(-0.0037)
)

func (enc *Encoder) mdctInitialize() {
	var (
		m int64
		k int64
	)
	for m = 18; func() int64 {
		p := &m
		x := *p
		*p--
		return x
	}() != 0; {
		for k = 36; func() int64 {
			p := &k
			x := *p
			*p--
			return x
		}() != 0; {
			enc.mdct.CosL[m][k] = int32(math.Sin(PI36*(float64(k)+0.5)) * math.Cos((PI/72)*float64(k*2+19)*float64(m*2+1)) * math.MaxInt32)
		}
	}
}
func (enc *Encoder) mdctSub(stride int64) {
	var (
		ch      int64
		gr      int64
		band    int64
		j       int64
		k       int64
		mdct_in [36]int32
	)
	for ch = enc.Wave.Channels; func() int64 {
		p := &ch
		x := *p
		*p--
		return x
	}() != 0; {
		for gr = 0; gr < enc.Mpeg.GranulesPerFrame; gr++ {
			mdct_enc := &enc.mdctFrequency[ch][gr]
			for k = 0; k < 18; k += 2 {
				enc.windowFilterSubband(&enc.buffer[ch], &enc.l3SubbandSamples[ch][gr+1][k], ch, stride)
				enc.windowFilterSubband(&enc.buffer[ch], &enc.l3SubbandSamples[ch][gr+1][k+1], ch, stride)
				for band = 1; band < 32; band += 2 {
					enc.l3SubbandSamples[ch][gr+1][k+1][band] *= -1
				}
			}
			for band = 0; band < 32; band++ {
				for k = 18; func() int64 {
					p := &k
					x := *p
					*p--
					return x
				}() != 0; {
					mdct_in[k] = enc.l3SubbandSamples[ch][gr][k][band]
					mdct_in[k+18] = enc.l3SubbandSamples[ch][gr+1][k][band]
				}
				for k = 18; func() int64 {
					p := &k
					x := *p
					*p--
					return x
				}() != 0; {
					var (
						vm    int32
						vm_lo uint32
					)
					_ = vm_lo
					vm = int32(((int64(mdct_in[35])) * (int64(enc.mdct.CosL[k][35]))) >> 32)
					for j = 35; j != 0; j -= 7 {
						vm += mul(mdct_in[j-1], enc.mdct.CosL[k][j-1])
						vm += mul(mdct_in[j-2], enc.mdct.CosL[k][j-2])
						vm += mul(mdct_in[j-3], enc.mdct.CosL[k][j-3])
						vm += mul(mdct_in[j-4], enc.mdct.CosL[k][j-4])
						vm += mul(mdct_in[j-5], enc.mdct.CosL[k][j-5])
						vm += mul(mdct_in[j-6], enc.mdct.CosL[k][j-6])
						vm += mul(mdct_in[j-7], enc.mdct.CosL[k][j-7])
					}
					mdct_enc[band*18+k] = vm
				}
				// Perform aliasing reduction butterfly
				if band != 0 {
					mdct_enc[band*18+0], mdct_enc[(band-1)*18+17-0] = cmuls(
						&mdct_enc[band*18+0], &mdct_enc[(band-1)*18+17-0],
						&MDCT_CS0, &MDCT_CA0,
					)

					mdct_enc[band*18+1], mdct_enc[(band-1)*18+17-1] = cmuls(
						&mdct_enc[band*18+1], &mdct_enc[(band-1)*18+17-1],
						&MDCT_CS1, &MDCT_CA1,
					)

					mdct_enc[band*18+2], mdct_enc[(band-1)*18+17-2] = cmuls(
						&mdct_enc[band*18+2], &mdct_enc[(band-1)*18+17-2],
						&MDCT_CS2, &MDCT_CA2,
					)

					mdct_enc[band*18+3], mdct_enc[(band-1)*18+17-3] = cmuls(
						&mdct_enc[band*18+3], &mdct_enc[(band-1)*18+17-3],
						&MDCT_CS3, &MDCT_CA3,
					)

					mdct_enc[band*18+4], mdct_enc[(band-1)*18+17-4] = cmuls(
						&mdct_enc[band*18+4], &mdct_enc[(band-1)*18+17-4],
						&MDCT_CS4, &MDCT_CA4,
					)

					mdct_enc[band*18+5], mdct_enc[(band-1)*18+17-5] = cmuls(
						&mdct_enc[band*18+5], &mdct_enc[(band-1)*18+17-5],
						&MDCT_CS5, &MDCT_CA5,
					)

					mdct_enc[band*18+6], mdct_enc[(band-1)*18+17-6] = cmuls(
						&mdct_enc[band*18+6], &mdct_enc[(band-1)*18+17-6],
						&MDCT_CS6, &MDCT_CA6,
					)

					mdct_enc[band*18+7], mdct_enc[(band-1)*18+17-7] = cmuls(
						&mdct_enc[band*18+7], &mdct_enc[(band-1)*18+17-7],
						&MDCT_CS7, &MDCT_CA7,
					)
				}
			}
		}
		copy(enc.l3SubbandSamples[ch][0][:], enc.l3SubbandSamples[ch][enc.Mpeg.GranulesPerFrame][:])
	}
}
