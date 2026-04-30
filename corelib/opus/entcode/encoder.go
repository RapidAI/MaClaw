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

	"github.com/RapidAI/CodeClaw/corelib/opus/internal/libc"
)

// Encoder is a range encoder.
//
// See Decoder and the references for implementation details \cite{Mar79,MNW98}.
//
//	@INPROCEEDINGS{Mar79,
//	 author="Martin, G.N.N.",
//	 title="Range encoding: an algorithm for removing redundancy from a digitised
//	  message",
//	 booktitle="Video \& Data Recording Conference",
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
//	 URL="http://www.stanford.edu/class/ee398/handouts/papers/Moffat98ArithmCoding.pdf"
//	}
type Encoder struct {
	Context
}

func (ec *Encoder) WriteByte(_value uint) int {
	if int(ec.Offs)+int(ec.End_offs) >= int(ec.Storage) {
		return -1
	}
	ec.Buf[func() uint32 {
		p := &ec.Offs
		x := *p
		*p++
		return x
	}()] = byte(uint8(_value))
	return 0
}
func (ec *Encoder) WriteByteAtEnd(_value uint) int {
	if int(ec.Offs)+int(ec.End_offs) >= int(ec.Storage) {
		return -1
	}
	ec.Buf[int(ec.Storage)-int(func() uint32 {
		p := &ec.End_offs
		*p++
		return *p
	}())] = byte(uint8(_value))
	return 0
}

// carryOut outputs a symbol, with a carry bit.
//
// If there is a potential to propagate a carry over several symbols, they are
// buffered until it can be determined whether or not an actual carry will occur.
//
// If the counter for the buffered symbols overflows, then the stream becomes undecodable.
//
// This gives a theoretical limit of a few billion symbols in a single packet on 32-bit systems.
//
// The alternative is to truncate the range in order to force a carry, but
// requires similar carry tracking in the decoder, needlessly slowing it down.
func (ec *Encoder) carryOut(_c int) {
	if _c != ((1 << 8) - 1) {
		var carry int
		carry = _c >> 8
		if ec.Rem >= 0 {
			ec.Error |= ec.WriteByte(uint(ec.Rem + carry))
		}
		if int(ec.Ext) > 0 {
			var sym uint
			sym = uint((carry + ((1 << 8) - 1)) & ((1 << 8) - 1))
			for {
				ec.Error |= ec.WriteByte(sym)
				if int(func() uint32 {
					p := &ec.Ext
					*p--
					return *p
				}()) <= 0 {
					break
				}
			}
		}
		ec.Rem = _c & ((1 << 8) - 1)
	} else {
		ec.Ext++
	}
}

func (ec *Encoder) normalize() {
	// If the range is too small, output some bits and rescale it.
	for int(ec.Rng) <= ((1 << (32 - 1)) >> 8) {
		ec.carryOut(int(ec.Val) >> (32 - 8 - 1))
		ec.Val = uint32(int32((int(ec.Val) << 8) & ((1 << (32 - 1)) - 1)))
		ec.Rng <<= 8
		ec.Nbits_total += 8
	}
}

// Init initializes the encoder.
func (ec *Encoder) Init(buf []byte) {
	ec.Buf = buf
	ec.End_offs = 0
	ec.End_window = 0
	ec.Nend_bits = 0
	// This is the offset from which Tell() will subtract partial bits.
	ec.Nbits_total = 32 + 1
	ec.Offs = 0
	ec.Rng = 1 << (32 - 1)
	ec.Rem = -1
	ec.Val = 0
	ec.Ext = 0
	ec.Storage = uint32(len(buf))
	ec.Error = 0
}

// Encode encodes a symbol given its frequency information.
//
// The frequency information must be discernable by the decoder, assuming it
// has read only the previous symbols from the stream.
//
// It is allowable to change the frequency information, or even the entire
// source alphabet, so long as the decoder can tell from the context of the
// previously encoded information that it is supposed to do so as well.
//
// fl: The cumulative frequency of all symbols that come before the one to be encoded.
//
// fh: The cumulative frequency of all symbols up to and including the one to be encoded.
// Together with fl, this defines the range [fl,fh) in which the decoded value will fall.
//
// ft: The sum of the frequencies of all the symbols
func (ec *Encoder) Encode(fl uint, fh uint, ft uint) {
	r := ec.Rng / uint32(ft)
	if fl > 0 {
		ec.Val += uint32(uint(ec.Rng) - uint(r)*(ft-fl))
		ec.Rng = uint32(uint(r) * (fh - fl))
	} else {
		ec.Rng -= uint32(uint(r) * (ft - fh))
	}
	ec.normalize()
}

// EncodeBin is an equivalent to Encode() with ft==1<<bits.
func (ec *Encoder) EncodeBin(fl uint, fh uint, bits uint) {
	r := uint32(uint(ec.Rng) >> bits)
	if fl > 0 {
		ec.Val += uint32(uint(ec.Rng) - uint(r)*((1<<bits)-fl))
		ec.Rng = uint32(uint(r) * (fh - fl))
	} else {
		ec.Rng -= uint32(uint(r) * ((1 << bits) - fh))
	}
	ec.normalize()
}

// EncBitLogp encodes a bit that has a 1/(1<<logp) probability of being a one.
func (ec *Encoder) EncBitLogp(val int, logp uint) {
	r := ec.Rng
	l := ec.Val
	s := uint32(uint(r) >> logp)
	r -= s
	if val != 0 {
		ec.Val = uint32(int32(int(l) + int(r)))
	}
	if val != 0 {
		ec.Rng = s
	} else {
		ec.Rng = r
	}
	ec.normalize()
}

// EncIcdf encodes a symbol given an "inverse" CDF table.
//
// s:    The index of the symbol to encode.
//
// icdf: The "inverse" CDF, such that symbol _s falls in the range [s>0?ft-icdf[s-1]:0,ft-icdf[s]), where ft=1<<ftb.
// The values must be monotonically non-increasing, and the last value must be 0.
//
// ftb: The number of bits of precision in the cumulative distribution.
func (ec *Encoder) EncIcdf(s int, icdf []byte, ftb uint) {
	var r uint32
	r = uint32(uint(ec.Rng) >> ftb)
	if s > 0 {
		ec.Val += uint32(int32(int(ec.Rng) - int(r*uint32(icdf[s-1]))))
		ec.Rng = r * uint32(icdf[s-1]-icdf[s])
	} else {
		ec.Rng -= r * uint32(icdf[s])
	}
	ec.normalize()
}

// EncUint encodes a raw unsigned integer in the stream.
//
// fl: The integer to encode.
//
// ft: The number of integers that can be encoded (one more than the max).
// This must be at least 2, and no more than 2**32-1.
func (ec *Encoder) EncUint(_fl uint32, _ft uint32) {
	_ft--
	// In order to optimize EC_ILOG(), it is undefined for the value 0.
	ftb := EC_ilog(_ft)
	if ftb > 8 {
		ftb -= 8
		ft := uint((int(_ft) >> ftb) + 1)
		fl := uint(int(_fl) >> ftb)
		ec.Encode(fl, fl+1, ft)
		ec.EncBits(uint32(int32(int(_fl)&((1<<ftb)-1))), uint(ftb))
	} else {
		ec.Encode(uint(_fl), uint(int(_fl)+1), uint(int(_ft)+1))
	}
}

// EncBits encodes a sequence of raw bits in the stream.
//
// fl:  The bits to encode.
//
// ftb: The number of bits to encode. This must be between 1 and 25, inclusive.
func (ec *Encoder) EncBits(_fl uint32, _bits uint) {
	var (
		window Window
		used   int
	)
	window = ec.End_window
	used = ec.Nend_bits
	if used+int(_bits) > (8 * int(unsafe.Sizeof(Window(0)))) {
		for {
			ec.Error |= ec.WriteByteAtEnd(uint(window) & ((1 << 8) - 1))
			window >>= 8
			used -= 8
			if used < 8 {
				break
			}
		}
	}
	window |= Window(int32(int(Window(_fl)) << used))
	used += int(_bits)
	ec.End_window = window
	ec.Nend_bits = used
	ec.Nbits_total += int(_bits)
}

// EncPatchInitialBits overwrites a few bits at the very start of an existing stream, after they have already been encoded.
//
// This makes it possible to have a few flags up front, where it is easy for decoders to access them without parsing
// the whole stream, even if their values are not determined until late in the encoding process, without having
// to buffer all the intermediate symbols in the encoder.
//
// In order for this to work, at least nbits bits must have already been encoded using probabilities that are an exact
// power of two. The encoder can verify the number of encoded bits is sufficient, but cannot check this latter condition.
//
// val:   The bits to encode (in the least nbits significant bits). They will be decoded in order from most-significant to least.
//
// nbits: The number of bits to overwrite. This must be no more than 8.
func (ec *Encoder) EncPatchInitialBits(val uint, nbits uint) {
	shift := int(8 - nbits)
	mask := uint((1<<nbits)-1) << uint(shift)
	if int(ec.Offs) > 0 {
		// The first byte has been finalized.
		ec.Buf[0] = byte(uint8((uint(ec.Buf[0]) & ^mask) | val<<uint(shift)))
	} else if ec.Rem >= 0 {
		// The first byte is still awaiting carry propagation.
		ec.Rem = (ec.Rem & int(^mask)) | int(val<<uint(shift))
	} else if uint(ec.Rng) <= ((1 << (32 - 1)) >> nbits) {
		// The renormalization loop has never been run.
		ec.Val = uint32(int32((int(ec.Val) & int(uint32(int32(^(int(uint32(mask)) << (32 - 8 - 1)))))) | int(uint32(val))<<(shift+(32-8-1))))
	} else {
		// The encoder hasn't even encoded nbits of data yet.
		ec.Error = -1
	}
}

// Shrink compacts the data to fit in the target size.
//
// This moves up the raw bits at the end of the current buffer so they are at the end of the new buffer size.
//
// The caller must ensure that the amount of data that's already been written will fit in the new size.
//
// size: The number of bytes in the new buffer. This must be large enough to contain the bits already written, and
// must be no larger than the existing size.
func (ec *Encoder) Shrink(_size uint32) {
	// Use unsafe pointer arithmetic to handle the case where _size == ec.Storage == len(ec.Buf).
	// In C, buf+size is valid pointer arithmetic even at the end of the buffer.
	dst := unsafe.Add(unsafe.Pointer(&ec.Buf[0]), int(_size)-int(ec.End_offs))
	src := unsafe.Add(unsafe.Pointer(&ec.Buf[0]), int(ec.Storage)-int(ec.End_offs))
	libc.MemMove(dst, src, int(ec.End_offs))
	ec.Storage = _size
}

// Done indicates that there are no more symbols to encode. All remaining output bytes are flushed to the output buffer.
// Init() must be called before the encoder can be used again.
func (ec *Encoder) Done() {
	var (
		window Window
		used   int
		msk    uint32
		end    uint32
		l      int
	)
	// We output the minimum number of bits that ensures that the symbols encoded
	// thus far will be decoded correctly regardless of the bits that follow.
	l = 32 - EC_ilog(ec.Rng)
	msk = uint32(int32(((1 << (32 - 1)) - 1) >> l))
	end = uint32(int32((int(ec.Val) + int(msk)) & int(^msk)))
	if (int(end) | int(msk)) >= int(ec.Val)+int(ec.Rng) {
		l++
		msk >>= 1
		end = uint32(int32((int(ec.Val) + int(msk)) & int(^msk)))
	}
	for l > 0 {
		ec.carryOut(int(end) >> (32 - 8 - 1))
		end = uint32(int32((int(end) << 8) & ((1 << (32 - 1)) - 1)))
		l -= 8
	}
	// If we have a buffered byte flush it into the output buffer.
	if ec.Rem >= 0 || int(ec.Ext) > 0 {
		ec.carryOut(0)
	}
	// If we have buffered extra bits, flush them as well.
	window = ec.End_window
	used = ec.Nend_bits
	for used >= 8 {
		ec.Error |= ec.WriteByteAtEnd(uint(window) & ((1 << 8) - 1))
		window >>= 8
		used -= 8
	}
	// Clear any excess space and add any remaining extra bits to the last byte.
	if ec.Error == 0 {
		clearLen := int(ec.Storage) - int(ec.Offs) - int(ec.End_offs)
		if clearLen > 0 {
			libc.MemSet(unsafe.Pointer(&ec.Buf[ec.Offs]), 0, clearLen*int(unsafe.Sizeof(byte(0))))
		}
		if used > 0 {
			// If there's no range coder data at all, give up.
			if int(ec.End_offs) >= int(ec.Storage) {
				ec.Error = -1
			} else {
				l = -l
				// If we've busted, don't add too many extra bits to the last byte; it
				// would corrupt the range coder data, and that's more important.
				if int(ec.Offs)+int(ec.End_offs) >= int(ec.Storage) && l < used {
					window &= Window(int32((1 << l) - 1))
					ec.Error = -1
				}
				ec.Buf[int(ec.Storage)-int(ec.End_offs)-1] |= byte(uint8(window))
			}
		}
	}
}
