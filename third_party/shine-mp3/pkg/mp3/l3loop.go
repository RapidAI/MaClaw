package mp3

import (
	"math"
)

const (
	scaleFactorBand_LMax = 22
	enTotKrit            = 10
	enDifKrit            = 100
	enScfsiBandKrit      = 10
	xmScfsiBandKrit      = 10
)

// innerLoop selects the best quantizerStepSize for a particular set of scaleFactors.
func (enc *Encoder) innerLoop(ix *[GRANULE_SIZE]int64, max_bits int64, cod_info *GranuleInfo, gr int64, ch int64) int64 {
	var (
		bits   int64
		c1bits int64
		bvbits int64
	)
	if max_bits < 0 {
		cod_info.QuantizerStepSize--
	}
	for {
		for enc.quantize(ix, func() int64 {
			p := &cod_info.QuantizerStepSize
			*p++
			return *p
		}()) > 8192 {
		}
		calcRunLength(ix, cod_info)
		bits = func() int64 {
			c1bits = count1BitCount(ix, cod_info)
			return c1bits
		}()
		enc.subDivide(cod_info)
		bigValuesTableSelect(ix, cod_info)
		bits += func() int64 {
			bvbits = bigValuesBitCount(ix, cod_info)
			return bvbits
		}()
		if bits <= max_bits {
			break
		}
	}
	return bits
}

// outerLoop controls the masking conditions of all scaleFactorBands. It computes the best scaleFactor and
// global gain. This module calls the inner iteration loop.
// l3XMin: the allowed distortion of the scaleFactor.
// ix: vector of quantized values ix(0..575)
func (enc *Encoder) outerLoop(max_bits int64, l3_xmin *PsyXMin, ix *[GRANULE_SIZE]int64, gr int64, ch int64) int64 {
	var (
		bits      int64
		huff_bits int64
		side_info *SideInfo    = &enc.sideInfo
		cod_info  *GranuleInfo = &side_info.Granules[gr].Channels[ch].Tt
	)
	cod_info.QuantizerStepSize = enc.binSearchStepSize(max_bits, ix, cod_info)
	cod_info.Part2Length = uint64(enc.calcPart2Length(gr, ch))
	huff_bits = int64(uint64(max_bits) - cod_info.Part2Length)
	bits = enc.innerLoop(ix, huff_bits, cod_info, gr, ch)
	cod_info.Part2_3Length = cod_info.Part2Length + uint64(bits)
	return int64(cod_info.Part2_3Length)
}

func (enc *Encoder) iterationLoop() {
	var (
		l3_xmin  PsyXMin
		cod_info *GranuleInfo
		max_bits int64
		ch       int64
		gr       int64
		i        int64
		ix       *[GRANULE_SIZE]int64
	)
	for ch = enc.Wave.Channels; func() int64 {
		p := &ch
		x := *p
		*p--
		return x
	}() != 0; {
		for gr = 0; gr < enc.Mpeg.GranulesPerFrame; gr++ {
			ix = &enc.l3Encoding[ch][gr]
			enc.l3loop.Xr = enc.mdctFrequency[ch][gr][:]
			for func() int32 {
				i = GRANULE_SIZE
				return func() int32 {
					p := &enc.l3loop.Xrmax
					enc.l3loop.Xrmax = 0
					return *p
				}()
			}(); func() int64 {
				p := &i
				x := *p
				*p--
				return x
			}() != 0; {
				a := mulSR(enc.l3loop.Xr[i], enc.l3loop.Xr[i])
				enc.l3loop.Xrsq[i] = a
				a = int32(math.Abs(float64(enc.l3loop.Xr[i])))
				enc.l3loop.Xrabs[i] = a
				if int64(enc.l3loop.Xrabs[i]) > int64(enc.l3loop.Xrmax) {
					enc.l3loop.Xrmax = enc.l3loop.Xrabs[i]
				}
			}
			cod_info = &(enc.sideInfo.Granules[gr].Channels[ch]).Tt
			cod_info.ScaleFactorBandMaxLen = scaleFactorBand_LMax - 1
			calcXMin(&enc.ratio, cod_info, &l3_xmin, gr, ch)
			if enc.Mpeg.Version == MPEG_I {
				enc.calcSCFSI(&l3_xmin, ch, gr)
			}
			max_bits = enc.maxReservoirBits(&enc.PerceptualEnergy[ch][gr])
			for i = 4; func() int64 {
				p := &i
				x := *p
				*p--
				return x
			}() != 0; {
				cod_info.ScaleFactorLen[i] = 0
			}
			cod_info.Part2_3Length = 0
			cod_info.BigValues = 0
			cod_info.Count1 = 0
			cod_info.ScaleFactorCompress = 0
			cod_info.TableSelect[0] = 0
			cod_info.TableSelect[1] = 0
			cod_info.TableSelect[2] = 0
			cod_info.Region0Count = 0
			cod_info.Region1Count = 0
			cod_info.Part2Length = 0
			cod_info.PreFlag = 0
			cod_info.ScaleFactorScale = 0
			cod_info.Count1TableSelect = 0
			if int64(enc.l3loop.Xrmax) != 0 {
				cod_info.Part2_3Length = uint64(enc.outerLoop(max_bits, &l3_xmin, ix, gr, ch))
			}
			enc.reservoirAdjust(cod_info)
			cod_info.GlobalGain = uint64(cod_info.QuantizerStepSize + 210)
		}
	}
	enc.reservoirFrameEnd()
}

// calcSCFSI calculates the scalefactor select information ( scfsi )
func (enc *Encoder) calcSCFSI(l3_xmin *PsyXMin, ch int64, gr int64) {
	var (
		l3_side            *SideInfo = &enc.sideInfo
		scfsi_band_long    [5]int64  = [5]int64{0, 6, 11, 16, 21}
		scfsi_band         int64
		sfb                int64
		start              int64
		end                int64
		i                  int64
		condition          int64 = 0
		temp               int64
		scalefac_band_long *[23]int64 = &scaleFactorBandIndex[enc.Mpeg.SampleRateIndex]
	)
	enc.l3loop.Xrmaxl[gr] = enc.l3loop.Xrmax
	for func() int64 {
		temp = 0
		return func() int64 {
			i = GRANULE_SIZE
			return i
		}()
	}(); func() int64 {
		p := &i
		x := *p
		*p--
		return x
	}() != 0; {
		temp += int64(enc.l3loop.Xrsq[i]) >> 10
	}
	if temp != 0 {
		enc.l3loop.EnTot[gr] = int32(math.Log(float64(temp)*4.768371584e-07) / LN2)
	} else {
		enc.l3loop.EnTot[gr] = 0
	}
	for sfb = 21; func() int64 {
		p := &sfb
		x := *p
		*p--
		return x
	}() != 0; {
		start = scalefac_band_long[sfb]
		end = scalefac_band_long[sfb+1]
		for func() int64 {
			temp = 0
			return func() int64 {
				i = start
				return i
			}()
		}(); i < end; i++ {
			temp += int64(enc.l3loop.Xrsq[i]) >> 10
		}
		if temp != 0 {
			enc.l3loop.En[gr][sfb] = int32(math.Log(float64(temp)*4.768371584e-07) / LN2)
		} else {
			enc.l3loop.En[gr][sfb] = 0
		}
		if l3_xmin.L[gr][ch][sfb] != 0 {
			enc.l3loop.Xm[gr][sfb] = int32(math.Log(l3_xmin.L[gr][ch][sfb]) / LN2)
		} else {
			enc.l3loop.Xm[gr][sfb] = 0
		}
	}
	if gr == 1 {
		var (
			gr2 int64
			tp  int64
		)
		for gr2 = 2; func() int64 {
			p := &gr2
			x := *p
			*p--
			return x
		}() != 0; {
			if int64(enc.l3loop.Xrmaxl[gr2]) != 0 {
				condition++
			}
			condition++
		}
		if math.Abs(float64(enc.l3loop.EnTot[0])-float64(enc.l3loop.EnTot[1])) < enTotKrit {
			condition++
		}
		for func() int64 {
			tp = 0
			return func() int64 {
				sfb = 21
				return sfb
			}()
		}(); func() int64 {
			p := &sfb
			x := *p
			*p--
			return x
		}() != 0; {
			tp += int64(math.Abs(float64(enc.l3loop.En[0][sfb]) - float64(enc.l3loop.En[1][sfb])))
		}
		if tp < enDifKrit {
			condition++
		}
		if condition == 6 {
			for scfsi_band = 0; scfsi_band < 4; scfsi_band++ {
				var (
					sum0 int64 = 0
					sum1 int64 = 0
				)
				l3_side.ScaleFactorSelectInfo[ch][scfsi_band] = 0
				start = scfsi_band_long[scfsi_band]
				end = scfsi_band_long[scfsi_band+1]
				for sfb = start; sfb < end; sfb++ {
					sum0 += int64(math.Abs(float64(enc.l3loop.En[0][sfb]) - float64(enc.l3loop.En[1][sfb])))
					sum1 += int64(math.Abs(float64(enc.l3loop.Xm[0][sfb]) - float64(enc.l3loop.Xm[1][sfb])))
				}
				if sum0 < enScfsiBandKrit && sum1 < xmScfsiBandKrit {
					l3_side.ScaleFactorSelectInfo[ch][scfsi_band] = 1
				} else {
					l3_side.ScaleFactorSelectInfo[ch][scfsi_band] = 0
				}
			}
		} else {
			for scfsi_band = 0; scfsi_band < 4; scfsi_band++ {
				l3_side.ScaleFactorSelectInfo[ch][scfsi_band] = 0
			}
		}
	}
}

// calcPart2Length calculates the number of bits needed to encode the scaleFactor in the
// main data block.
func (enc *Encoder) calcPart2Length(gr int64, ch int64) int64 {
	var (
		slen1 int64
		slen2 int64
		bits  int64
		gi    *GranuleInfo = &enc.sideInfo.Granules[gr].Channels[ch].Tt
	)
	bits = 0
	{
		slen1 = slen1Table[gi.ScaleFactorCompress]
		slen2 = slen2Table[gi.ScaleFactorCompress]
		if gr == 0 || (enc.sideInfo.ScaleFactorSelectInfo[ch][0]) == 0 {
			bits += slen1 * 6
		}
		if gr == 0 || (enc.sideInfo.ScaleFactorSelectInfo[ch][1]) == 0 {
			bits += slen1 * 5
		}
		if gr == 0 || (enc.sideInfo.ScaleFactorSelectInfo[ch][2]) == 0 {
			bits += slen2 * 5
		}
		if gr == 0 || (enc.sideInfo.ScaleFactorSelectInfo[ch][3]) == 0 {
			bits += slen2 * 5
		}
	}
	return bits
}

// calcXMin calculates the allowed distortion for each scalefactor band,
// as determined by the psychoacoustic model. xmin(sb) = ratio(sb) * en(sb) / bw(sb)
func calcXMin(ratio *PsyRatio, cod_info *GranuleInfo, l3_xmin *PsyXMin, gr int64, ch int64) {
	var sfb int64
	for sfb = int64(cod_info.ScaleFactorBandMaxLen); func() int64 {
		p := &sfb
		x := *p
		*p--
		return x
	}() != 0; {
		l3_xmin.L[gr][ch][sfb] = 0
	}
}

// loopInitialize calculates the look up tables used by the iteration loop.
func (enc *Encoder) loopInitialize() {
	var i int64
	for i = 128; func() int64 {
		p := &i
		x := *p
		*p--
		return x
	}() != 0; {
		enc.l3loop.StepTable[i] = math.Pow(2.0, float64(127-i)/4)
		if (enc.l3loop.StepTable[i] * 2) > math.MaxInt32 {
			enc.l3loop.StepTableI[i] = math.MaxInt32
		} else {
			enc.l3loop.StepTableI[i] = int32((enc.l3loop.StepTable[i] * 2) + 0.5)
		}
	}
	for i = 10000; func() int64 {
		p := &i
		x := *p
		*p--
		return x
	}() != 0; {
		enc.l3loop.Int2idx[i] = int64(math.Sqrt(math.Sqrt(float64(i))*float64(i)) - 0.0946 + 0.5)
	}
}

// quantize perform quantization of the vector xr ( -> ix).
// Returns maximum value of ix
func (enc *Encoder) quantize(ix *[GRANULE_SIZE]int64, stepsize int64) int64 {
	var (
		i      int64
		max    int64
		ln     int64
		scalei int32
		scale  float64
		dbl    float64
	)
	scalei = enc.l3loop.StepTableI[stepsize+127]
	if int64(int32((((int64(enc.l3loop.Xrmax))*(int64(scalei)))+0x80000000)>>32)) > 165140 {
		max = 16384
	} else {
		for i = 0; i < GRANULE_SIZE; i++ {
			ln = int64(mulR(int32(math.Abs(float64(enc.l3loop.Xr[i]))), scalei))
			if ln < 10000 {
				ix[i] = enc.l3loop.Int2idx[ln]
			} else {
				scale = enc.l3loop.StepTable[stepsize+math.MaxInt8]
				dbl = (float64(enc.l3loop.Xrabs[i])) * scale * 4.656612875e-10
				ix[i] = int64(math.Sqrt(math.Sqrt(dbl) * dbl))
			}
			if max < ix[i] {
				max = ix[i]
			}
		}
	}
	return max
}

// ixMax calculates the maximum of ix from 0 to 575
func ixMax(ix *[GRANULE_SIZE]int64, begin uint64, end uint64) int64 {
	var (
		i   int64
		max int64 = 0
	)
	for i = int64(begin); uint64(i) < end; i++ {
		if max < ix[i] {
			max = ix[i]
		}
	}
	return max
}

// calcRunLength calculates rZero, count1, big_values (Partitions ix into big values, quadruples and zeros)
func calcRunLength(ix *[GRANULE_SIZE]int64, cod_info *GranuleInfo) {
	var (
		i     int64
		rzero int64 = 0
	)
	for i = GRANULE_SIZE; i > 1; i -= 2 {
		if ix[i-1] == 0 && ix[i-2] == 0 {
			rzero++
		} else {
			break
		}
	}
	cod_info.Count1 = 0
	for ; i > 3; i -= 4 {
		if ix[i-1] <= 1 && ix[i-2] <= 1 && ix[i-3] <= 1 && ix[i-4] <= 1 {
			cod_info.Count1++
		} else {
			break
		}
	}
	cod_info.BigValues = uint64(i >> 1)
}

// count1BitCount determines the number of bits to encode the quadruples.
func count1BitCount(ix *[GRANULE_SIZE]int64, cod_info *GranuleInfo) int64 {
	var (
		p        int64
		i        int64
		k        int64
		v        int64
		w        int64
		x        int64
		y        int64
		signbits int64
		sum0     int64 = 0
		sum1     int64 = 0
	)
	for func() int64 {
		i = int64(cod_info.BigValues << 1)
		return func() int64 {
			k = 0
			return k
		}()
	}(); uint64(k) < cod_info.Count1; func() int64 {
		i += 4
		return func() int64 {
			p := &k
			x := *p
			*p++
			return x
		}()
	}() {
		v = ix[i]
		w = ix[i+1]
		x = ix[i+2]
		y = ix[i+3]
		p = v + (w << 1) + (x << 2) + (y << 3)
		signbits = 0
		if v != 0 {
			signbits++
		}
		if w != 0 {
			signbits++
		}
		if x != 0 {
			signbits++
		}
		if y != 0 {
			signbits++
		}
		sum0 += signbits
		sum1 += signbits
		sum0 += int64(huffmanCodeTable[32].hLen[p])
		sum1 += int64(huffmanCodeTable[33].hLen[p])
	}
	if sum0 < sum1 {
		cod_info.Count1TableSelect = 0
		return sum0
	} else {
		cod_info.Count1TableSelect = 1
		return sum1
	}
}

// subDivide subdivides the bigValue region which will use separate Huffman tables.
func (enc *Encoder) subDivide(cod_info *GranuleInfo) {
	var subdv_table [23]struct {
		Region0_count uint64
		Region1_count uint64
	} = [23]struct {
		Region0_count uint64
		Region1_count uint64
	}{{}, {}, {}, {}, {}, {Region0_count: 0, Region1_count: 1}, {Region0_count: 1, Region1_count: 1}, {Region0_count: 1, Region1_count: 1}, {Region0_count: 1, Region1_count: 2}, {Region0_count: 2, Region1_count: 2}, {Region0_count: 2, Region1_count: 3}, {Region0_count: 2, Region1_count: 3}, {Region0_count: 3, Region1_count: 4}, {Region0_count: 3, Region1_count: 4}, {Region0_count: 3, Region1_count: 4}, {Region0_count: 4, Region1_count: 5}, {Region0_count: 4, Region1_count: 5}, {Region0_count: 4, Region1_count: 6}, {Region0_count: 5, Region1_count: 6}, {Region0_count: 5, Region1_count: 6}, {Region0_count: 5, Region1_count: 7}, {Region0_count: 6, Region1_count: 7}, {Region0_count: 6, Region1_count: 7}}
	if cod_info.BigValues == 0 {
		cod_info.Region0Count = 0
		cod_info.Region1Count = 0
	} else {
		var (
			scalefac_band_long []int64 = scaleFactorBandIndex[enc.Mpeg.SampleRateIndex][:]
			bigvalues_region   int64
			scfb_anz           int64
			thiscount          int64
		)
		bigvalues_region = int64(cod_info.BigValues * 2)
		scfb_anz = 0
		for scalefac_band_long[scfb_anz] < bigvalues_region {
			scfb_anz++
		}
		for thiscount = int64(subdv_table[scfb_anz].Region0_count); thiscount != 0; thiscount-- {
			if scalefac_band_long[thiscount+1] <= bigvalues_region {
				break
			}
		}
		cod_info.Region0Count = uint64(thiscount)
		cod_info.Address1 = uint64(scalefac_band_long[thiscount+1])
		scalefac_band_long = scalefac_band_long[cod_info.Region0Count+1:]
		for thiscount = int64(subdv_table[scfb_anz].Region1_count); thiscount != 0; thiscount-- {
			if scalefac_band_long[thiscount+1] <= bigvalues_region {
				break
			}
		}
		cod_info.Region1Count = uint64(thiscount)
		cod_info.Address2 = uint64(scalefac_band_long[thiscount+1])
		cod_info.Address3 = uint64(bigvalues_region)
	}
}

// bigValuesTableSelect selects huffman code tables for the bigValues region
func bigValuesTableSelect(ix *[GRANULE_SIZE]int64, cod_info *GranuleInfo) {
	cod_info.TableSelect[0] = 0
	cod_info.TableSelect[1] = 0
	cod_info.TableSelect[2] = 0
	{
		if cod_info.Address1 > 0 {
			cod_info.TableSelect[0] = uint64(newChooseTable(ix, 0, cod_info.Address1))
		}
		if cod_info.Address2 > cod_info.Address1 {
			cod_info.TableSelect[1] = uint64(newChooseTable(ix, cod_info.Address1, cod_info.Address2))
		}
		if cod_info.BigValues<<1 > cod_info.Address2 {
			cod_info.TableSelect[2] = uint64(newChooseTable(ix, cod_info.Address2, cod_info.BigValues<<1))
		}
	}
}

// newChooseTable chooses the Huffman table that will encode ix[begin..end] with the fewest bits.
// Note: This code contains knowledge about the sizes and characteristics of the Huffman tables as
// defined in the IS (Table B.7), and will not work with any arbitrary tables.
func newChooseTable(ix *[GRANULE_SIZE]int64, begin uint64, end uint64) int64 {
	var (
		i      int64
		max    int64
		choice [2]int64
		sum    [2]int64
	)
	max = ixMax(ix, begin, end)
	if max == 0 {
		return 0
	}
	choice[0] = 0
	choice[1] = 0
	if max < 15 {
		for i = 14; func() int64 {
			p := &i
			x := *p
			*p--
			return x
		}() != 0; {
			if huffmanCodeTable[i].xLen > uint(max) {
				choice[0] = i
				break
			}
		}
		sum[0] = countBit(ix, begin, end, uint64(choice[0]))
		switch choice[0] {
		case 2:
			sum[1] = countBit(ix, begin, end, 3)
			if sum[1] <= sum[0] {
				choice[0] = 3
			}
		case 5:
			sum[1] = countBit(ix, begin, end, 6)
			if sum[1] <= sum[0] {
				choice[0] = 6
			}
		case 7:
			sum[1] = countBit(ix, begin, end, 8)
			if sum[1] <= sum[0] {
				choice[0] = 8
				sum[0] = sum[1]
			}
			sum[1] = countBit(ix, begin, end, 9)
			if sum[1] <= sum[0] {
				choice[0] = 9
			}
		case 10:
			sum[1] = countBit(ix, begin, end, 11)
			if sum[1] <= sum[0] {
				choice[0] = 11
				sum[0] = sum[1]
			}
			sum[1] = countBit(ix, begin, end, 12)
			if sum[1] <= sum[0] {
				choice[0] = 12
			}
		case 13:
			sum[1] = countBit(ix, begin, end, 15)
			if sum[1] <= sum[0] {
				choice[0] = 15
			}
		}
	} else {
		max -= 15
		for i = 15; i < 24; i++ {
			if huffmanCodeTable[i].linMax >= uint(max) {
				choice[0] = i
				break
			}
		}
		for i = 24; i < 32; i++ {
			if huffmanCodeTable[i].linMax >= uint(max) {
				choice[1] = i
				break
			}
		}
		sum[0] = countBit(ix, begin, end, uint64(choice[0]))
		sum[1] = countBit(ix, begin, end, uint64(choice[1]))
		if sum[1] < sum[0] {
			choice[0] = choice[1]
		}
	}
	return choice[0]
}

// bigValuesBitCount Count the number of bits necessary to code the bigValues region.
func bigValuesBitCount(ix *[GRANULE_SIZE]int64, gi *GranuleInfo) int64 {
	var (
		bits  int64 = 0
		table uint64
	)
	if (func() uint64 {
		table = gi.TableSelect[0]
		return table
	}()) != 0 {
		bits += countBit(ix, 0, gi.Address1, table)
	}
	if (func() uint64 {
		table = gi.TableSelect[1]
		return table
	}()) != 0 {
		bits += countBit(ix, gi.Address1, gi.Address2, table)
	}
	if (func() uint64 {
		table = gi.TableSelect[2]
		return table
	}()) != 0 {
		bits += countBit(ix, gi.Address2, gi.Address3, table)
	}
	return bits
}

// countBit counts the number of bits necessary to code the subregion.
func countBit(ix *[GRANULE_SIZE]int64, start uint64, end uint64, table uint64) int64 {
	var (
		linbits uint64
		ylen    uint64
		i       int64
		sum     int64
		x       int64
		y       int64
		h       *huffCodeTableInfo
	)
	if table == 0 {
		return 0
	}
	h = &(huffmanCodeTable[table])
	sum = 0
	ylen = uint64(h.yLen)
	linbits = uint64(h.linBits)
	if table > 15 {
		for i = int64(start); uint64(i) < end; i += 2 {
			x = ix[i]
			y = ix[i+1]
			if x > 14 {
				x = 15
				sum += int64(linbits)
			}
			if y > 14 {
				y = 15
				sum += int64(linbits)
			}
			sum += int64(h.hLen[x*int64(ylen)+y])
			if x != 0 {
				sum++
			}
			if y != 0 {
				sum++
			}
		}
	} else {
		for i = int64(start); uint64(i) < end; i += 2 {
			x = ix[i]
			y = ix[i+1]
			sum += int64(h.hLen[x*int64(ylen)+y])
			if x != 0 {
				sum++
			}
			if y != 0 {
				sum++
			}
		}
	}
	return sum
}

// binSearchStepSize successively approximates an approach to obtaining a initial quantizer
// step size. The following optional code written by Seymour Shlien will speed up the shine_outer_loop code which is
// called by iteration_loop. When BIN_SEARCH is defined, the shine_outer_loop function precedes the call to the
// function shine_inner_loop with a call to bin_search gain defined below, which returns a good starting quantizerStepSize.
func (enc *Encoder) binSearchStepSize(desired_rate int64, ix *[GRANULE_SIZE]int64, cod_info *GranuleInfo) int64 {
	var (
		bit   int64
		next  int64
		count int64
	)
	next = -120
	count = 120
	for {
		{
			var half int64 = count / 2
			if enc.quantize(ix, next+half) > 8192 {
				bit = 100000
			} else {
				calcRunLength(ix, cod_info)
				bit = count1BitCount(ix, cod_info)
				enc.subDivide(cod_info)
				bigValuesTableSelect(ix, cod_info)
				bit += bigValuesBitCount(ix, cod_info)
			}
			if bit < desired_rate {
				count = half
			} else {
				next += half
				count -= half
			}
		}
		if count <= 1 {
			break
		}
	}
	return next
}
