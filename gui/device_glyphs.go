package main

// The small ESP display deliberately does not carry a full CJK font.  Every
// device-bound string therefore carries only the glyphs it needs, in a compact
// 24x24 one-bit representation.  This keeps the device firmware small while
// allowing arbitrary city names and ordinary reply text to render correctly.

import (
	"encoding/base64"
	"image"
	"image/color"
	"io"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

const (
	deviceGlyphSize       = 24
	deviceGlyphBytes      = deviceGlyphSize * 3
	// The round ESP screen pages long replies locally.  Send enough glyphs for
	// several pages so later pages do not degrade to question marks.
	deviceGlyphPayloadMax = 96
)

var deviceGlyphFontLoader struct {
	sync.Once
	font *opentype.Font
	err  error
}

// deviceGlyphsForText returns the smallest set of non-ASCII glyphs necessary
// to draw the supplied strings. The ESP UI shows at most 21 glyphs per text
// line, hence the hard cap also bounds a single gateway payload.
func deviceGlyphsForText(texts ...string) map[string]string {
	need := make(map[rune]struct{})
	for _, text := range texts {
		for _, r := range text {
			if r < 0x80 || r > 0xFFFF || (r >= 0xD800 && r <= 0xDFFF) {
				continue
			}
			need[r] = struct{}{}
			if len(need) >= deviceGlyphPayloadMax {
				break
			}
		}
		if len(need) >= deviceGlyphPayloadMax {
			break
		}
	}
	if len(need) == 0 {
		return nil
	}

	face, err := deviceGlyphFace()
	if err != nil {
		return nil
	}
	defer face.Close()

	result := make(map[string]string, len(need))
	for r := range need {
		if bitmap, ok := rasterizeDeviceGlyph(face, r); ok {
			result[deviceGlyphKey(r)] = base64.StdEncoding.EncodeToString(bitmap[:])
		}
	}
	return result
}

func deviceGlyphKey(r rune) string {
	return "U+" + string([]byte{
		hexDigit(byte(r >> 12)), hexDigit(byte(r >> 8)),
		hexDigit(byte(r >> 4)), hexDigit(byte(r)),
	})
}

func hexDigit(value byte) byte {
	value &= 0x0f
	if value < 10 {
		return '0' + value
	}
	return 'A' + value - 10
}

func deviceGlyphFace() (font.Face, error) {
	deviceGlyphFontLoader.Do(func() {
		for _, name := range []string{"msyh.ttc", "simhei.ttf", "simsun.ttc"} {
			path := filepath.Join(os.Getenv("WINDIR"), "Fonts", name)
			if path == "Fonts"+string(filepath.Separator)+name {
				path = filepath.Join(`C:\Windows\Fonts`, name)
			}
			file, err := os.Open(path)
			if err != nil {
				continue
			}
			bytes, readErr := io.ReadAll(file)
			_ = file.Close()
			if readErr != nil {
				continue
			}
			collection, parseErr := opentype.ParseCollection(bytes)
			if parseErr != nil {
				continue
			}
			deviceGlyphFontLoader.font, deviceGlyphFontLoader.err = collection.Font(0)
			return
		}
		deviceGlyphFontLoader.err = os.ErrNotExist
	})
	if deviceGlyphFontLoader.err != nil {
		return nil, deviceGlyphFontLoader.err
	}
	return opentype.NewFace(deviceGlyphFontLoader.font, &opentype.FaceOptions{
		Size: 24, DPI: 72, Hinting: font.HintingFull,
	})
}

func rasterizeDeviceGlyph(face font.Face, r rune) ([deviceGlyphBytes]byte, bool) {
	var out [deviceGlyphBytes]byte
	canvas := image.NewAlpha(image.Rect(0, 0, deviceGlyphSize, deviceGlyphSize))
	metrics := face.Metrics()
	baseline := metrics.Ascent.Ceil()
	if baseline <= 0 || baseline >= deviceGlyphSize {
		baseline = deviceGlyphSize - 2
	}
	drawer := font.Drawer{
		Dst: canvas, Src: image.NewUniform(color.Alpha{A: 255}), Face: face,
		Dot: fixed.P(0, baseline),
	}
	drawer.DrawString(string(r))
	for y := 0; y < deviceGlyphSize; y++ {
		for x := 0; x < deviceGlyphSize; x++ {
			if canvas.AlphaAt(x, y).A >= 96 {
				out[y*3+x/8] |= 1 << uint(7-(x%8))
			}
		}
	}
	for _, b := range out {
		if b != 0 {
			return out, true
		}
	}
	return out, false
}
