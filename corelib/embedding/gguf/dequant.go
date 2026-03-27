package gguf

import "encoding/binary"

// dequantQ8_0 dequantizes Q8_0 blocks: each block is 32 int8 values with a f16 scale.
// Block layout: [scale:f16][d0..d31:int8] = 34 bytes per 32 elements.
func dequantQ8_0(data []byte, out []float32) {
	const blockSize = 32
	const blockBytes = 2 + blockSize // f16 scale + 32 int8
	nBlocks := len(out) / blockSize
	for b := 0; b < nBlocks; b++ {
		off := b * blockBytes
		scale := float16to32(binary.LittleEndian.Uint16(data[off:]))
		for i := 0; i < blockSize; i++ {
			out[b*blockSize+i] = scale * float32(int8(data[off+2+i]))
		}
	}
}

// dequantQ4_0 dequantizes Q4_0 blocks: each block is 32 4-bit values with a f16 scale.
// Block layout: [scale:f16][nibbles:16 bytes] = 18 bytes per 32 elements.
// Each nibble is unsigned 0-15, centered by subtracting 8.
func dequantQ4_0(data []byte, out []float32) {
	const blockSize = 32
	const blockBytes = 2 + 16 // f16 scale + 16 bytes of nibbles
	nBlocks := len(out) / blockSize
	for b := 0; b < nBlocks; b++ {
		off := b * blockBytes
		scale := float16to32(binary.LittleEndian.Uint16(data[off:]))
		for i := 0; i < 16; i++ {
			byte_ := data[off+2+i]
			lo := int(byte_&0x0f) - 8
			hi := int(byte_>>4) - 8
			out[b*blockSize+i] = scale * float32(lo)
			out[b*blockSize+16+i] = scale * float32(hi)
		}
	}
}

// GetMetaI32 returns an int32 metadata value with a default fallback.
func GetMetaI32(meta map[string]MetaValue, key string, def int) int {
	v, ok := meta[key]
	if !ok {
		return def
	}
	// Prefer I32 for signed types (5=INT32, 1=INT8, 3=INT16, 11=INT64)
	switch v.Type {
	case 5, 1, 3, 11:
		return int(v.I32)
	default:
		return int(v.U32)
	}
}

// GetMetaF32 returns a float32 metadata value with a default fallback.
func GetMetaF32(meta map[string]MetaValue, key string, def float32) float32 {
	v, ok := meta[key]
	if !ok {
		return def
	}
	return v.F32
}

// GetMetaStr returns a string metadata value.
func GetMetaStr(meta map[string]MetaValue, key string) string {
	if v, ok := meta[key]; ok {
		return v.Str
	}
	return ""
}

// GetMetaStrArr returns a string array metadata value.
func GetMetaStrArr(meta map[string]MetaValue, key string) []string {
	if v, ok := meta[key]; ok {
		return v.Arr
	}
	return nil
}
