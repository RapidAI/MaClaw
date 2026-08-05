package main

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/draw"
	_ "image/png"
	"math"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/gui/petpack"
	xdraw "golang.org/x/image/draw"
)

// Eight 256px RGB565+A8 frames are sent by media reference. Bundled pet packs are
// authored at 256px, so preserving that resolution avoids first shrinking to
// 128px and then magnifying to 220px on the LCD.
const (
	devicePetAssetWidth  = 256
	devicePetAssetHeight = 256
	devicePetFrameCount  = 8
	devicePetFrameMS     = 450
)

type devicePetAsset struct {
	Encoding string   `json:"encoding,omitempty"`
	Width    int      `json:"width,omitempty"`
	Height   int      `json:"height,omitempty"`
	Data     string   `json:"data,omitempty"`
	Frames   []string `json:"frames,omitempty"`
	FrameMS  int      `json:"frameMs,omitempty"`
}

func (a *App) devicePetAssetForConfig(cfg corelib.AppConfig) devicePetAsset {
	reg := petpack.EnsureGlobal()
	if reg == nil {
		return devicePetAsset{}
	}
	packID := petpack.SanitizeSkinID(cfg.PetSkin, false, nil)
	if packID == "" {
		packID = petpack.DefaultPackID
	}
	variant := petpack.ResolveVariantForRuntime(cfg.PetVariant)
	frames := devicePetRenderedFrames(reg, packID, variant)
	if len(frames) == 0 {
		raw, _, err := reg.LoadStateFrameBytes(packID, "idle", variant)
		if err != nil || len(raw) == 0 {
			return devicePetAsset{}
		}
		fallback, _, err := image.Decode(bytes.NewReader(raw))
		if err != nil || fallback == nil {
			return devicePetAsset{}
		}
		frames = []image.Image{fallback}
	}
	frames = ensureDevicePetMotionFrames(frames)
	encoded := make([]string, 0, len(frames))
	for _, frame := range frames {
		if pixels := devicePetRGB565A8(frame); len(pixels) > 0 {
			encoded = append(encoded, base64.StdEncoding.EncodeToString(pixels))
		}
	}
	if len(encoded) == 0 {
		return devicePetAsset{}
	}
	asset := devicePetAsset{Encoding: "rgb565a8", Width: devicePetAssetWidth, Height: devicePetAssetHeight, Data: encoded[0], FrameMS: devicePetFrameMS}
	if len(encoded) > 1 {
		asset.Frames = encoded[1:]
	}
	return asset
}

func devicePetRenderedFrames(reg *petpack.Registry, packID, variant string) []image.Image {
	resolved, err := reg.Resolve(packID, variant)
	if err != nil || resolved == nil {
		return nil
	}
	frames := make([]image.Image, 0, devicePetFrameCount)
	switch resolved.Renderer {
	case petpack.RendererCharacter:
		renderer, renderErr := petpack.NewCharacterRenderer(resolved)
		if renderErr != nil || renderer == nil {
			return nil
		}
		// Match the PC renderer's continuously advancing clock. Two widely spaced
		// keyframes made articulated motion collapse into a mechanical crossfade.
		// Prime the state machine at time zero, then discard the authored entry
		// transition and sample the complete idle loop.
		_ = renderer.RenderState(petpack.StateIdle, 0, devicePetAssetWidth)
		for index := 0; index < devicePetFrameCount; index++ {
			// Skip the authored entry transition and sample one complete 3.6 s
			// breathing loop so the last-to-first device segment is continuous.
			elapsed := int64(500 + index*devicePetFrameMS)
			if rendered := renderer.RenderState(petpack.StateIdle, elapsed, devicePetAssetWidth); rendered != nil {
				frames = append(frames, rendered)
			}
		}
	case petpack.RendererSkeleton:
		renderer, renderErr := petpack.NewRigRenderer(resolved)
		if renderErr != nil || renderer == nil {
			return nil
		}
		for frame := 0; frame < devicePetFrameCount; frame++ {
			elapsed := frame * devicePetFrameMS
			if frame := renderer.Render(petpack.StateIdle, elapsed, devicePetAssetWidth); frame != nil {
				frames = append(frames, frame)
			}
		}
	case petpack.RendererProcedural:
		// The desktop fallback animates native/procedural packs with the same
		// articulated pose used by the floating pet. Export two phases so hardware
		// does not silently collapse these selected packs to one static raster.
		for frame := 0; frame < devicePetFrameCount; frame++ {
			phase := 2 * math.Pi * float64(frame) / devicePetFrameCount
			frames = append(frames, renderClawMatePetWithPose(
				devicePetAssetWidth, packID, petFacePoseForPhase(phase, "balanced")))
		}
	}
	return frames
}

func ensureDevicePetMotionFrames(frames []image.Image) []image.Image {
	if len(frames) == 0 || frames[0] == nil {
		return frames
	}
	if len(frames) == 1 {
		motion := devicePetMotionFrame(frames[0])
		sequence := make([]image.Image, 0, devicePetFrameCount)
		for frame := 0; frame < devicePetFrameCount; frame++ {
			phase := float64(frame) / float64(devicePetFrameCount)
			if phase <= 0.5 {
				sequence = append(sequence, blendDevicePetFrames(frames[0], motion, phase*2))
			} else {
				sequence = append(sequence, blendDevicePetFrames(motion, frames[0], (phase-0.5)*2))
			}
		}
		return sequence
	}
	first := devicePetRGB565A8(frames[0])
	second := devicePetRGB565A8(frames[1])
	if len(first) > 0 && bytes.Equal(first, second) {
		frames[1] = devicePetMotionFrame(frames[0])
	}
	if len(frames) > devicePetFrameCount {
		frames = frames[:devicePetFrameCount]
	}
	return frames
}

func blendDevicePetFrames(first, second image.Image, mix float64) image.Image {
	if first == nil || second == nil || mix <= 0 {
		return first
	}
	if mix >= 1 {
		return second
	}
	bounds := first.Bounds()
	if second.Bounds().Dx() != bounds.Dx() || second.Bounds().Dy() != bounds.Dy() {
		return first
	}
	frame := image.NewNRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	for y := 0; y < bounds.Dy(); y++ {
		for x := 0; x < bounds.Dx(); x++ {
			fr, fg, fb, fa := straightDevicePetRGBA(first.At(bounds.Min.X+x, bounds.Min.Y+y))
			sr, sg, sb, sa := straightDevicePetRGBA(second.At(second.Bounds().Min.X+x, second.Bounds().Min.Y+y))
			lerp := func(a, b uint32) uint8 { return uint8((float64(a)*(1-mix)+float64(b)*mix)/257 + 0.5) }
			frame.SetNRGBA(x, y, color.NRGBA{R: lerp(fr, sr), G: lerp(fg, sg), B: lerp(fb, sb), A: lerp(fa, sa)})
		}
	}
	return frame
}

func straightDevicePetRGBA(value color.Color) (r, g, b, a uint32) {
	r, g, b, a = value.RGBA()
	if a != 0 && a != 0xffff {
		r = r * 0xffff / a
		g = g * 0xffff / a
		b = b * 0xffff / a
	}
	return r, g, b, a
}
func devicePetMotionFrame(source image.Image) image.Image {
	if source == nil || source.Bounds().Dx() < 1 || source.Bounds().Dy() < 1 {
		return source
	}
	bounds := source.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	frame := image.NewNRGBA(image.Rect(0, 0, width, height))
	inset := width / 64
	if inset < 1 {
		inset = 1
	}
	lift := height / 48
	if lift < 1 {
		lift = 1
	}
	destination := image.Rect(inset, 0, width-inset, height-lift)
	if destination.Dx() < 1 || destination.Dy() < 1 {
		draw.Draw(frame, frame.Bounds(), source, bounds.Min, draw.Over)
		return frame
	}
	xdraw.CatmullRom.Scale(frame, destination, source, bounds, draw.Over, nil)
	return frame
}

func devicePetRGB565A8(src image.Image) []byte {
	if src == nil || src.Bounds().Dx() < 1 || src.Bounds().Dy() < 1 {
		return nil
	}
	bounds := src.Bounds()
	// RGB565A8 keeps the pet pack's alpha instead of baking the GUI's blue idle
	// color into every pixel. Hardware can therefore composite the character on
	// its own clock/weather surface without showing an opaque rectangle.
	canvas := image.NewNRGBA(image.Rect(0, 0, devicePetAssetWidth, devicePetAssetHeight))
	scaleX := float64(devicePetAssetWidth) / float64(bounds.Dx())
	scaleY := float64(devicePetAssetHeight) / float64(bounds.Dy())
	scale := scaleX
	if scaleY < scale {
		scale = scaleY
	}
	drawW, drawH := int(float64(bounds.Dx())*scale+0.5), int(float64(bounds.Dy())*scale+0.5)
	offX, offY := (devicePetAssetWidth-drawW)/2, (devicePetAssetHeight-drawH)/2
	// Resample once on the desktop with a high-quality filter. The previous
	// nearest-neighbour lookup threw away edge detail before RGB565 conversion,
	// which made diagonals and antialiased outlines look jagged on the LCD even
	// when the source pack itself was a native 256px asset.
	xdraw.CatmullRom.Scale(canvas, image.Rect(offX, offY, offX+drawW, offY+drawH),
		src, bounds, draw.Over, nil)
	pixels := make([]byte, devicePetAssetWidth*devicePetAssetHeight*3)
	for y := 0; y < devicePetAssetHeight; y++ {
		for x := 0; x < devicePetAssetWidth; x++ {
			r, g, b, alpha := canvas.At(x, y).RGBA()
			// RGBA returns premultiplied channels. Store straight RGB alongside
			// alpha so the ESP can blend antialiased edges against its live UI.
			if alpha != 0 && alpha != 0xffff {
				r = r * 0xffff / alpha
				g = g * 0xffff / alpha
				b = b * 0xffff / alpha
			}
			value := uint16(((r >> 11) << 11) | ((g >> 10) << 5) | (b >> 11))
			i := (y*devicePetAssetWidth + x) * 3
			pixels[i], pixels[i+1], pixels[i+2] = byte(value), byte(value>>8), byte(alpha>>8)
		}
	}
	return pixels
}

// devicePetRGB565 is retained as the package-local compatibility name used by
// older tests and callers; the transport now includes alpha in the third byte.
func devicePetRGB565(src image.Image) []byte {
	return devicePetRGB565A8(src)
}

func (a *App) devicePetProfileForConfig(cfg corelib.AppConfig) map[string]any {
	motionEnabled := cfg.PetMotionEnabled == nil || *cfg.PetMotionEnabled
	return map[string]any{"skin": cfg.PetSkin, "motionEnabled": motionEnabled, "asset": a.devicePetAssetForConfig(cfg)}
}

// devicePetProfileChanged reports whether a config update changes anything
// rendered by paired hardware. Keeping this separate from the floating-pet
// predicates avoids sending a large asset again for unrelated sound, size, or
// interaction settings.
func devicePetProfileChanged(oldConfig, newConfig corelib.AppConfig) bool {
	oldMotionEnabled := oldConfig.PetMotionEnabled == nil || *oldConfig.PetMotionEnabled
	newMotionEnabled := newConfig.PetMotionEnabled == nil || *newConfig.PetMotionEnabled
	return oldConfig.PetSkin != newConfig.PetSkin ||
		oldConfig.PetVariant != newConfig.PetVariant ||
		oldMotionEnabled != newMotionEnabled
}
