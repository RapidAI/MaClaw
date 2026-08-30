package main

import (
	"bytes"
	"image"
	"image/png"

	"github.com/RapidAI/CodeClaw/gui/petpack"
)

// tryLoadPackFrame returns a scaled frame from the selected pack. The legacy
// variant argument remains for call compatibility, but no longer selects a
// different rendering path. Shared by Windows (animated) and Linux (still).
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
