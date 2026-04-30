package libopus

import (
	"github.com/RapidAI/CodeClaw/corelib/opus/internal/libc"
	"unsafe"
)

const MAX_PSEUDO = 40
const LOG_MAX_PSEUDO = 6
const CELT_MAX_PULSES = 128
const MAX_FINE_BITS = 8
const FINE_OFFSET = 21
const QTHETA_OFFSET = 4
const QTHETA_OFFSET_TWOPHASE = 16
const ALLOC_STEPS = 6

func get_pulses(i int) int {
	if i < 8 {
		return i
	}
	return ((i & 7) + 8) << ((i >> 3) - 1)
}
func bits2pulses(m *OpusCustomMode, band int, LM int, bits int) int {
	var (
		i     int
		lo    int
		hi    int
		cache *uint8
	)
	LM++
	cache = (*uint8)(unsafe.Add(unsafe.Pointer(m.Cache.Bits), *(*int16)(unsafe.Add(unsafe.Pointer(m.Cache.Index), unsafe.Sizeof(int16(0))*uintptr(LM*m.NbEBands+band)))))
	lo = 0
	hi = int(*cache)
	bits--
	for i = 0; i < LOG_MAX_PSEUDO; i++ {
		var mid int = (lo + hi + 1) >> 1
		if int(*(*uint8)(unsafe.Add(unsafe.Pointer(cache), mid))) >= bits {
			hi = mid
		} else {
			lo = mid
		}
	}
	if bits-(func() int {
		if lo == 0 {
			return -1
		}
		return int(*(*uint8)(unsafe.Add(unsafe.Pointer(cache), lo)))
	}()) <= int(*(*uint8)(unsafe.Add(unsafe.Pointer(cache), hi)))-bits {
		return lo
	} else {
		return hi
	}
}
func pulses2bits(m *OpusCustomMode, band int, LM int, pulses int) int {
	var cache *uint8
	LM++
	cache = (*uint8)(unsafe.Add(unsafe.Pointer(m.Cache.Bits), *(*int16)(unsafe.Add(unsafe.Pointer(m.Cache.Index), unsafe.Sizeof(int16(0))*uintptr(LM*m.NbEBands+band)))))
	if pulses == 0 {
		return 0
	}
	return int(*(*uint8)(unsafe.Add(unsafe.Pointer(cache), pulses))) + 1
}

var LOG2_FRAC_TABLE [24]uint8 = [24]uint8{0, 8, 13, 16, 19, 21, 23, 24, 26, 27, 28, 29, 30, 31, 32, 32, 33, 34, 34, 35, 36, 36, 37, 37}

func interp_bits2pulses(m *OpusCustomMode, start int, end int, skip_start int, bits1 *int, bits2 *int, thresh *int, cap_ *int, total int32, _balance *int32, skip_rsv int, intensity *int, intensity_rsv int, dual_stereo *int, dual_stereo_rsv int, bits *int, ebits *int, fine_priority *int, C int, LM int, ec *ec_ctx, encode int, prev int, signalBandwidth int) int {
	var (
		psum        int32
		lo          int
		hi          int
		i           int
		j           int
		logM        int
		stereo      int
		codedBands  int = -1
		alloc_floor int
		left        int32
		percoeff    int32
		done        int
		balance     int32
	)
	alloc_floor = C << BITRES
	stereo = int(libc.BoolToInt(C > 1))
	logM = LM << BITRES
	lo = 0
	hi = int(1 << ALLOC_STEPS)
	for i = 0; i < ALLOC_STEPS; i++ {
		var mid int = (lo + hi) >> 1
		psum = 0
		done = 0
		for j = end; func() int {
			p := &j
			x := *p
			*p--
			return x
		}() > start; {
			var tmp int = *(*int)(unsafe.Add(unsafe.Pointer(bits1), unsafe.Sizeof(int(0))*uintptr(j))) + (mid * int(int32(*(*int)(unsafe.Add(unsafe.Pointer(bits2), unsafe.Sizeof(int(0))*uintptr(j))))) >> ALLOC_STEPS)
			if tmp >= *(*int)(unsafe.Add(unsafe.Pointer(thresh), unsafe.Sizeof(int(0))*uintptr(j))) || done != 0 {
				done = 1
				if tmp < (*(*int)(unsafe.Add(unsafe.Pointer(cap_), unsafe.Sizeof(int(0))*uintptr(j)))) {
					psum += int32(tmp)
				} else {
					psum += int32(*(*int)(unsafe.Add(unsafe.Pointer(cap_), unsafe.Sizeof(int(0))*uintptr(j))))
				}
			} else {
				if tmp >= alloc_floor {
					psum += int32(alloc_floor)
				}
			}
		}
		if int(psum) > int(total) {
			hi = mid
		} else {
			lo = mid
		}
	}
	psum = 0
	done = 0
	for j = end; func() int {
		p := &j
		x := *p
		*p--
		return x
	}() > start; {
		var tmp int = *(*int)(unsafe.Add(unsafe.Pointer(bits1), unsafe.Sizeof(int(0))*uintptr(j))) + (int(int32(lo)) * *(*int)(unsafe.Add(unsafe.Pointer(bits2), unsafe.Sizeof(int(0))*uintptr(j))) >> ALLOC_STEPS)
		if tmp < *(*int)(unsafe.Add(unsafe.Pointer(thresh), unsafe.Sizeof(int(0))*uintptr(j))) && done == 0 {
			if tmp >= alloc_floor {
				tmp = alloc_floor
			} else {
				tmp = 0
			}
		} else {
			done = 1
		}
		if tmp < (*(*int)(unsafe.Add(unsafe.Pointer(cap_), unsafe.Sizeof(int(0))*uintptr(j)))) {
			tmp = tmp
		} else {
			tmp = *(*int)(unsafe.Add(unsafe.Pointer(cap_), unsafe.Sizeof(int(0))*uintptr(j)))
		}
		*(*int)(unsafe.Add(unsafe.Pointer(bits), unsafe.Sizeof(int(0))*uintptr(j))) = tmp
		psum += int32(tmp)
	}
	for codedBands = end; ; codedBands-- {
		var (
			band_width int
			band_bits  int
			rem        int
		)
		j = codedBands - 1
		if j <= skip_start {
			total += int32(skip_rsv)
			break
		}
		left = int32(int(total) - int(psum))
		percoeff = int32(celt_udiv(uint32(left), uint32(int32(int(*(*int16)(unsafe.Add(unsafe.Pointer(m.EBands), unsafe.Sizeof(int16(0))*uintptr(codedBands))))-int(*(*int16)(unsafe.Add(unsafe.Pointer(m.EBands), unsafe.Sizeof(int16(0))*uintptr(start))))))))
		left -= int32((int(*(*int16)(unsafe.Add(unsafe.Pointer(m.EBands), unsafe.Sizeof(int16(0))*uintptr(codedBands)))) - int(*(*int16)(unsafe.Add(unsafe.Pointer(m.EBands), unsafe.Sizeof(int16(0))*uintptr(start))))) * int(percoeff))
		if (int(left) - (int(*(*int16)(unsafe.Add(unsafe.Pointer(m.EBands), unsafe.Sizeof(int16(0))*uintptr(j)))) - int(*(*int16)(unsafe.Add(unsafe.Pointer(m.EBands), unsafe.Sizeof(int16(0))*uintptr(start)))))) > 0 {
			rem = int(left) - (int(*(*int16)(unsafe.Add(unsafe.Pointer(m.EBands), unsafe.Sizeof(int16(0))*uintptr(j)))) - int(*(*int16)(unsafe.Add(unsafe.Pointer(m.EBands), unsafe.Sizeof(int16(0))*uintptr(start)))))
		} else {
			rem = 0
		}
		band_width = int(*(*int16)(unsafe.Add(unsafe.Pointer(m.EBands), unsafe.Sizeof(int16(0))*uintptr(codedBands)))) - int(*(*int16)(unsafe.Add(unsafe.Pointer(m.EBands), unsafe.Sizeof(int16(0))*uintptr(j))))
		band_bits = *(*int)(unsafe.Add(unsafe.Pointer(bits), unsafe.Sizeof(int(0))*uintptr(j))) + int(percoeff)*band_width + rem
		if band_bits >= (func() int {
			if (*(*int)(unsafe.Add(unsafe.Pointer(thresh), unsafe.Sizeof(int(0))*uintptr(j)))) > (alloc_floor + (int(1 << BITRES))) {
				return *(*int)(unsafe.Add(unsafe.Pointer(thresh), unsafe.Sizeof(int(0))*uintptr(j)))
			}
			return alloc_floor + (int(1 << BITRES))
		}()) {
			if encode != 0 {
				var depth_threshold int
				if codedBands > 17 {
					if j < prev {
						depth_threshold = 7
					} else {
						depth_threshold = 9
					}
				} else {
					depth_threshold = 0
				}
				if codedBands <= start+2 || band_bits > (depth_threshold*band_width<<LM<<BITRES)>>4 && j <= signalBandwidth {
					ec_enc_bit_logp((*ec_enc)(unsafe.Pointer(ec)), 1, 1)
					break
				}
				ec_enc_bit_logp((*ec_enc)(unsafe.Pointer(ec)), 0, 1)
			} else if ec_dec_bit_logp((*ec_dec)(unsafe.Pointer(ec)), 1) != 0 {
				break
			}
			psum += int32(int(1 << BITRES))
			band_bits -= int(1 << BITRES)
		}
		psum -= int32(*(*int)(unsafe.Add(unsafe.Pointer(bits), unsafe.Sizeof(int(0))*uintptr(j))) + intensity_rsv)
		if intensity_rsv > 0 {
			intensity_rsv = int(LOG2_FRAC_TABLE[j-start])
		}
		psum += int32(intensity_rsv)
		if band_bits >= alloc_floor {
			psum += int32(alloc_floor)
			*(*int)(unsafe.Add(unsafe.Pointer(bits), unsafe.Sizeof(int(0))*uintptr(j))) = alloc_floor
		} else {
			*(*int)(unsafe.Add(unsafe.Pointer(bits), unsafe.Sizeof(int(0))*uintptr(j))) = 0
		}
	}
	if intensity_rsv > 0 {
		if encode != 0 {
			if (*intensity) < codedBands {
				*intensity = *intensity
			} else {
				*intensity = codedBands
			}
			ec_enc_uint((*ec_enc)(unsafe.Pointer(ec)), uint32(int32(*intensity-start)), uint32(int32(codedBands+1-start)))
		} else {
			*intensity = start + int(ec_dec_uint((*ec_dec)(unsafe.Pointer(ec)), uint32(int32(codedBands+1-start))))
		}
	} else {
		*intensity = 0
	}
	if *intensity <= start {
		total += int32(dual_stereo_rsv)
		dual_stereo_rsv = 0
	}
	if dual_stereo_rsv > 0 {
		if encode != 0 {
			ec_enc_bit_logp((*ec_enc)(unsafe.Pointer(ec)), *dual_stereo, 1)
		} else {
			*dual_stereo = ec_dec_bit_logp((*ec_dec)(unsafe.Pointer(ec)), 1)
		}
	} else {
		*dual_stereo = 0
	}
	left = int32(int(total) - int(psum))
	percoeff = int32(celt_udiv(uint32(left), uint32(int32(int(*(*int16)(unsafe.Add(unsafe.Pointer(m.EBands), unsafe.Sizeof(int16(0))*uintptr(codedBands))))-int(*(*int16)(unsafe.Add(unsafe.Pointer(m.EBands), unsafe.Sizeof(int16(0))*uintptr(start))))))))
	left -= int32((int(*(*int16)(unsafe.Add(unsafe.Pointer(m.EBands), unsafe.Sizeof(int16(0))*uintptr(codedBands)))) - int(*(*int16)(unsafe.Add(unsafe.Pointer(m.EBands), unsafe.Sizeof(int16(0))*uintptr(start))))) * int(percoeff))
	for j = start; j < codedBands; j++ {
		*(*int)(unsafe.Add(unsafe.Pointer(bits), unsafe.Sizeof(int(0))*uintptr(j))) += int(percoeff) * (int(*(*int16)(unsafe.Add(unsafe.Pointer(m.EBands), unsafe.Sizeof(int16(0))*uintptr(j+1)))) - int(*(*int16)(unsafe.Add(unsafe.Pointer(m.EBands), unsafe.Sizeof(int16(0))*uintptr(j)))))
	}
	for j = start; j < codedBands; j++ {
		var tmp int = (func() int {
			if int(left) < (int(*(*int16)(unsafe.Add(unsafe.Pointer(m.EBands), unsafe.Sizeof(int16(0))*uintptr(j+1)))) - int(*(*int16)(unsafe.Add(unsafe.Pointer(m.EBands), unsafe.Sizeof(int16(0))*uintptr(j))))) {
				return int(left)
			}
			return int(*(*int16)(unsafe.Add(unsafe.Pointer(m.EBands), unsafe.Sizeof(int16(0))*uintptr(j+1)))) - int(*(*int16)(unsafe.Add(unsafe.Pointer(m.EBands), unsafe.Sizeof(int16(0))*uintptr(j))))
		}())
		*(*int)(unsafe.Add(unsafe.Pointer(bits), unsafe.Sizeof(int(0))*uintptr(j))) += tmp
		left -= int32(tmp)
	}
	balance = 0
	for j = start; j < codedBands; j++ {
		var (
			N0     int
			N      int
			den    int
			offset int
			NClogN int
			excess int32
			bit    int32
		)
		N0 = int(*(*int16)(unsafe.Add(unsafe.Pointer(m.EBands), unsafe.Sizeof(int16(0))*uintptr(j+1)))) - int(*(*int16)(unsafe.Add(unsafe.Pointer(m.EBands), unsafe.Sizeof(int16(0))*uintptr(j))))
		N = N0 << LM
		bit = int32(int(int32(*(*int)(unsafe.Add(unsafe.Pointer(bits), unsafe.Sizeof(int(0))*uintptr(j))))) + int(balance))
		if N > 1 {
			if (int(bit) - *(*int)(unsafe.Add(unsafe.Pointer(cap_), unsafe.Sizeof(int(0))*uintptr(j)))) > 0 {
				excess = int32(int(bit) - *(*int)(unsafe.Add(unsafe.Pointer(cap_), unsafe.Sizeof(int(0))*uintptr(j))))
			} else {
				excess = 0
			}
			*(*int)(unsafe.Add(unsafe.Pointer(bits), unsafe.Sizeof(int(0))*uintptr(j))) = int(bit) - int(excess)
			den = C*N + (func() int {
				if C == 2 && N > 2 && *dual_stereo == 0 && j < *intensity {
					return 1
				}
				return 0
			}())
			NClogN = den * (int(*(*int16)(unsafe.Add(unsafe.Pointer(m.LogN), unsafe.Sizeof(int16(0))*uintptr(j)))) + logM)
			offset = (NClogN >> 1) - den*FINE_OFFSET
			if N == 2 {
				offset += den << BITRES >> 2
			}
			if *(*int)(unsafe.Add(unsafe.Pointer(bits), unsafe.Sizeof(int(0))*uintptr(j)))+offset < den*2<<BITRES {
				offset += NClogN >> 2
			} else if *(*int)(unsafe.Add(unsafe.Pointer(bits), unsafe.Sizeof(int(0))*uintptr(j)))+offset < den*3<<BITRES {
				offset += NClogN >> 3
			}
			if 0 > (*(*int)(unsafe.Add(unsafe.Pointer(bits), unsafe.Sizeof(int(0))*uintptr(j))) + offset + (den << (int(BITRES - 1)))) {
				*(*int)(unsafe.Add(unsafe.Pointer(ebits), unsafe.Sizeof(int(0))*uintptr(j))) = 0
			} else {
				*(*int)(unsafe.Add(unsafe.Pointer(ebits), unsafe.Sizeof(int(0))*uintptr(j))) = *(*int)(unsafe.Add(unsafe.Pointer(bits), unsafe.Sizeof(int(0))*uintptr(j))) + offset + (den << (int(BITRES - 1)))
			}
			*(*int)(unsafe.Add(unsafe.Pointer(ebits), unsafe.Sizeof(int(0))*uintptr(j))) = int(celt_udiv(uint32(int32(*(*int)(unsafe.Add(unsafe.Pointer(ebits), unsafe.Sizeof(int(0))*uintptr(j))))), uint32(int32(den)))) >> BITRES
			if C**(*int)(unsafe.Add(unsafe.Pointer(ebits), unsafe.Sizeof(int(0))*uintptr(j))) > (*(*int)(unsafe.Add(unsafe.Pointer(bits), unsafe.Sizeof(int(0))*uintptr(j))) >> BITRES) {
				*(*int)(unsafe.Add(unsafe.Pointer(ebits), unsafe.Sizeof(int(0))*uintptr(j))) = *(*int)(unsafe.Add(unsafe.Pointer(bits), unsafe.Sizeof(int(0))*uintptr(j))) >> stereo >> BITRES
			}
			if (*(*int)(unsafe.Add(unsafe.Pointer(ebits), unsafe.Sizeof(int(0))*uintptr(j)))) < MAX_FINE_BITS {
				*(*int)(unsafe.Add(unsafe.Pointer(ebits), unsafe.Sizeof(int(0))*uintptr(j))) = *(*int)(unsafe.Add(unsafe.Pointer(ebits), unsafe.Sizeof(int(0))*uintptr(j)))
			} else {
				*(*int)(unsafe.Add(unsafe.Pointer(ebits), unsafe.Sizeof(int(0))*uintptr(j))) = MAX_FINE_BITS
			}
			*(*int)(unsafe.Add(unsafe.Pointer(fine_priority), unsafe.Sizeof(int(0))*uintptr(j))) = int(libc.BoolToInt(*(*int)(unsafe.Add(unsafe.Pointer(ebits), unsafe.Sizeof(int(0))*uintptr(j)))*(den<<BITRES) >= *(*int)(unsafe.Add(unsafe.Pointer(bits), unsafe.Sizeof(int(0))*uintptr(j)))+offset))
			*(*int)(unsafe.Add(unsafe.Pointer(bits), unsafe.Sizeof(int(0))*uintptr(j))) -= C * *(*int)(unsafe.Add(unsafe.Pointer(ebits), unsafe.Sizeof(int(0))*uintptr(j))) << BITRES
		} else {
			if 0 > (int(bit) - (C << BITRES)) {
				excess = 0
			} else {
				excess = int32(int(bit) - (C << BITRES))
			}
			*(*int)(unsafe.Add(unsafe.Pointer(bits), unsafe.Sizeof(int(0))*uintptr(j))) = int(bit) - int(excess)
			*(*int)(unsafe.Add(unsafe.Pointer(ebits), unsafe.Sizeof(int(0))*uintptr(j))) = 0
			*(*int)(unsafe.Add(unsafe.Pointer(fine_priority), unsafe.Sizeof(int(0))*uintptr(j))) = 1
		}
		if int(excess) > 0 {
			var (
				extra_fine int
				extra_bits int
			)
			if (int(excess) >> (stereo + BITRES)) < (MAX_FINE_BITS - *(*int)(unsafe.Add(unsafe.Pointer(ebits), unsafe.Sizeof(int(0))*uintptr(j)))) {
				extra_fine = int(excess) >> (stereo + BITRES)
			} else {
				extra_fine = MAX_FINE_BITS - *(*int)(unsafe.Add(unsafe.Pointer(ebits), unsafe.Sizeof(int(0))*uintptr(j)))
			}
			*(*int)(unsafe.Add(unsafe.Pointer(ebits), unsafe.Sizeof(int(0))*uintptr(j))) += extra_fine
			extra_bits = extra_fine * C << BITRES
			*(*int)(unsafe.Add(unsafe.Pointer(fine_priority), unsafe.Sizeof(int(0))*uintptr(j))) = int(libc.BoolToInt(extra_bits >= int(excess)-int(balance)))
			excess -= int32(extra_bits)
		}
		balance = excess
	}
	*_balance = balance
	for ; j < end; j++ {
		*(*int)(unsafe.Add(unsafe.Pointer(ebits), unsafe.Sizeof(int(0))*uintptr(j))) = *(*int)(unsafe.Add(unsafe.Pointer(bits), unsafe.Sizeof(int(0))*uintptr(j))) >> stereo >> BITRES
		*(*int)(unsafe.Add(unsafe.Pointer(bits), unsafe.Sizeof(int(0))*uintptr(j))) = 0
		*(*int)(unsafe.Add(unsafe.Pointer(fine_priority), unsafe.Sizeof(int(0))*uintptr(j))) = int(libc.BoolToInt(*(*int)(unsafe.Add(unsafe.Pointer(ebits), unsafe.Sizeof(int(0))*uintptr(j))) < 1))
	}
	return codedBands
}
func clt_compute_allocation(m *OpusCustomMode, start int, end int, offsets *int, cap_ *int, alloc_trim int, intensity *int, dual_stereo *int, total int32, balance *int32, pulses *int, ebits *int, fine_priority *int, C int, LM int, ec *ec_ctx, encode int, prev int, signalBandwidth int) int {
	var (
		lo              int
		hi              int
		len_            int
		j               int
		codedBands      int
		skip_start      int
		skip_rsv        int
		intensity_rsv   int
		dual_stereo_rsv int
		bits1           *int
		bits2           *int
		thresh          *int
		trim_offset     *int
	)
	if int(total) > 0 {
		total = total
	} else {
		total = 0
	}
	len_ = m.NbEBands
	skip_start = start
	if int(total) >= int(1<<BITRES) {
		skip_rsv = int(1 << BITRES)
	} else {
		skip_rsv = 0
	}
	total -= int32(skip_rsv)
	intensity_rsv = func() int {
		dual_stereo_rsv = 0
		return dual_stereo_rsv
	}()
	if C == 2 {
		intensity_rsv = int(LOG2_FRAC_TABLE[end-start])
		if intensity_rsv > int(total) {
			intensity_rsv = 0
		} else {
			total -= int32(intensity_rsv)
			if int(total) >= int(1<<BITRES) {
				dual_stereo_rsv = int(1 << BITRES)
			} else {
				dual_stereo_rsv = 0
			}
			total -= int32(dual_stereo_rsv)
		}
	}
	bits1 = (*int)(libc.Malloc(len_ * int(unsafe.Sizeof(int(0)))))
	bits2 = (*int)(libc.Malloc(len_ * int(unsafe.Sizeof(int(0)))))
	thresh = (*int)(libc.Malloc(len_ * int(unsafe.Sizeof(int(0)))))
	trim_offset = (*int)(libc.Malloc(len_ * int(unsafe.Sizeof(int(0)))))
	for j = start; j < end; j++ {
		if (C << BITRES) > (((int(*(*int16)(unsafe.Add(unsafe.Pointer(m.EBands), unsafe.Sizeof(int16(0))*uintptr(j+1)))) - int(*(*int16)(unsafe.Add(unsafe.Pointer(m.EBands), unsafe.Sizeof(int16(0))*uintptr(j))))) * 3 << LM << BITRES) >> 4) {
			*(*int)(unsafe.Add(unsafe.Pointer(thresh), unsafe.Sizeof(int(0))*uintptr(j))) = C << BITRES
		} else {
			*(*int)(unsafe.Add(unsafe.Pointer(thresh), unsafe.Sizeof(int(0))*uintptr(j))) = ((int(*(*int16)(unsafe.Add(unsafe.Pointer(m.EBands), unsafe.Sizeof(int16(0))*uintptr(j+1)))) - int(*(*int16)(unsafe.Add(unsafe.Pointer(m.EBands), unsafe.Sizeof(int16(0))*uintptr(j))))) * 3 << LM << BITRES) >> 4
		}
		*(*int)(unsafe.Add(unsafe.Pointer(trim_offset), unsafe.Sizeof(int(0))*uintptr(j))) = C * (int(*(*int16)(unsafe.Add(unsafe.Pointer(m.EBands), unsafe.Sizeof(int16(0))*uintptr(j+1)))) - int(*(*int16)(unsafe.Add(unsafe.Pointer(m.EBands), unsafe.Sizeof(int16(0))*uintptr(j))))) * (alloc_trim - 5 - LM) * (end - j - 1) * (1 << (LM + BITRES)) >> 6
		if (int(*(*int16)(unsafe.Add(unsafe.Pointer(m.EBands), unsafe.Sizeof(int16(0))*uintptr(j+1))))-int(*(*int16)(unsafe.Add(unsafe.Pointer(m.EBands), unsafe.Sizeof(int16(0))*uintptr(j)))))<<LM == 1 {
			*(*int)(unsafe.Add(unsafe.Pointer(trim_offset), unsafe.Sizeof(int(0))*uintptr(j))) -= C << BITRES
		}
	}
	lo = 1
	hi = m.NbAllocVectors - 1
	for {
		{
			var (
				done int = 0
				psum int = 0
				mid  int = (lo + hi) >> 1
			)
			for j = end; func() int {
				p := &j
				x := *p
				*p--
				return x
			}() > start; {
				var (
					bitsj int
					N     int = int(*(*int16)(unsafe.Add(unsafe.Pointer(m.EBands), unsafe.Sizeof(int16(0))*uintptr(j+1)))) - int(*(*int16)(unsafe.Add(unsafe.Pointer(m.EBands), unsafe.Sizeof(int16(0))*uintptr(j))))
				)
				bitsj = C * N * int(*(*uint8)(unsafe.Add(unsafe.Pointer(m.AllocVectors), mid*len_+j))) << LM >> 2
				if bitsj > 0 {
					if 0 > (bitsj + *(*int)(unsafe.Add(unsafe.Pointer(trim_offset), unsafe.Sizeof(int(0))*uintptr(j)))) {
						bitsj = 0
					} else {
						bitsj = bitsj + *(*int)(unsafe.Add(unsafe.Pointer(trim_offset), unsafe.Sizeof(int(0))*uintptr(j)))
					}
				}
				bitsj += *(*int)(unsafe.Add(unsafe.Pointer(offsets), unsafe.Sizeof(int(0))*uintptr(j)))
				if bitsj >= *(*int)(unsafe.Add(unsafe.Pointer(thresh), unsafe.Sizeof(int(0))*uintptr(j))) || done != 0 {
					done = 1
					if bitsj < (*(*int)(unsafe.Add(unsafe.Pointer(cap_), unsafe.Sizeof(int(0))*uintptr(j)))) {
						psum += bitsj
					} else {
						psum += *(*int)(unsafe.Add(unsafe.Pointer(cap_), unsafe.Sizeof(int(0))*uintptr(j)))
					}
				} else {
					if bitsj >= C<<BITRES {
						psum += C << BITRES
					}
				}
			}
			if psum > int(total) {
				hi = mid - 1
			} else {
				lo = mid + 1
			}
		}
		if lo > hi {
			break
		}
	}
	hi = func() int {
		p := &lo
		x := *p
		*p--
		return x
	}()
	for j = start; j < end; j++ {
		var (
			bits1j int
			bits2j int
			N      int = int(*(*int16)(unsafe.Add(unsafe.Pointer(m.EBands), unsafe.Sizeof(int16(0))*uintptr(j+1)))) - int(*(*int16)(unsafe.Add(unsafe.Pointer(m.EBands), unsafe.Sizeof(int16(0))*uintptr(j))))
		)
		bits1j = C * N * int(*(*uint8)(unsafe.Add(unsafe.Pointer(m.AllocVectors), lo*len_+j))) << LM >> 2
		if hi >= m.NbAllocVectors {
			bits2j = *(*int)(unsafe.Add(unsafe.Pointer(cap_), unsafe.Sizeof(int(0))*uintptr(j)))
		} else {
			bits2j = C * N * int(*(*uint8)(unsafe.Add(unsafe.Pointer(m.AllocVectors), hi*len_+j))) << LM >> 2
		}
		if bits1j > 0 {
			if 0 > (bits1j + *(*int)(unsafe.Add(unsafe.Pointer(trim_offset), unsafe.Sizeof(int(0))*uintptr(j)))) {
				bits1j = 0
			} else {
				bits1j = bits1j + *(*int)(unsafe.Add(unsafe.Pointer(trim_offset), unsafe.Sizeof(int(0))*uintptr(j)))
			}
		}
		if bits2j > 0 {
			if 0 > (bits2j + *(*int)(unsafe.Add(unsafe.Pointer(trim_offset), unsafe.Sizeof(int(0))*uintptr(j)))) {
				bits2j = 0
			} else {
				bits2j = bits2j + *(*int)(unsafe.Add(unsafe.Pointer(trim_offset), unsafe.Sizeof(int(0))*uintptr(j)))
			}
		}
		if lo > 0 {
			bits1j += *(*int)(unsafe.Add(unsafe.Pointer(offsets), unsafe.Sizeof(int(0))*uintptr(j)))
		}
		bits2j += *(*int)(unsafe.Add(unsafe.Pointer(offsets), unsafe.Sizeof(int(0))*uintptr(j)))
		if *(*int)(unsafe.Add(unsafe.Pointer(offsets), unsafe.Sizeof(int(0))*uintptr(j))) > 0 {
			skip_start = j
		}
		if 0 > (bits2j - bits1j) {
			bits2j = 0
		} else {
			bits2j = bits2j - bits1j
		}
		*(*int)(unsafe.Add(unsafe.Pointer(bits1), unsafe.Sizeof(int(0))*uintptr(j))) = bits1j
		*(*int)(unsafe.Add(unsafe.Pointer(bits2), unsafe.Sizeof(int(0))*uintptr(j))) = bits2j
	}
	codedBands = interp_bits2pulses(m, start, end, skip_start, bits1, bits2, thresh, cap_, total, balance, skip_rsv, intensity, intensity_rsv, dual_stereo, dual_stereo_rsv, pulses, ebits, fine_priority, C, LM, ec, encode, prev, signalBandwidth)
	return codedBands
}
