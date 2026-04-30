package silk

const CELT_SIG_SCALE = 32768.0
const OPUS_FAST_INT64 = 1
const Q15ONE = 1.0
const NORM_SCALING = 1.0
const EPSILON = 1e-15
const VERY_SMALL = 1e-30
const VERY_LARGE16 = 1e+15
const GLOBAL_STACK_SIZE = 120000

type opus_val16 = float32
type opus_val32 = float32
type opus_val64 = float32
type celt_sig = float32
type celt_norm = float32
type celt_ener = float32

func opus_select_arch() int {
	return 0 // FIXME
}
