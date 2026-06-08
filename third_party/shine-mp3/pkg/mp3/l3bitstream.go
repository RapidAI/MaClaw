package mp3

// This is called after a frame of audio has been quantized and coded.
// It will write the encoded audio to the bitstream. Note that
// from a layer3 encoder's perspective the bit stream is primarily
// a series of main_data() blocks, with header and side information
// inserted at the proper locations to maintain framing. (See Figure A.7 in the IS).
func (enc *Encoder) formatBitstream() {
	var (
		gr int64
		ch int64
		i  int64
	)
	for ch = 0; ch < enc.Wave.Channels; ch++ {
		for gr = 0; gr < enc.Mpeg.GranulesPerFrame; gr++ {
			pi := &enc.l3Encoding[ch][gr]
			pr := &enc.mdctFrequency[ch][gr]
			for i = 0; i < GRANULE_SIZE; i++ {

				if pr[i] < 0 && pi[i] > 0 {
					pi[i] *= -1
				}
			}
		}
	}
	enc.encodeSideInfo()
	enc.encodeMainData()
}
func (enc *Encoder) encodeMainData() {
	var (
		gr  int64
		ch  int64
		sfb int64
		si  SideInfo = enc.sideInfo
	)
	for gr = 0; gr < enc.Mpeg.GranulesPerFrame; gr++ {
		for ch = 0; ch < enc.Wave.Channels; ch++ {
			var (
				gi    *GranuleInfo         = &si.Granules[gr].Channels[ch].Tt
				slen1 uint64               = uint64(slen1Table[gi.ScaleFactorCompress])
				slen2 uint64               = uint64(slen2Table[gi.ScaleFactorCompress])
				ix    *[GRANULE_SIZE]int64 = &enc.l3Encoding[ch][gr]
			)
			if gr == 0 || si.ScaleFactorSelectInfo[ch][0] == 0 {
				for sfb = 0; sfb < 6; sfb++ {
					enc.bitstream.putBits(uint32(enc.scaleFactor.L[gr][ch][sfb]), uint(slen1))
				}
			}
			if gr == 0 || si.ScaleFactorSelectInfo[ch][1] == 0 {
				for sfb = 6; sfb < 11; sfb++ {
					enc.bitstream.putBits(uint32(enc.scaleFactor.L[gr][ch][sfb]), uint(slen1))
				}
			}
			if gr == 0 || si.ScaleFactorSelectInfo[ch][2] == 0 {
				for sfb = 11; sfb < 16; sfb++ {
					enc.bitstream.putBits(uint32(enc.scaleFactor.L[gr][ch][sfb]), uint(slen2))
				}
			}
			if gr == 0 || si.ScaleFactorSelectInfo[ch][3] == 0 {
				for sfb = 16; sfb < 21; sfb++ {
					enc.bitstream.putBits(uint32(enc.scaleFactor.L[gr][ch][sfb]), uint(slen2))
				}
			}
			enc.huffmanCodeBits(ix, gi)
		}
	}
}
func (enc *Encoder) encodeSideInfo() {
	var (
		gr         int64
		ch         int64
		scfsi_band int64
		region     int64
		si         SideInfo = enc.sideInfo
	)
	enc.bitstream.putBits(2047, 11)
	enc.bitstream.putBits(uint32(enc.Mpeg.Version), 2)
	enc.bitstream.putBits(uint32(enc.Mpeg.Layer), 2)
	if enc.Mpeg.Crc == 0 {
		enc.bitstream.putBits(uint32(1), 1)
	} else {
		enc.bitstream.putBits(uint32(0), 1)
	}
	enc.bitstream.putBits(uint32(enc.Mpeg.BitrateIndex), 4)
	enc.bitstream.putBits(uint32(enc.Mpeg.SampleRateIndex%3), 2)
	enc.bitstream.putBits(uint32(enc.Mpeg.Padding), 1)
	enc.bitstream.putBits(uint32(enc.Mpeg.Ext), 1)
	enc.bitstream.putBits(uint32(enc.Mpeg.Mode), 2)
	enc.bitstream.putBits(uint32(enc.Mpeg.ModeExt), 2)
	enc.bitstream.putBits(uint32(enc.Mpeg.Copyright), 1)
	enc.bitstream.putBits(uint32(enc.Mpeg.Original), 1)
	enc.bitstream.putBits(uint32(enc.Mpeg.Emph), 2)
	if enc.Mpeg.Version == MPEG_I {
		enc.bitstream.putBits(0, 9)
		if enc.Wave.Channels == 2 {
			enc.bitstream.putBits(uint32(si.PrivateBits), 3)
		} else {
			enc.bitstream.putBits(uint32(si.PrivateBits), 5)
		}
	} else {
		enc.bitstream.putBits(0, 8)
		if enc.Wave.Channels == 2 {
			enc.bitstream.putBits(uint32(si.PrivateBits), 2)
		} else {
			enc.bitstream.putBits(uint32(si.PrivateBits), 1)
		}
	}
	if enc.Mpeg.Version == MPEG_I {
		for ch = 0; ch < enc.Wave.Channels; ch++ {
			for scfsi_band = 0; scfsi_band < 4; scfsi_band++ {
				enc.bitstream.putBits(uint32(si.ScaleFactorSelectInfo[ch][scfsi_band]), 1)
			}
		}
	}
	for gr = 0; gr < enc.Mpeg.GranulesPerFrame; gr++ {
		for ch = 0; ch < enc.Wave.Channels; ch++ {
			var gi *GranuleInfo = &si.Granules[gr].Channels[ch].Tt
			enc.bitstream.putBits(uint32(gi.Part2_3Length), 12)
			enc.bitstream.putBits(uint32(gi.BigValues), 9)
			enc.bitstream.putBits(uint32(gi.GlobalGain), 8)
			if enc.Mpeg.Version == MPEG_I {
				enc.bitstream.putBits(uint32(gi.ScaleFactorCompress), 4)
			} else {
				enc.bitstream.putBits(uint32(gi.ScaleFactorCompress), 9)
			}
			enc.bitstream.putBits(0, 1)
			for region = 0; region < 3; region++ {
				enc.bitstream.putBits(uint32(gi.TableSelect[region]), 5)
			}
			enc.bitstream.putBits(uint32(gi.Region0Count), 4)
			enc.bitstream.putBits(uint32(gi.Region1Count), 3)
			if enc.Mpeg.Version == MPEG_I {
				enc.bitstream.putBits(uint32(gi.PreFlag), 1)
			}
			enc.bitstream.putBits(uint32(gi.ScaleFactorScale), 1)
			enc.bitstream.putBits(uint32(gi.Count1TableSelect), 1)
		}
	}
}
func (enc *Encoder) huffmanCodeBits(ix *[GRANULE_SIZE]int64, gi *GranuleInfo) {
	var (
		scalefac       [23]int64 = scaleFactorBandIndex[enc.Mpeg.SampleRateIndex]
		scalefac_index uint64
		region1Start   int64
		region2Start   int64
		i              int64
		bigvalues      int64
		count1End      int64
		v              int64
		w              int64
		x              int64
		y              int64
		h              *huffCodeTableInfo
		bits           int64
	)
	bits = int64(enc.bitstream.getBitsCount())
	bigvalues = int64(gi.BigValues << 1)
	scalefac_index = gi.Region0Count + 1
	region1Start = scalefac[scalefac_index]
	scalefac_index += gi.Region1Count + 1
	region2Start = scalefac[scalefac_index]
	for i = 0; i < bigvalues; i += 2 {
		idx := 0
		if i >= region1Start {
			idx++
		}
		if i >= region2Start {
			idx++
		}
		var tableindex uint64 = gi.TableSelect[idx]
		if tableindex != 0 {
			x = (*ix)[i]
			y = (*ix)[i+1]
			huffmanCode(&enc.bitstream, int64(tableindex), x, y)
		}
	}
	h = &huffmanCodeTable[gi.Count1TableSelect+32]
	count1End = int64(uint64(bigvalues) + (gi.Count1 << 2))
	for i = bigvalues; i < count1End; i += 4 {
		v = (*ix)[i]
		w = (*ix)[i+1]
		x = (*ix)[i+2]
		y = (*ix)[i+3]
		huffmanCoderCount1(&enc.bitstream, h, v, w, x, y)
	}
	bits = int64(enc.bitstream.getBitsCount()) - bits
	bits = int64(gi.Part2_3Length - gi.Part2Length - uint64(bits))
	if bits != 0 {
		var (
			stuffingWords int64 = bits / 32
			remainingBits int64 = bits % 32
		)
		for func() int64 {
			p := &stuffingWords
			x := *p
			*p--
			return x
		}() != 0 {
			enc.bitstream.putBits(^uint32(0), 32)
		}
		if remainingBits != 0 {
			enc.bitstream.putBits(uint32((1<<remainingBits)-1), uint(remainingBits))
		}
	}
}
func absAndSign(x *int64) int64 {
	if *x > 0 {
		return 0
	}
	*x *= -1
	return 1
}
func huffmanCoderCount1(bs *bitstream, h *huffCodeTableInfo, v int64, w int64, x int64, y int64) {
	var (
		signv uint64
		signw uint64
		signx uint64
		signy uint64
		code  uint64 = 0
		p     int64
		cbits int64 = 0
	)
	signv = uint64(absAndSign(&v))
	signw = uint64(absAndSign(&w))
	signx = uint64(absAndSign(&x))
	signy = uint64(absAndSign(&y))
	p = v + (w << 1) + (x << 2) + (y << 3)
	bs.putBits(uint32(h.table[p]), uint(h.hLen[p]))
	if v != 0 {
		code = signv
		cbits = 1
	}
	if w != 0 {
		code = (code << 1) | signw
		cbits++
	}
	if x != 0 {
		code = (code << 1) | signx
		cbits++
	}
	if y != 0 {
		code = (code << 1) | signy
		cbits++
	}
	bs.putBits(uint32(code), uint(cbits))
}
func huffmanCode(bs *bitstream, table_select int64, x int64, y int64) {
	var (
		cbits int64  = 0
		xbits int64  = 0
		code  uint64 = 0
		ext   uint64 = 0
		signx uint64
		signy uint64
		ylen  uint64
		idx   uint64
		h     *huffCodeTableInfo
	)
	signx = uint64(absAndSign(&x))
	signy = uint64(absAndSign(&y))
	h = &(huffmanCodeTable[table_select])
	ylen = uint64(h.yLen)
	if table_select > 15 {
		var (
			linbitsx uint64 = 0
			linbitsy uint64 = 0
			linbits  uint64 = uint64(h.linBits)
		)
		if x > 14 {
			linbitsx = uint64(x - 15)
			x = 15
		}
		if y > 14 {
			linbitsy = uint64(y - 15)
			y = 15
		}
		idx = (uint64(x) * ylen) + uint64(y)
		code = uint64(h.table[idx])
		cbits = int64(h.hLen[idx])
		if x > 14 {
			ext |= linbitsx
			xbits += int64(linbits)
		}
		if x != 0 {
			ext <<= 1
			ext |= signx
			xbits += 1
		}
		if y > 14 {
			ext <<= linbits
			ext |= linbitsy
			xbits += int64(linbits)
		}
		if y != 0 {
			ext <<= 1
			ext |= signy
			xbits += 1
		}
		bs.putBits(uint32(code), uint(cbits))
		bs.putBits(uint32(ext), uint(xbits))
	} else {
		idx = (uint64(x) * ylen) + uint64(y)
		code = uint64(h.table[idx])
		cbits = int64(h.hLen[idx])
		if x != 0 {
			code <<= 1
			code |= signx
			cbits += 1
		}
		if y != 0 {
			code <<= 1
			code |= signy
			cbits += 1
		}
		bs.putBits(uint32(code), uint(cbits))
	}
}
