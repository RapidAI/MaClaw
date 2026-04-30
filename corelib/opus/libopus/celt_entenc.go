package libopus

import (
	"unsafe"

	"github.com/RapidAI/CodeClaw/corelib/opus/entcode"
)

type ec_enc = entcode.Encoder

func ec_enc_init(_this *ec_enc, _buf *uint8, _size uint32) {
	_this.Init(unsafe.Slice(_buf, _size))
}
func ec_encode(_this *ec_enc, _fl uint, _fh uint, _ft uint) {
	_this.Encode(_fl, _fh, _ft)
}
func ec_encode_bin(_this *ec_enc, _fl uint, _fh uint, _bits uint) {
	_this.EncodeBin(_fl, _fh, _bits)
}
func ec_enc_bit_logp(_this *ec_enc, _val int, _logp uint) {
	_this.EncBitLogp(_val, _logp)
}
func ec_enc_icdf(_this *ec_enc, _s int, _icdf []byte, _ftb uint) {
	_this.EncIcdf(_s, _icdf, _ftb)
}
func ec_enc_uint(_this *ec_enc, _fl uint32, _ft uint32) {
	_this.EncUint(_fl, _ft)
}
func ec_enc_bits(_this *ec_enc, _fl uint32, _bits uint) {
	_this.EncBits(_fl, _bits)
}
func ec_enc_patch_initial_bits(_this *ec_enc, _val uint, _nbits uint) {
	_this.EncPatchInitialBits(_val, _nbits)
}
func ec_enc_shrink(_this *ec_enc, _size uint32) {
	_this.Shrink(_size)
}
func ec_enc_done(_this *ec_enc) {
	_this.Done()
}
