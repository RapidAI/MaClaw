package guiapp

import (
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/RapidAI/CodeClaw/buddy/protocol"
	"github.com/RapidAI/CodeClaw/buddy/service"
	"github.com/RapidAI/CodeClaw/corelib"
)

// buddyPetAsset is the device-sized form of the GUI's current pet.
type buddyPetAsset struct {
	Frames  [][]byte
	FrameMS int
}

// buildBuddyPetAsset renders the GUI pet via the same pipeline the ESP32
// gateway uses (8 frames, RGB565+A8), then downscales each 256px frame to the
// buddy's 128px target. Alpha is preserved so the pet composites over the
// device's own weather ring.
func (a *App) buildBuddyPetAsset() (*buddyPetAsset, error) {
	cfg, err := a.LoadConfig()
	if err != nil {
		return nil, err
	}
	return a.buildBuddyPetAssetForConfig(cfg)
}

// buddyPetAssetChanged reports whether a config update changes the frames the
// device renders. Motion, size, and sound settings do not alter the asset.
func buddyPetAssetChanged(oldConfig, newConfig corelib.AppConfig) bool {
	return oldConfig.PetSkin != newConfig.PetSkin ||
		oldConfig.PetVariant != newConfig.PetVariant
}

// buildBuddyPetAssetForConfig renders from an already-loaded config. The
// config save path calls this instead of reloading, so it cannot race the
// write it is reacting to.
func (a *App) buildBuddyPetAssetForConfig(cfg corelib.AppConfig) (*buddyPetAsset, error) {
	asset := a.devicePetAssetForConfig(cfg)
	if asset.Width <= 0 || asset.Height <= 0 || asset.Data == "" {
		return nil, errors.New("no pet asset for the current configuration")
	}
	encoded := append([]string{asset.Data}, asset.Frames...)
	frames := make([][]byte, 0, len(encoded))
	for _, item := range encoded {
		raw, err := base64.StdEncoding.DecodeString(item)
		if err != nil {
			continue
		}
		if len(raw) != asset.Width*asset.Height*3 {
			continue
		}
		frames = append(frames, downscalePetRGB565A8(raw, asset.Width, asset.Height,
			protocol.AssetWidth, protocol.AssetHeight))
	}
	if len(frames) == 0 {
		return nil, errors.New("no usable pet frames")
	}
	if len(frames) > protocol.AssetMaxFrames {
		frames = frames[:protocol.AssetMaxFrames]
	}
	frameMS := asset.FrameMS
	if frameMS <= 0 {
		frameMS = 450
	}
	return &buddyPetAsset{Frames: frames, FrameMS: frameMS}, nil
}

// downscalePetRGB565A8 performs an alpha-weighted box downscale. The source
// stores straight RGB + alpha; weighting by alpha keeps transparent edge
// pixels from darkening the silhouette.
func downscalePetRGB565A8(src []byte, sw, sh, dw, dh int) []byte {
	dst := make([]byte, dw*dh*3)
	for y := 0; y < dh; y++ {
		sy0 := y * sh / dh
		sy1 := (y + 1) * sh / dh
		if sy1 <= sy0 {
			sy1 = sy0 + 1
		}
		for x := 0; x < dw; x++ {
			sx0 := x * sw / dw
			sx1 := (x + 1) * sw / dw
			if sx1 <= sx0 {
				sx1 = sx0 + 1
			}
			rSum, gSum, bSum, aSum, wSum, n := 0, 0, 0, 0, 0, 0
			for sy := sy0; sy < sy1; sy++ {
				for sx := sx0; sx < sx1; sx++ {
					i := (sy*sw + sx) * 3
					value := uint16(src[i]) | uint16(src[i+1])<<8
					alpha := int(src[i+2])
					weight := alpha + 1
					rSum += int((value>>11)&0x1F) * weight
					gSum += int((value>>5)&0x3F) * weight
					bSum += int(value&0x1F) * weight
					aSum += alpha
					wSum += weight
					n++
				}
			}
			if n == 0 || wSum == 0 {
				continue
			}
			r := (rSum + wSum/2) / wSum
			g := (gSum + wSum/2) / wSum
			b := (bSum + wSum/2) / wSum
			alpha := (aSum + n/2) / n
			value := uint16(r<<11 | g<<5 | b)
			i := (y*dw + x) * 3
			dst[i], dst[i+1], dst[i+2] = byte(value), byte(value>>8), byte(alpha)
		}
	}
	return dst
}

// pushBuddyPetAsset renders and queues the current GUI pet. It runs in the
// background because rendering eight frames takes a few hundred milliseconds.
func (a *App) pushBuddyPetAsset(runner *service.Runner) {
	if runner == nil {
		return
	}
	asset, err := a.buildBuddyPetAsset()
	if err != nil {
		logBuddyf("[buddy] pet asset unavailable: %v", err)
		return
	}
	runner.SetPetAsset(asset.Frames, asset.FrameMS)
	alpha := 0
	if len(asset.Frames) > 0 {
		frame := asset.Frames[0]
		for i := 2; i < len(frame); i += 3 {
			if frame[i] != 0 {
				alpha++
			}
		}
	}
	logBuddyf("[buddy] pet asset queued: %d frames (%dms), alpha=%d/%d",
		len(asset.Frames), asset.FrameMS, alpha, protocol.AssetWidth*protocol.AssetHeight)
}

// refreshBuddyPetForConfig re-renders and queues the pet after the system pet
// settings change. Safe to call from a goroutine after a config commit.
func (a *App) refreshBuddyPetForConfig(cfg corelib.AppConfig) {
	a.buddyMu.Lock()
	runner := a.buddyRunner
	a.buddyMu.Unlock()
	if runner == nil {
		return
	}
	asset, err := a.buildBuddyPetAssetForConfig(cfg)
	if err != nil {
		logBuddyf("[buddy] pet asset unavailable after config change: %v", err)
		return
	}
	runner.SetPetAsset(asset.Frames, asset.FrameMS)
	logBuddyf("[buddy] pet asset queued after config change: %d frames", len(asset.Frames))
}

// BuddyRefreshPet re-renders the current GUI pet and sends it to the device.
// The settings panel calls this after the person changes the pet skin.
func (a *App) BuddyRefreshPet() error {
	a.buddyMu.Lock()
	runner := a.buddyRunner
	a.buddyMu.Unlock()
	if runner == nil {
		return fmt.Errorf("buddy service is not running")
	}
	asset, err := a.buildBuddyPetAsset()
	if err != nil {
		return err
	}
	runner.SetPetAsset(asset.Frames, asset.FrameMS)
	return nil
}
