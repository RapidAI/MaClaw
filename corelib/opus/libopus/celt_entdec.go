package libopus

import (
	"unsafe"

	"github.com/RapidAI/CodeClaw/corelib/opus/entcode"
)

type ec_dec = entcode.Decoder

func ec_dec_init(_this *ec_dec, _buf *uint8, _storage uint32) {
	_this.Init(unsafe.Slice(_buf, _storage))
}
func ec_decode(_this *ec_dec, _ft uint) uint {
	return _this.Decode(_ft)
}
func ec_decode_bin(_this *ec_dec, _bits uint) uint {
	return _this.DecodeBin(_bits)
}
func ec_dec_update(_this *ec_dec, _fl uint, _fh uint, _ft uint) {
	_this.DecUpdate(_fl, _fh, _ft)
}
func ec_dec_bit_logp(_this *ec_dec, _logp uint) int {
	return _this.DecBitLogp(_logp)
}
func ec_dec_icdf(_this *ec_dec, _icdf []byte, _ftb uint) int {
	return _this.DecIcdf(_icdf, _ftb)
}
func ec_dec_uint(_this *ec_dec, _ft uint32) uint32 {
	return _this.DecUint(_ft)
}
func ec_dec_bits(_this *ec_dec, _bits uint) uint32 {
	return _this.DecBits(_bits)
}
