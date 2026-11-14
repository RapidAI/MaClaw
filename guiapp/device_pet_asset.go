package guiapp

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/draw"
	_ "image/png"
	"math"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/guiapp/petpack"
	xdraw "golang.org/x/image/draw"
)

// Eight 256px RGB565+A8 frames are sent by media reference. Bundled pet packs are
// authored at 256px, so preserving that resolution avoids first shrinking to
// 128px and then magnifying to 220px on the LCD.
const (
	devicePetAssetWidth    = 256
	devicePetAssetHeight   = 256
	devicePetFrameCount    = 8
	devicePetFrameMS       = 450
	devicePetRouteFrameMS  = 320
	devicePetTrackMargin   = 4
	devicePetRouteSlack    = 96
	devicePetRouteMinScale = 0.82
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
	frames, frameMS := devicePetFramesForPack(reg, packID, variant)
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
	if frameMS <= 0 {
		frameMS = devicePetFrameMS
	}
	asset := devicePetAsset{Encoding: "rgb565a8", Width: devicePetAssetWidth, Height: devicePetAssetHeight, Data: encoded[0], FrameMS: frameMS}
	if len(encoded) > 1 {
		asset.Frames = encoded[1:]
	}
	return asset
}

func devicePetRenderedFrames(reg *petpack.Registry, packID, variant string) []image.Image {
	frames, _ := devicePetFramesForPack(reg, packID, variant)
	return frames
}

// devicePetFramesForPack renders the loop a paired screen actually plays.
// Character and skeleton packs that declare the 2.0 action clips (walk, run,
// jump, bend, sing, stretch) get that reel. Packs without those clips keep
// the closed idle loop.
func devicePetFramesForPack(reg *petpack.Registry, packID, variant string) ([]image.Image, int) {
	resolved, err := reg.Resolve(packID, variant)
	if err != nil || resolved == nil {
		return nil, 0
	}
	switch resolved.Renderer {
	case petpack.RendererCharacter, petpack.RendererSkeleton:
		// The desktop performance director is intentionally non-periodic. Sampling
		// it into eight frames jumps when the device wraps to frame zero. Rich
		// actions are therefore a fixed reel of authored clips. Walk and run
		// advance a route inside the frame and turn at the safe-area edge, the
		// same clamp the floating window applies to travel_x, so the pet stays
		// on the small round display.
		renderer, renderErr := petpack.NewRigRenderer(resolved)
		if renderErr != nil || renderer == nil {
			return nil, 0
		}
		if frames, frameMS := devicePetRichRigFrames(renderer); len(frames) > 0 {
			return frames, frameMS
		}
		frames := make([]image.Image, 0, devicePetFrameCount)
		for index := 0; index < devicePetFrameCount; index++ {
			elapsed := index * devicePetFrameMS
			if rendered := renderer.Render(petpack.StateIdle, elapsed, devicePetAssetWidth); rendered != nil {
				frames = append(frames, rendered)
			}
		}
		return frames, devicePetFrameMS
	case petpack.RendererProcedural:
		// The desktop fallback animates native/procedural packs with the same
		// articulated pose used by the floating pet. Export a full cycle so
		// hardware does not silently collapse these selected packs to one raster.
		frames := make([]image.Image, 0, devicePetFrameCount)
		for frame := 0; frame < devicePetFrameCount; frame++ {
			phase := 2 * math.Pi * float64(frame) / devicePetFrameCount
			frames = append(frames, renderDeviceProceduralPet(devicePetAssetWidth, packID, phase))
		}
		return frames, devicePetFrameMS
	}
	return nil, 0
}

// devicePetShot is one authored pose in the eight-frame device reel.
// along is the fraction of the remaining safe track, -1..1. Positive moves
// the pet right. faceLeft mirrors the pose, matching a window turn at the edge.
type devicePetShot struct {
	body     string
	expr     string
	at       int
	along    float64
	faceLeft bool
}

func devicePetRichShots(rr *petpack.RigRenderer) []devicePetShot {
	if rr == nil {
		return nil
	}
	rich := false
	for _, name := range []string{"walk_loop", "run_loop", "jump_loop", "bend_loop", "sing_loop", "stretch_loop"} {
		if rr.Clip(name) != nil {
			rich = true
			break
		}
	}
	if !rich {
		return nil
	}
	calm := devicePetExpr(rr, "expr_calm", "expr_soft", "expr_idle")
	pleased := devicePetExpr(rr, "expr_pleased", "expr_calm", "expr_soft", "expr_idle")
	speak := devicePetExpr(rr, "expr_speak", "expr_talk", "expr_pleased", calm)
	idleBody := ""
	for _, name := range []string{"idle_breathe_loop", "idle_loop", "idle"} {
		if rr.Clip(name) != nil {
			idleBody = name
			break
		}
	}
	var shots []devicePetShot
	if idleBody != "" {
		shots = appendDevicePetShot(shots, rr, idleBody, calm, 0, 1, 0, false)
	}
	if rr.Clip("walk_loop") != nil {
		shots = appendDevicePetShot(shots, rr, "walk_loop", calm, 0, 1, 0.55, false)
		shots = appendDevicePetShot(shots, rr, "walk_loop", calm, 1, 2, 1, true)
	}
	if rr.Clip("run_loop") != nil {
		shots = appendDevicePetShot(shots, rr, "run_loop", pleased, 0, 1, 0.45, true)
		shots = appendDevicePetShot(shots, rr, "run_loop", pleased, 1, 2, 0, true)
	} else if rr.Clip("walk_loop") != nil {
		shots = appendDevicePetShot(shots, rr, "walk_loop", calm, 1, 4, 0.45, true)
		shots = appendDevicePetShot(shots, rr, "walk_loop", calm, 3, 4, 0, true)
	}
	if rr.Clip("jump_loop") != nil {
		shots = appendDevicePetShot(shots, rr, "jump_loop", pleased, 2, 5, 0, true)
	}
	if rr.Clip("bend_loop") != nil {
		shots = appendDevicePetShot(shots, rr, "bend_loop", calm, 2, 5, 0, false)
	}
	if rr.Clip("sing_loop") != nil {
		shots = appendDevicePetShot(shots, rr, "sing_loop", speak, 3, 8, 0, false)
	} else if rr.Clip("stretch_loop") != nil {
		shots = appendDevicePetShot(shots, rr, "stretch_loop", calm, 1, 2, 0, false)
	}
	if len(shots) == 0 {
		return nil
	}
	if len(shots) > devicePetFrameCount {
		shots = shots[:devicePetFrameCount]
	}
	for len(shots) < devicePetFrameCount {
		body := idleBody
		if body == "" {
			body = shots[0].body
		}
		before := len(shots)
		shots = appendDevicePetShot(shots, rr, body, calm, len(shots), devicePetFrameCount, 0, false)
		if len(shots) == before {
			break
		}
	}
	return shots
}

func appendDevicePetShot(shots []devicePetShot, rr *petpack.RigRenderer, body, expr string, numer, denom int, along float64, faceLeft bool) []devicePetShot {
	clip := rr.Clip(body)
	if clip == nil {
		return shots
	}
	at := 0
	if denom > 0 && clip.DurationMS > 0 {
		at = clip.DurationMS * numer / denom
	}
	return append(shots, devicePetShot{body: body, expr: expr, at: at, along: along, faceLeft: faceLeft})
}

func devicePetExpr(rr *petpack.RigRenderer, names ...string) string {
	if rr == nil {
		return ""
	}
	for _, name := range names {
		if name != "" && rr.Clip(name) != nil {
			return name
		}
	}
	return ""
}

func devicePetLayers(rr *petpack.RigRenderer, body, expr string) []string {
	layers := []string{body}
	if expr != "" && rr.Clip(expr) != nil {
		layers = append(layers, expr)
	}
	for _, name := range []string{"gaze_center", "secondary_idle"} {
		clip := rr.Clip(name)
		if clip != nil && len(clip.Tracks) > 0 {
			layers = append(layers, name)
		}
	}
	return layers
}

func devicePetRichRigFrames(rr *petpack.RigRenderer) ([]image.Image, int) {
	shots := devicePetRichShots(rr)
	if len(shots) == 0 || rr == nil {
		return nil, 0
	}
	raw := make([]*image.NRGBA, 0, len(shots))
	kept := make([]devicePetShot, 0, len(shots))
	for _, shot := range shots {
		frame := rr.RenderClips(devicePetLayers(rr, shot.body, shot.expr), shot.at, devicePetAssetWidth)
		if frame == nil {
			continue
		}
		raw = append(raw, frame)
		kept = append(kept, shot)
	}
	if len(raw) < 2 {
		return nil, 0
	}
	boxes := make([]petOpaqueBox, len(raw))
	for i, frame := range raw {
		boxes[i] = petOpaqueBoxOf(frame)
	}
	scale := devicePetRouteScale(boxes)
	frames := make([]image.Image, 0, len(raw))
	for i, frame := range raw {
		frames = append(frames, layoutDevicePetFrame(frame, scale, kept[i].along, kept[i].faceLeft))
	}
	frameMS := devicePetFrameMS
	if rr.Clip("walk_loop") != nil || rr.Clip("run_loop") != nil {
		frameMS = devicePetRouteFrameMS
	}
	return frames, frameMS
}

type petOpaqueBox struct {
	minX, minY, maxX, maxY int
	ok                     bool
}

func petOpaqueBoxOf(src *image.NRGBA) petOpaqueBox {
	if src == nil {
		return petOpaqueBox{}
	}
	bounds := src.Bounds()
	box := petOpaqueBox{minX: bounds.Dx(), minY: bounds.Dy(), maxX: -1, maxY: -1}
	for y := 0; y < bounds.Dy(); y++ {
		row := src.Pix[src.PixOffset(bounds.Min.X, bounds.Min.Y+y):]
		for x := 0; x < bounds.Dx(); x++ {
			if row[x*4+3] <= 16 {
				continue
			}
			if x < box.minX {
				box.minX = x
			}
			if y < box.minY {
				box.minY = y
			}
			if x > box.maxX {
				box.maxX = x
			}
			if y > box.maxY {
				box.maxY = y
			}
			box.ok = true
		}
	}
	return box
}

// devicePetRouteScale shrinks a reel just enough that the widest pose still
// has room to travel, and the tallest pose (a jump) still fits the canvas.
func devicePetRouteScale(boxes []petOpaqueBox) float64 {
	minX, minY := devicePetAssetWidth, devicePetAssetHeight
	maxX, maxY := -1, -1
	for _, box := range boxes {
		if !box.ok {
			continue
		}
		if box.minX < minX {
			minX = box.minX
		}
		if box.minY < minY {
			minY = box.minY
		}
		if box.maxX > maxX {
			maxX = box.maxX
		}
		if box.maxY > maxY {
			maxY = box.maxY
		}
	}
	if maxX < minX || maxY < minY {
		return 1
	}
	contentW := maxX - minX + 1
	contentH := maxY - minY + 1
	availW := devicePetAssetWidth - 2*devicePetTrackMargin
	availH := devicePetAssetHeight - 2*devicePetTrackMargin
	if contentW < 1 || contentH < 1 || availW < 1 || availH < 1 {
		return 1
	}
	scale := 1.0
	if contentH > availH {
		scale = float64(availH) / float64(contentH)
	}
	roomW := float64(availW - devicePetRouteSlack)
	if roomW < float64(availW)*devicePetRouteMinScale {
		roomW = float64(availW) * devicePetRouteMinScale
	}
	if float64(contentW)*scale > roomW {
		scale = roomW / float64(contentW)
	}
	if scale > 1 {
		scale = 1
	}
	if scale < devicePetRouteMinScale {
		scale = devicePetRouteMinScale
	}
	return scale
}

// layoutDevicePetFrame keeps one pose inside the frame while shifting it along
// the walk/run route. along is clamped to the opaque silhouette's own slack,
// which is the device analogue of the floating window stopping at the work area.
func layoutDevicePetFrame(src *image.NRGBA, scale, along float64, faceLeft bool) *image.NRGBA {
	if src == nil {
		return nil
	}
	if faceLeft {
		src = mirrorDevicePet(src)
	}
	fitted := src
	if scale < 0.999 {
		fitted = scaleDevicePetAboutCenter(src, scale)
	}
	box := petOpaqueBoxOf(fitted)
	if !box.ok {
		return fitted
	}
	width, height := fitted.Bounds().Dx(), fitted.Bounds().Dy()
	slackRight := (width - 1 - devicePetTrackMargin) - box.maxX
	slackLeft := box.minX - devicePetTrackMargin
	if slackRight < 0 {
		slackRight = 0
	}
	if slackLeft < 0 {
		slackLeft = 0
	}
	dx := 0
	if along > 0 {
		dx = int(math.Round(along * float64(slackRight)))
	} else if along < 0 {
		dx = int(math.Round(along * float64(slackLeft)))
	}
	dy := 0
	limitTop := devicePetTrackMargin
	limitBottom := height - 1 - devicePetTrackMargin
	if box.minY < limitTop {
		dy = limitTop - box.minY
	}
	if box.maxY+dy > limitBottom {
		dy = limitBottom - box.maxY
	}
	if box.minY+dy < limitTop {
		dy = limitTop - box.minY
	}
	if dx == 0 && dy == 0 {
		return fitted
	}
	return translateDevicePet(fitted, dx, dy)
}

func mirrorDevicePet(src *image.NRGBA) *image.NRGBA {
	if src == nil {
		return nil
	}
	bounds := src.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	dst := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			dst.SetNRGBA(width-1-x, y, src.NRGBAAt(bounds.Min.X+x, bounds.Min.Y+y))
		}
	}
	return dst
}

func scaleDevicePetAboutCenter(src *image.NRGBA, scale float64) *image.NRGBA {
	bounds := src.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	dst := image.NewNRGBA(image.Rect(0, 0, width, height))
	dw := int(float64(width)*scale + 0.5)
	dh := int(float64(height)*scale + 0.5)
	if dw < 1 {
		dw = 1
	}
	if dh < 1 {
		dh = 1
	}
	x0 := (width - dw) / 2
	y0 := (height - dh) / 2
	xdraw.CatmullRom.Scale(dst, image.Rect(x0, y0, x0+dw, y0+dh), src, bounds, draw.Over, nil)
	return dst
}

func translateDevicePet(src *image.NRGBA, dx, dy int) *image.NRGBA {
	bounds := src.Bounds()
	dst := image.NewNRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(dst, image.Rect(dx, dy, dx+bounds.Dx(), dy+bounds.Dy()), src, bounds.Min, draw.Over)
	return dst
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
	// Some static/native packs return the same raster for every requested phase.
	// Replacing only frame 1 with a synthetic pose creates a single bump followed
	// by six frozen segments. Generate the same closed breathing loop used by a
	// true one-frame asset only when the complete sequence is actually static.
	if allDevicePetFramesEqual(frames) {
		return ensureDevicePetMotionFrames([]image.Image{frames[0]})
	}
	if len(frames) > devicePetFrameCount {
		frames = frames[:devicePetFrameCount]
	}
	return frames
}

func allDevicePetFramesEqual(frames []image.Image) bool {
	if len(frames) < 2 || frames[0] == nil {
		return false
	}
	first := devicePetRGB565A8(frames[0])
	if len(first) == 0 {
		return false
	}
	for _, frame := range frames[1:] {
		if frame == nil || !bytes.Equal(first, devicePetRGB565A8(frame)) {
			return false
		}
	}
	return true
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
	// Keep the feet/bottom of the authored canvas fixed. Scaling toward the top
	// made the whole silhouette lift and settle on every synthetic breath,
	// which looked like vertical jitter rather than breathing on the device.
	destination := image.Rect(inset, lift, width-inset, height)
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

// devicePetProfileForSkin keeps the desktop motion preference but renders the
// requested installed pack. Hardware custom-pet selection intentionally does
// not alter the desktop's own pet configuration.
func (a *App) devicePetProfileForSkin(cfg corelib.AppConfig, skin string) map[string]any {
	reg := petpack.EnsureGlobal()
	if reg == nil {
		return a.devicePetProfileForConfig(cfg)
	}
	cfg.PetSkin = petpack.SanitizeSkinID(skin, true, reg.Allowlist())
	cfg.PetVariant = petpack.VariantDefault
	return a.devicePetProfileForConfig(cfg)
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
