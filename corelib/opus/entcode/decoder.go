/* Copyright (c) 2001-2011 Timothy B. Terriberry
   Copyright (c) 2008-2009 Xiph.Org Foundation */
/*
   Redistribution and use in source and binary forms, with or without
   modification, are permitted provided that the following conditions
   are met:

   - Redistributions of source code must retain the above copyright
   notice, this list of conditions and the following disclaimer.

   - Redistributions in binary form must reproduce the above copyright
   notice, this list of conditions and the following disclaimer in the
   documentation and/or other materials provided with the distribution.

   THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
   ``AS IS'' AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
   LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
   A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT OWNER
   OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL,
   EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT LIMITED TO,
   PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE, DATA, OR
   PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY THEORY OF
   LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT (INCLUDING
   NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE OF THIS
   SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
*/

package entcode

import (
	"unsafe"
)

// Decoder is a range decoder.
//
// This is an entropy decoder based upon \cite{Mar79}, which is itself a
// rediscovery of the FIFO arithmetic code introduced by \cite{Pas76}.
//
// It is very similar to arithmetic encoding, except that encoding is done with
// digits in any base, instead of with bits, and so it is faster when using
// larger bases (i.e.: a byte).
//
// The author claims an average waste of $\frac{1}{2}\log_b(2b)$ bits, where $b$
// is the base, longer than the theoretical optimum, but to my knowledge there
// is no published justification for this claim.
//
// This only seems true when using near-infinite precision arithmetic so that
// the process is carried out with no rounding errors.
//
// An excellent description of implementation details is available at
// http://www.arturocampos.com/ac_range.html
//
// A recent work \cite{MNW98} which proposes several changes to arithmetic
// encoding for efficiency actually re-discovers many of the principles
// behind range encoding, and presents a good theoretical analysis of them.
//
// End of stream is handled by writing out the smallest number of bits that
// ensures that the stream will be correctly decoded regardless of the value of
// any subsequent bits.
//
// Tell() can be used to determine how many bits were needed to decode
// all the symbols thus far; other data can be packed in the remaining bits of
// the input buffer.
//
//	@PHDTHESIS{Pas76,
//	  author="Richard Clark Pasco",
//	  title="Source coding algorithms for fast data compression",
//	  school="Dept. of Electrical Engineering, Stanford University",
//	  address="Stanford, CA",
//	  month=May,
//	  year=1976
//	}
//
//	@INPROCEEDINGS{Mar79,
//	 author="Martin, G.N.N.",
//	 title="Range encoding: an algorithm for removing redundancy from a digitised
//	  message",
//	 booktitle="Video & Data Recording Conference",
//	 year=1979,
//	 address="Southampton",
//	 month=Jul
//	}
//
//	@ARTICLE{MNW98,
//	 author="Alistair Moffat and Radford Neal and Ian H. Witten",
//	 title="Arithmetic Coding Revisited",
//	 journal="{ACM} Transactions on Information Systems",
//	 year=1998,
//	 volume=16,
//	 number=3,
//	 pages="256--294",
//	 month=Jul,
//	 URL="http://www.stanford.edu/class/ee398a/handouts/papers/Moffat98ArithmCoding.pdf"
//	}
type Decoder struct {
	Context
}

func (ec *Decoder) readByte() int {
	if int(ec.Offs) < int(ec.Storage) {
		v := ec.Buf[ec.Offs]
		ec.Offs++
		return int(v)
	}
	return 0
}

func (ec *Decoder) readByteFromEnd() int {
	if int(ec.End_offs) < int(ec.Storage) {
		ec.End_offs++
		return int(ec.Buf[int(ec.Storage)-int(ec.End_offs)])
	}
	return 0
}

// normalize the contents of val and rng so that rng lies entirely in the high-order symbol.
func (ec *Decoder) normalize() {
	// If the range is too small, rescale it and input some bits.
	for int(ec.Rng) <= ((1 << (32 - 1)) >> 8) {
		ec.Nbits_total += 8
		ec.Rng <<= 8
		// Use up the remaining bits from our last symbol.
		sym := ec.Rem
		// Read the next value from the input.
		ec.Rem = ec.readByte()
		// Take the rest of the bits we need from this new symbol.
		sym = (sym<<8 | ec.Rem) >> (8 - ((32-2)%8 + 1))
		// And subtract them from val, capped to be less than EC_CODE_TOP.
		ec.Val = uint32(int32(((int(ec.Val) << 8) + (^sym & ((1 << 8) - 1))) & ((1 << (32 - 1)) - 1)))
	}
}

// Init initializes the decoder.
func (ec *Decoder) Init(buf []uint8) {
	ec.Buf = buf
	ec.Storage = uint32(len(buf))
	ec.End_offs = 0
	ec.End_window = 0
	ec.Nend_bits = 0
	// This is the offset from which Tell() will subtract partial bits.
	// The final value after the normalize() call will be the same as in
	// the encoder, but we have to compensate for the bits that are added there.
	ec.Nbits_total = 32 + 1 - ((32-((32-2)%8+1))/8)*8
	ec.Offs = 0
	ec.Rng = 1 << ((32-2)%8 + 1)
	ec.Rem = ec.readByte()
	ec.Val = uint32(int32(int(ec.Rng) - 1 - (ec.Rem >> (8 - ((32-2)%8 + 1)))))
	ec.Error = 0
	// Normalize the interval.
	ec.normalize()
}

// Decode calculates the cumulative frequency for the next symbol.
// This can then be fed into the probability model to determine what that
// symbol is, and the additional frequency information required to advance to
// the next symbol.
//
// This function cannot be called more than once without a corresponding call to
// DecUpdate(), or decoding will not proceed correctly.
//
// ft: The total frequency of the symbols in the alphabet the next symbol was encoded with.
//
// Return: A cumulative frequency representing the encoded symbol. If the cumulative frequency of all the symbols
// before the one that was encoded was fl, and the cumulative frequency of all the symbols up to and including the
// one encoded is fh, then the returned value will fall in the range [fl,fh).
func (ec *Decoder) Decode(ft uint) uint {
	ec.Ext = ec.Rng / uint32(ft)
	s := uint(int(ec.Val) / int(ec.Ext))
	return ft - ((s + 1) + ((ft - (s + 1)) & uint(-int(bool2int(ft < (s+1))))))
}

// DecodeBin is an equivalent to Decode() with ft==1<<bits.
func (ec *Decoder) DecodeBin(bits uint) uint {
	ec.Ext = uint32(uint(ec.Rng) >> bits)
	s := uint(int(ec.Val) / int(ec.Ext))
	return (1 << bits) - ((s + 1) + (((1 << bits) - (s + 1)) & uint(-int(bool2int((1<<bits) < (s+1))))))
}

// DecUpdate advances the decoder past the next symbol using the frequency information the symbol was encoded with.
// Exactly one call to Decode() must have been made so that all necessary intermediate calculations are performed.
//
// fl:  The cumulative frequency of all symbols that come before the symbol decoded.
//
// fh:  The cumulative frequency of all symbols up to and including the symbol decoded.
// Together with fl, this defines the range [fl,fh) in which the value returned above must fall.
//
// ft:  The total frequency of the symbols in the alphabet the symbol decoded was encoded in.
// This must be the same as passed to the preceding call to Decode().
func (ec *Decoder) DecUpdate(fl uint, fh uint, ft uint) {
	s := uint32(uint(ec.Ext) * (ft - fh))
	ec.Val -= s
	if fl > 0 {
		ec.Rng = uint32(uint(ec.Ext) * (fh - fl))
	} else {
		ec.Rng = uint32(int32(int(ec.Rng) - int(s)))
	}
	ec.normalize()
}

// DecBitLogp decodes a bit that has a 1/(1<<logp) probability of being a one.
func (ec *Decoder) DecBitLogp(logp uint) int {
	r := ec.Rng
	d := ec.Val
	s := uint32(uint(r) >> logp)
	ret := int(bool2int(int(d) < int(s)))
	if ret == 0 {
		ec.Val = uint32(int32(int(d) - int(s)))
	}
	if ret != 0 {
		ec.Rng = s
	} else {
		ec.Rng = uint32(int32(int(r) - int(s)))
	}
	ec.normalize()
	return ret
}

// DecIcdf decodes a symbol given an "inverse" CDF table. No call to DecUpdate() is necessary after this call.
//
// icdf: The "inverse" CDF, such that symbol s falls in the range [s>0?ft-icdf[s-1]:0,ft-icdf[s]), where ft=1<<ftb.
// The values must be monotonically non-increasing, and the last value must be 0.
//
// ftb: The number of bits of precision in the cumulative distribution.
//
// Return: The decoded symbol s.
func (ec *Decoder) DecIcdf(icdf []byte, ftb uint) int {
	s := ec.Rng
	d := ec.Val
	r := uint32(uint(s) >> ftb)
	ret := -1
	var t uint32
	for {
		t = s
		s = r * uint32(icdf[func() int {
			p := &ret
			*p++
			return *p
		}()])
		if int(d) >= int(s) {
			break
		}
	}
	ec.Val = uint32(int32(int(d) - int(s)))
	ec.Rng = uint32(int32(int(t) - int(s)))
	ec.normalize()
	return ret
}

// DecUint extracts a raw unsigned integer with a non-power-of-2 range from the stream.
// The bits must have been encoded with Encoder.EncUint(). No call to DecUpdate() is necessary after this call.
//
// ft: The number of integers that can be decoded (one more than the max).
// This must be at least 2, and no more than 2**32-1.
//
// Return: The decoded bits.
func (ec *Decoder) DecUint(fta uint32) uint32 {
	fta--
	// In order to optimize EC_ILOG(), it is undefined for the value 0.
	ftb := EC_ilog(fta)
	if ftb > 8 {
		var t uint32
		ftb -= 8
		ft := uint(int(fta)>>ftb) + 1
		s := ec.Decode(ft)
		ec.DecUpdate(s, s+1, ft)
		t = uint32(int32(int(uint32(s))<<ftb | int(ec.DecBits(uint(ftb)))))
		if int(t) <= int(fta) {
			return t
		}
		ec.Error = 1
		return fta
	} else {
		fta++
		s := ec.Decode(uint(fta))
		ec.DecUpdate(s, s+1, uint(fta))
		return uint32(s)
	}
}

// DecBits extracts a sequence of raw bits from the stream.
// The bits must have been encoded with Encoder.EncBits(). No call to DecUpdate() is necessary after this call.
//
// ftb: The number of bits to extract. This must be between 0 and 25, inclusive.
//
// Return: The decoded bits.
func (ec *Decoder) DecBits(bits uint) uint32 {
	window := ec.End_window
	available := ec.Nend_bits
	if uint(available) < bits {
		for {
			window |= Window(int32(int(Window(int32(ec.readByteFromEnd()))) << available))
			available += 8
			if available > (8*int(unsafe.Sizeof(Window(0))))-8 {
				break
			}
		}
	}
	ret := uint32(uint(window) & ((1 << bits) - 1))
	window >>= Window(bits)
	available -= int(bits)
	ec.End_window = window
	ec.Nend_bits = available
	ec.Nbits_total += int(bits)
	return ret
}
