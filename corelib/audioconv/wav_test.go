package audioconv

import (
	"math"
	"testing"
)

// Downsampling must suppress content above the destination Nyquist instead of
// folding it into the audible band. A 10 kHz tone in a 24 kHz source aliases
// to 6 kHz under plain linear interpolation; the anti-aliased resampler must
// attenuate it into silence.
func TestResampleS16SuppressesAliasedContent(t *testing.T) {
	const srcRate = 24000
	const dstRate = 16000
	src := make([]byte, srcRate*2) // one second
	for i := 0; i < srcRate; i++ {
		v := int16(math.Sin(2*math.Pi*10000*float64(i)/srcRate) * 20000)
		writeS16LE(src, i, v)
	}
	out := resampleS16(src, srcRate, dstRate)
	if len(out) != dstRate*2 {
		t.Fatalf("resampled length = %d bytes, want %d", len(out), dstRate*2)
	}
	peak := 0
	for i := 512; i < dstRate-512; i++ { // skip the filter's edge transients
		v := int(readS16LE(out, i))
		if v < 0 {
			v = -v
		}
		if v > peak {
			peak = v
		}
	}
	if peak > 400 { // >34 dB below the 20000-amplitude source
		t.Fatalf("aliased 10 kHz tone leaked through at peak %d", peak)
	}
}

// In-band content must survive the same resample essentially intact.
func TestResampleS16KeepsInBandContent(t *testing.T) {
	const srcRate = 24000
	const dstRate = 16000
	src := make([]byte, srcRate*2)
	for i := 0; i < srcRate; i++ {
		v := int16(math.Sin(2*math.Pi*4000*float64(i)/srcRate) * 20000)
		writeS16LE(src, i, v)
	}
	out := resampleS16(src, srcRate, dstRate)
	if len(out) != dstRate*2 {
		t.Fatalf("resampled length = %d bytes, want %d", len(out), dstRate*2)
	}
	peak := 0
	for i := 512; i < dstRate-512; i++ {
		v := int(readS16LE(out, i))
		if v < 0 {
			v = -v
		}
		if v > peak {
			peak = v
		}
	}
	if peak < 16000 {
		t.Fatalf("in-band 4 kHz tone lost too much level: peak %d", peak)
	}
}
