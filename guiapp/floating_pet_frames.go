package guiapp

import (
	"bytes"
	"image"
	"image/png"

	"github.com/RapidAI/CodeClaw/guiapp/petpack"
)

// tryLoadPackFrame returns a scaled frame from the selected pack. The legacy
// variant argument remains for call compatibility, but no longer selects a
// different rendering path. Shared by Windows (animated) and Linux/macOS (still).
func tryLoadPackFrame(skin, variant, state string, size int, cache *petpack.FrameCache) *image.NRGBA {
	reg := petpack.EnsureGlobal()
	if reg == nil {
		return nil
	}
	st := petpack.NormalizeState(state)
	frame, _, err := reg.ResolveAndLoad(skin, petpack.VariantDefault, st, size, cache)
	if err != nil || frame == nil {
		return nil
	}
	return frame
}

func encodeNRGBAToPNG(img *image.NRGBA) []byte {
	if img == nil {
		return nil
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil
	}
	return buf.Bytes()
}

// desktopPetStillPNG is the static pet face used where the Windows animation
// loop is not available. A pack frame wins; logoFallback is only the last resort.
func desktopPetStillPNG(skin, variant, state string, size int, cache *petpack.FrameCache, logoFallback []byte) []byte {
	sz := size
	if sz < 56 {
		sz = defaultPetSize
	}
	if frame := tryLoadPackFrame(skin, variant, state, sz, cache); frame != nil {
		if b := encodeNRGBAToPNG(frame); len(b) > 0 {
			return b
		}
	}
	if len(logoFallback) > 0 {
		return logoFallback
	}
	return nil
}
