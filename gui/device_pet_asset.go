package main

import (
	"bytes"
	"encoding/base64"
	"image"
	_ "image/png"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/gui/petpack"
)

// Two 128px RGB565 frames fit a handshake response without crowding the ESP's
// internal heap. They come from the GUI's own character renderer, not a
// hand-drawn MCU substitute.
const (
	devicePetAssetWidth  = 128
	devicePetAssetHeight = 128
	devicePetIdleBGRed   = 28
	devicePetIdleBGGreen = 82
	devicePetIdleBGBlue  = 133
)

type devicePetAsset struct {
	Encoding string   `json:"encoding,omitempty"`
	Width    int      `json:"width,omitempty"`
	Height   int      `json:"height,omitempty"`
	Data     string   `json:"data,omitempty"`
	Frames   []string `json:"frames,omitempty"`
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
	encoded := make([]string, 0, len(frames))
	for _, frame := range frames {
		if pixels := devicePetRGB565(frame); len(pixels) > 0 {
			encoded = append(encoded, base64.StdEncoding.EncodeToString(pixels))
		}
	}
	if len(encoded) == 0 {
		return devicePetAsset{}
	}
	asset := devicePetAsset{Encoding: "rgb565le", Width: devicePetAssetWidth, Height: devicePetAssetHeight, Data: encoded[0]}
	if len(encoded) > 1 {
		asset.Frames = encoded[1:]
	}
	return asset
}

func devicePetRenderedFrames(reg *petpack.Registry, packID, variant string) []image.Image {
	resolved, err := reg.Resolve(packID, variant)
	if err != nil || resolved == nil || resolved.Renderer != petpack.RendererCharacter {
		return nil
	}
	renderer, err := petpack.NewCharacterRenderer(resolved)
	if err != nil || renderer == nil {
		return nil
	}
	frames := make([]image.Image, 0, 2)
	for _, elapsed := range []int64{0, 700} {
		if frame := renderer.RenderState(petpack.StateIdle, elapsed, devicePetAssetWidth); frame != nil {
			frames = append(frames, frame)
		}
	}
	return frames
}

func devicePetRGB565(src image.Image) []byte {
	if src == nil || src.Bounds().Dx() < 1 || src.Bounds().Dy() < 1 {
		return nil
	}
	bounds := src.Bounds()
	pixels := make([]byte, devicePetAssetWidth*devicePetAssetHeight*2)
	scaleX := float64(devicePetAssetWidth) / float64(bounds.Dx())
	scaleY := float64(devicePetAssetHeight) / float64(bounds.Dy())
	scale := scaleX
	if scaleY < scale {
		scale = scaleY
	}
	drawW, drawH := int(float64(bounds.Dx())*scale+0.5), int(float64(bounds.Dy())*scale+0.5)
	offX, offY := (devicePetAssetWidth-drawW)/2, (devicePetAssetHeight-drawH)/2
	for y := 0; y < devicePetAssetHeight; y++ {
		for x := 0; x < devicePetAssetWidth; x++ {
			r, g, b := uint32(devicePetIdleBGRed)*257, uint32(devicePetIdleBGGreen)*257, uint32(devicePetIdleBGBlue)*257
			if x >= offX && x < offX+drawW && y >= offY && y < offY+drawH {
				sx, sy := bounds.Min.X+(x-offX)*bounds.Dx()/drawW, bounds.Min.Y+(y-offY)*bounds.Dy()/drawH
				sr, sg, sb, sa := src.At(sx, sy).RGBA()
				// color.Color.RGBA returns premultiplied 16-bit channels. The source
				// term must not be divided by alpha again; only scale the uncovered
				// background contribution.
				r = sr + r*(0xffff-sa)/0xffff
				g = sg + g*(0xffff-sa)/0xffff
				b = sb + b*(0xffff-sa)/0xffff
			}
			// Convert full 16-bit channels to RGB565. Masking the low byte (the
			// previous implementation) only worked accidentally for some 8-bit
			// colors and produced dark blocks after alpha compositing.
			value := uint16(((r >> 11) << 11) | ((g >> 10) << 5) | (b >> 11))
			i := (y*devicePetAssetWidth + x) * 2
			pixels[i], pixels[i+1] = byte(value), byte(value>>8)
		}
	}
	return pixels
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
