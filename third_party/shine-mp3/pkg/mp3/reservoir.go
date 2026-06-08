// Layer3 bit reservoir: Described in C.1.5.4.2.2 of the IS
package mp3

// maxReservoirBits is called at the beginning of each granule to get the max bit
// allowance for the current granule based on reservoir size and perceptual entropy.
func (enc *Encoder) maxReservoirBits(pe *float64) int64 {
	var (
		more_bits int64
		max_bits  int64
		add_bits  int64
		over_bits int64
		mean_bits int64 = enc.meanBits
	)
	mean_bits /= enc.Wave.Channels
	max_bits = mean_bits
	if max_bits > 4095 {
		max_bits = 4095
	}
	if enc.reservoirMaxSize == 0 {
		return max_bits
	}
	more_bits = int64(*pe*3.1 - float64(mean_bits))
	add_bits = 0
	if more_bits > 100 {
		var frac int64 = (enc.reservoirSize * 6) / 10
		if frac < more_bits {
			add_bits = frac
		} else {
			add_bits = more_bits
		}
	}
	over_bits = enc.reservoirSize - (enc.reservoirMaxSize<<3)/10 - add_bits
	if over_bits > 0 {
		add_bits += over_bits
	}
	max_bits += add_bits
	if max_bits > 4095 {
		max_bits = 4095
	}
	return max_bits
}

// reservoirAdjust is called after a granule's bit allocation. It readjusts the size of
// the reservoir to reflect the granule's usage.
func (enc *Encoder) reservoirAdjust(gi *GranuleInfo) {
	enc.reservoirSize += int64(uint64(enc.meanBits/enc.Wave.Channels) - gi.Part2_3Length)
}
func (enc *Encoder) reservoirFrameEnd() {
	var (
		gi            *GranuleInfo
		gr            int64
		ch            int64
		ancillary_pad int64
		stuffingBits  int64
		over_bits     int64
		l3_side       *SideInfo = &enc.sideInfo
	)
	ancillary_pad = 0
	if enc.Wave.Channels == 2 && (enc.meanBits&1) != 0 {
		enc.reservoirSize += 1
	}
	over_bits = enc.reservoirSize - enc.reservoirMaxSize
	if over_bits < 0 {
		over_bits = 0
	}
	enc.reservoirSize -= over_bits
	stuffingBits = over_bits + ancillary_pad
	if (func() int64 {
		over_bits = enc.reservoirSize % 8
		return over_bits
	}()) != 0 {
		stuffingBits += over_bits
		enc.reservoirSize -= over_bits
	}
	if stuffingBits != 0 {
		gi = &(l3_side.Granules[0].Channels[0]).Tt
		if gi.Part2_3Length+uint64(stuffingBits) < 4095 {
			gi.Part2_3Length += uint64(stuffingBits)
		} else {
			for gr = 0; gr < enc.Mpeg.GranulesPerFrame; gr++ {
				for ch = 0; ch < enc.Wave.Channels; ch++ {
					var (
						extraBits  int64
						bitsThisGr int64
						gi         *GranuleInfo = &(l3_side.Granules[gr].Channels[ch]).Tt
					)
					if stuffingBits == 0 {
						break
					}
					extraBits = int64(4095 - gi.Part2_3Length)
					if extraBits < stuffingBits {
						bitsThisGr = extraBits
					} else {
						bitsThisGr = stuffingBits
					}
					gi.Part2_3Length += uint64(bitsThisGr)
					stuffingBits -= bitsThisGr
				}
			}
			l3_side.ReservoirDrain = stuffingBits
		}
	}
}
