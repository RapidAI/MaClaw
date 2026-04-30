/* Copyright (c) 2001-2011 Timothy B. Terriberry
 */
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

import "math/bits"

// EC_UINT_BITS is the number of bits to use for the range-coded part of unsigned integers.
const EC_UINT_BITS = 8

// BITRES is the resolution of fractional-precision bit usage measurements, i.e., 3 => 1/8th bits.
const BITRES = 3

// Window must be at least 32 bits, but if you have fast arithmetic on a
// larger type, you can speed up the decoder by using it here.
type Window uint32

// Context for the entropy encoder/decoder.
//
// We use the same structure for both, so that common functions like Tell() can be used on either one.
type Context struct {
	Buf     []byte // Buffered input/output.
	Storage uint32 // The size of the buffer.
	// End_offs is the offset at which the last byte containing raw bits was read/written.
	End_offs uint32
	// End_window contains bits that will be read from/written at the end.
	End_window Window
	// Nend_bits is the number of valid bits in End_window.
	Nend_bits int
	// Nbits_total is the total number of whole bits read/written.
	// This does not include partial bits currently in the range coder.
	Nbits_total int
	Offs        uint32 // The offset at which the next range coder byte will be read/written.
	Rng         uint32 // The number of values in the current range.
	// Val meaning is different in encoder/decoder.
	//
	// In the Decoder: the difference between the top of the current range and the input value, minus one.
	//
	// In the Encoder: the low end of the current range.
	Val uint32
	// Ext meaning is different in encoder/decoder.
	//
	// In the Decoder: the saved normalization factor from Decode().
	//
	// In the Encoder: the number of outstanding carry propagating symbols.
	Ext   uint32
	Rem   int // A buffered input/output symbol, awaiting carry propagation.
	Error int // Nonzero if an error occurred.
}

func EC_ilog(v uint32) int {
	return 32 - bits.LeadingZeros32(v) // TODO: check
}

func (ec *Context) RangeBytes() uint32 {
	return ec.Offs
}

func (ec *Context) GetBuffer() []byte {
	return ec.Buf
}

func (ec *Context) GetError() int {
	return ec.Error
}

// Tell returns the number of bits "used" by the encoded or decoded symbols so far.
// This same number can be computed in either the encoder or the decoder, and is
// suitable for making coding decisions.
//
// Return: The number of bits. This will always be slightly larger than the exact value (e.g., all
// rounding error is in the positive direction).
func (ec *Context) Tell() int {
	return ec.Nbits_total - EC_ilog(ec.Rng)
}

// TellFrac returns the number of bits "used" by the encoded or decoded symbols so far.
// This same number can be computed in either the encoder or the decoder, and is
// suitable for making coding decisions.
//
// Return: The number of bits scaled by 2**BITRES. This will always be slightly larger than the exact value (e.g., all
// rounding error is in the positive direction).
func (ec *Context) TellFrac() uint32 {
	var correction = [8]uint{35733, 38967, 42495, 46340, 50535, 55109, 60097, 65535}
	nbits := ec.Nbits_total << BITRES
	l := EC_ilog(ec.Rng)
	r := uint32(int32(int(ec.Rng) >> (l - 16)))
	b := uint((int(r) >> 12) - 8)
	if uint(r) > correction[b] {
		b++
	}
	l = (l << 3) + int(b)
	return uint32(int32(nbits - l))
}

func bool2int(v bool) int {
	if v {
		return 1
	}
	return 0
}
