package libopus

import (
	"github.com/RapidAI/CodeClaw/corelib/opus/entcode"
)

const EC_UINT_BITS = entcode.EC_UINT_BITS
const BITRES = entcode.BITRES

type ec_ctx = entcode.Context

func celt_udiv(n uint32, d uint32) uint32 {
	return n / d
}
func celt_sudiv(n int32, d int32) int32 {
	return n / d
}
func ec_ilog(v uint32) int {
	return entcode.EC_ilog(v)
}
func ec_range_bytes(ec *ec_ctx) uint32 {
	return ec.RangeBytes()
}
func ec_get_buffer(ec *ec_ctx) []byte {
	return ec.GetBuffer()
}
func ec_get_error(ec *ec_ctx) int {
	return ec.GetError()
}
func ec_tell(ec *ec_ctx) int {
	return ec.Tell()
}
func ec_tell_frac(ec *ec_ctx) uint32 {
	return ec.TellFrac()
}
