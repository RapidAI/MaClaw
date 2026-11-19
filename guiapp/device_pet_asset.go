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
	devicePetAssetWidth   = 256
	devicePetAssetHeight  = 256
	devicePetFrameCount   = 8
	devicePetFrameMS      = 450
	devicePetRouteFrameMS = 320
	devicePetTrackMargin  = 4
	devicePetRouteSlack   = 96
	// The dial paints the whole asset, so this floor has to leave the slack
	// above actually free. A higher floor pins a full-height pet in place.
	devicePetRouteMinScale = 0.60
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
	// Rearing is companionship time already stored for the desktop pet.
	// The stage's variant and scale are the points that change the look.
	rearingVariant, growth := a.petRearingLook(packID)
	variant := petAppearanceVariant(cfg.PetVariant, rearingVariant)
	frames, frameMS := devicePetFramesForPack(reg, packID, variant, growth, a.petRearingAdornmentSlots(packID))
	if len(frames) == 0 {
		raw, _, err := reg.LoadStateFrameBytes(packID, "idle", variant)
		if err != nil || len(raw) == 0 {
			return devicePetAsset{}
		}
		fallback, _, err := image.Decode(bytes.NewReader(raw))
		if err != nil || fallback == nil {
			return devicePetAsset{}
		}
		frames = devicePetApplyGrowth([]image.Image{fallback}, growth)
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

func devicePetRenderedFrames(reg *petpack.Registry, packID, variant string, growth float64, adornments []string) []image.Image {
	frames, _ := devicePetFramesForPack(reg, packID, variant, growth, adornments)
	return frames
}

// devicePetFramesForPack renders the loop a paired screen actually plays.
// Character and skeleton packs that can stroll (walk_loop, or run_loop when
// the pack has no walk) get the desktop idle-roam reel. Packs without a
// traveling clip keep the closed idle loop. growth is the rearing stage
// scale (0 or 1 leaves the authored size).
func devicePetFramesForPack(reg *petpack.Registry, packID, variant string, growth float64, adornments []string) ([]image.Image, int) {
	resolved, err := reg.Resolve(packID, variant)
	if err != nil || resolved == nil {
		return nil, 0
	}
	switch resolved.Renderer {
	case petpack.RendererCharacter, petpack.RendererSkeleton:
		// The desktop idle state strolls by playing walk_loop and stepping
		// DesktopRoam: the heading cycles through right, down, left, and up,
		// and the sprite turns when the horizontal direction changes. That
		// clock is not periodic, so eight frames cannot sample it live. The
		// reel bakes one closed stroll inside the safe area instead.
		renderer, renderErr := petpack.NewRigRenderer(resolved)
		if renderErr != nil || renderer == nil {
			return nil, 0
		}
		renderer.SetVisibleAdornmentSlots(adornments)
		if frames, frameMS := devicePetRichRigFrames(renderer, growth); len(frames) > 0 {
			return frames, frameMS
		}
		frames := make([]image.Image, 0, devicePetFrameCount)
		for index := 0; index < devicePetFrameCount; index++ {
			elapsed := index * devicePetFrameMS
			if rendered := renderer.Render(petpack.StateIdle, elapsed, devicePetAssetWidth); rendered != nil {
				frames = append(frames, rendered)
			}
		}
		return devicePetApplyGrowth(frames, growth), devicePetFrameMS
	case petpack.RendererProcedural:
		// The desktop fallback animates native/procedural packs with the same
		// articulated pose used by the floating pet. Export a full cycle so
		// hardware does not silently collapse these selected packs to one raster.
		frames := make([]image.Image, 0, devicePetFrameCount)
		for frame := 0; frame < devicePetFrameCount; frame++ {
			phase := 2 * math.Pi * float64(frame) / devicePetFrameCount
			frames = append(frames, renderDeviceProceduralPet(devicePetAssetWidth, packID, phase))
		}
		return devicePetApplyGrowth(frames, growth), devicePetFrameMS
	}
	return nil, 0
}

// devicePetShot is one pose in the eight-frame stroll the buddy loops.
// alongX and alongY are fractions of the remaining safe track, -1..1.
// Positive alongX moves the pet right; positive alongY moves it down.
// faceLeft mirrors the pose, the same turn keepFacing applies when a roam
// heading points left.
type devicePetShot struct {
	body     string
	expr     string
	at       int
	alongX   float64
	alongY   float64
	faceLeft bool
}

// devicePetWanderShots builds the desktop idle roam as one closed loop.
// idle_walk plays walk_loop; a pack with only a run still strolls on that
// clip. The lap is clockwise on the screen (right, down, left, up). Facing
// uses the same horizontal threshold as desktop keepFacing. Frame time stays
// monotonic so a looping walk is two gait cycles and layered gaze does not
// restart halfway around the lap.
func devicePetWanderShots(rr *petpack.RigRenderer) []devicePetShot {
	if rr == nil {
		return nil
	}
	body := ""
	switch {
	case rr.Clip("walk_loop") != nil:
		body = "walk_loop"
	case rr.Clip("run_loop") != nil:
		body = "run_loop"
	default:
		return nil
	}
	expr := devicePetExpr(rr, "expr_calm", "expr_soft", "expr_idle")
	faceLeft := false
	shots := make([]devicePetShot, 0, devicePetFrameCount)
	for i := 0; i < devicePetFrameCount; i++ {
		theta := -math.Pi/2 + float64(i)*2*math.Pi/float64(devicePetFrameCount)
		heading := math.Atan2(math.Cos(theta), -math.Sin(theta))
		faceLeft = devicePetFaceLeft(heading, faceLeft)
		shots = appendDevicePetShot(shots, rr, body, expr, i, 4, math.Cos(theta), math.Sin(theta), faceLeft)
	}
	if len(shots) < 2 {
		return nil
	}
	return shots
}

// devicePetFaceLeft matches petpack keepFacing: a nearly vertical heading
// keeps the side the pet is already showing.
func devicePetFaceLeft(heading float64, current bool) bool {
	c := math.Cos(heading)
	if c < -0.2 {
		return true
	}
	if c > 0.2 {
		return false
	}
	return current
}

// devicePetStrollRadius is the lap radius, in asset pixels, that makes one
// gait cycle cover about half the circle. That matches the clip's desktop
// travel, so the feet are not skating across the dial.
func devicePetStrollRadius(clip *petpack.RigClip) float64 {
	travel := 110.0
	if clip != nil {
		if d := math.Hypot(clip.TravelX, clip.TravelY); d >= 40 {
			travel = d
		}
	}
	return travel / math.Pi
}

// devicePetFacingBox is the opaque box after a left-facing mirror, so the
// route scale reserves room for the turned pose and not only the right-facing one.
func devicePetFacingBox(box petOpaqueBox, faceLeft bool) petOpaqueBox {
	if !box.ok || !faceLeft {
		return box
	}
	box.minX, box.maxX = devicePetAssetWidth-1-box.maxX, devicePetAssetWidth-1-box.minX
	return box
}

func appendDevicePetShot(shots []devicePetShot, rr *petpack.RigRenderer, body, expr string, numer, denom int, alongX, alongY float64, faceLeft bool) []devicePetShot {
	clip := rr.Clip(body)
	if clip == nil {
		return shots
	}
	at := 0
	if denom > 0 && clip.DurationMS > 0 {
		at = clip.DurationMS * numer / denom
	}
	return append(shots, devicePetShot{body: body, expr: expr, at: at, alongX: alongX, alongY: alongY, faceLeft: faceLeft})
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
	gaze := "gaze_center"
	if clip := rr.Clip("gaze_wander"); clip != nil && len(clip.Tracks) > 0 {
		gaze = "gaze_wander"
	}
	for _, name := range []string{gaze, "secondary_idle"} {
		clip := rr.Clip(name)
		if clip != nil && len(clip.Tracks) > 0 {
			layers = append(layers, name)
		}
	}
	return layers
}

func devicePetRichRigFrames(rr *petpack.RigRenderer, growth float64) ([]image.Image, int) {
	shots := devicePetWanderShots(rr)
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
		boxes[i] = devicePetFacingBox(petOpaqueBoxOf(frame), kept[i].faceLeft)
	}
	// Stage growth is applied after the stroll floor, then capped at 1 so a
	// larger pet uses the slack instead of walking off the dial.
	scale := devicePetRouteScale(boxes) * devicePetGrowthFactor(growth)
	if scale > 1 {
		scale = 1
	}
	radius := devicePetStrollRadius(rr.Clip(kept[0].body))
	frames := layoutDevicePetReel(raw, kept, scale, radius)
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

// devicePetRouteScale shrinks a reel just enough that the widest and tallest
// poses still have room to stroll inside the frame.
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
	roomH := float64(availH - devicePetRouteSlack)
	if roomH < 1 {
		roomH = 1
	}
	if float64(contentH) > roomH {
		scale = roomH / float64(contentH)
	}
	roomW := float64(availW - devicePetRouteSlack)
	if roomW < 1 {
		roomW = 1
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

// layoutDevicePetFrame places one pose. A single frame is its own union, so
// this stays the per-image primitive the layout test calls.
func layoutDevicePetFrame(src *image.NRGBA, scale, alongX, alongY float64, faceLeft bool) *image.NRGBA {
	frames := layoutDevicePetReel([]*image.NRGBA{src}, []devicePetShot{{
		alongX: alongX, alongY: alongY, faceLeft: faceLeft,
	}}, scale, math.Inf(1))
	if len(frames) == 0 || frames[0] == nil {
		return nil
	}
	out, _ := frames[0].(*image.NRGBA)
	return out
}

// layoutDevicePetReel shifts every pose by the same baseline. Recentering each
// silhouette on its own box would cancel the gait and make a wide step jump
// farther than a narrow one. radius caps the lap so one walk cycle stays near
// the clip's desktop travel; math.Inf uses the full safe slack.
func layoutDevicePetReel(frames []*image.NRGBA, shots []devicePetShot, scale, radius float64) []image.Image {
	if len(frames) == 0 || len(frames) != len(shots) {
		return nil
	}
	fitted := make([]*image.NRGBA, len(frames))
	boxes := make([]petOpaqueBox, len(frames))
	union := petOpaqueBox{}
	has := false
	for i, src := range frames {
		if src == nil {
			continue
		}
		img := src
		if shots[i].faceLeft {
			img = mirrorDevicePet(img)
		}
		if scale > 0 && math.Abs(scale-1) > 0.001 {
			img = scaleDevicePetAboutCenter(img, scale)
		}
		fitted[i] = img
		box := petOpaqueBoxOf(img)
		boxes[i] = box
		if !box.ok {
			continue
		}
		if !has {
			union = box
			has = true
			continue
		}
		if box.minX < union.minX {
			union.minX = box.minX
		}
		if box.minY < union.minY {
			union.minY = box.minY
		}
		if box.maxX > union.maxX {
			union.maxX = box.maxX
		}
		if box.maxY > union.maxY {
			union.maxY = box.maxY
		}
	}
	out := make([]image.Image, len(frames))
	if !has {
		for i := range fitted {
			out[i] = fitted[i]
		}
		return out
	}
	width, height := devicePetAssetWidth, devicePetAssetHeight
	for _, img := range fitted {
		if img != nil {
			width, height = img.Bounds().Dx(), img.Bounds().Dy()
			break
		}
	}
	limitLeft := devicePetTrackMargin
	limitRight := width - 1 - devicePetTrackMargin
	limitTop := devicePetTrackMargin
	limitBottom := height - 1 - devicePetTrackMargin
	unionW := union.maxX - union.minX + 1
	unionH := union.maxY - union.minY + 1
	safeW := limitRight - limitLeft + 1
	safeH := limitBottom - limitTop + 1
	slackX := safeW - unionW
	slackY := safeH - unionH
	if slackX < 0 {
		slackX = 0
	}
	if slackY < 0 {
		slackY = 0
	}
	// One baseline for the whole lap. A pose that stands on the bottom edge
	// would otherwise have no room to stroll downward.
	dx0 := limitLeft + (safeW-unionW)/2 - union.minX
	dy0 := limitTop + (safeH-unionH)/2 - union.minY
	ampX := float64(slackX) / 2
	ampY := float64(slackY) / 2
	if radius > 0 && !math.IsInf(radius, 0) {
		if ampX > radius {
			ampX = radius
		}
		if ampY > radius {
			ampY = radius
		}
	}
	for i := range fitted {
		if fitted[i] == nil || !boxes[i].ok {
			out[i] = fitted[i]
			continue
		}
		dx := dx0 + int(math.Round(clampDevicePetAlong(shots[i].alongX)*ampX))
		dy := dy0 + int(math.Round(clampDevicePetAlong(shots[i].alongY)*ampY))
		dx, dy = clampDevicePetShift(boxes[i], dx, dy, limitLeft, limitRight, limitTop, limitBottom)
		if dx == 0 && dy == 0 {
			out[i] = fitted[i]
			continue
		}
		out[i] = translateDevicePet(fitted[i], dx, dy)
	}
	return out
}

func clampDevicePetAlong(v float64) float64 {
	if math.IsNaN(v) {
		return 0
	}
	if v > 1 {
		return 1
	}
	if v < -1 {
		return -1
	}
	return v
}

func clampDevicePetShift(box petOpaqueBox, dx, dy, limitLeft, limitRight, limitTop, limitBottom int) (int, int) {
	if box.minX+dx < limitLeft {
		dx = limitLeft - box.minX
	}
	if box.maxX+dx > limitRight {
		dx -= box.maxX + dx - limitRight
	}
	if box.minX+dx < limitLeft {
		dx = limitLeft - box.minX
	}
	if box.minY+dy < limitTop {
		dy = limitTop - box.minY
	}
	if box.maxY+dy > limitBottom {
		dy -= box.maxY + dy - limitBottom
	}
	if box.minY+dy < limitTop {
		dy = limitTop - box.minY
	}
	return dx, dy
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

// devicePetGrowthFactor clamps a rearing scale. Zero means the stage did not
// ask for a size change. Pack validation allows 0.5–1.5.
func devicePetGrowthFactor(growth float64) float64 {
	if growth == 0 || math.IsNaN(growth) || math.IsInf(growth, 0) {
		return 1
	}
	if growth < 0.5 {
		return 0.5
	}
	if growth > 1.5 {
		return 1.5
	}
	return growth
}

// devicePetApplyGrowth scales idle and procedural frames about the opaque
// body and keeps that body inside the margin. Stroll frames do not use this:
// their growth is baked into the route scale before the shared lap shift.
func devicePetApplyGrowth(frames []image.Image, growth float64) []image.Image {
	factor := devicePetGrowthFactor(growth)
	if len(frames) == 0 || math.Abs(factor-1) < 0.001 {
		return frames
	}
	imgs := make([]*image.NRGBA, len(frames))
	union := petOpaqueBox{}
	has := false
	width, height := 0, 0
	for i, frame := range frames {
		img := devicePetNRGBA(frame)
		imgs[i] = img
		if img == nil {
			continue
		}
		if width == 0 {
			width, height = img.Bounds().Dx(), img.Bounds().Dy()
		}
		box := petOpaqueBoxOf(img)
		if !box.ok || img.Bounds().Dx() != width || img.Bounds().Dy() != height {
			continue
		}
		if !has {
			union = box
			has = true
			continue
		}
		if box.minX < union.minX {
			union.minX = box.minX
		}
		if box.minY < union.minY {
			union.minY = box.minY
		}
		if box.maxX > union.maxX {
			union.maxX = box.maxX
		}
		if box.maxY > union.maxY {
			union.maxY = box.maxY
		}
	}
	if !has || width < 1 || height < 1 {
		return frames
	}
	cx := float64(union.minX+union.maxX) / 2
	cy := float64(union.minY+union.maxY) / 2
	limitLeft := float64(devicePetTrackMargin)
	limitRight := float64(width - 1 - devicePetTrackMargin)
	limitTop := float64(devicePetTrackMargin)
	limitBottom := float64(height - 1 - devicePetTrackMargin)
	fit := factor
	clampFit := func(span, room float64) {
		if span <= 0.5 {
			return
		}
		if room <= 0 {
			if fit > 1 {
				fit = 1
			}
			return
		}
		if room/span < fit {
			fit = room / span
		}
	}
	clampFit(cx-float64(union.minX), cx-limitLeft)
	clampFit(float64(union.maxX)-cx, limitRight-cx)
	clampFit(cy-float64(union.minY), cy-limitTop)
	clampFit(float64(union.maxY)-cy, limitBottom-cy)
	if fit < 0.05 {
		fit = 0.05
	}
	if math.Abs(fit-1) < 0.001 {
		return frames
	}
	out := make([]image.Image, len(frames))
	limitL := devicePetTrackMargin
	limitR := width - 1 - devicePetTrackMargin
	limitT := devicePetTrackMargin
	limitB := height - 1 - devicePetTrackMargin
	for i, img := range imgs {
		if img == nil || img.Bounds().Dx() != width || img.Bounds().Dy() != height {
			out[i] = frames[i]
			continue
		}
		scaled := scaleDevicePetAbout(img, fit, cx, cy)
		box := petOpaqueBoxOf(scaled)
		if box.ok {
			dx, dy := clampDevicePetShift(box, 0, 0, limitL, limitR, limitT, limitB)
			if dx != 0 || dy != 0 {
				scaled = translateDevicePet(scaled, dx, dy)
			}
		}
		out[i] = scaled
	}
	return out
}

func devicePetNRGBA(src image.Image) *image.NRGBA {
	if src == nil {
		return nil
	}
	if img, ok := src.(*image.NRGBA); ok && img.Bounds().Min.X == 0 && img.Bounds().Min.Y == 0 {
		return img
	}
	bounds := src.Bounds()
	dst := image.NewNRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(dst, dst.Bounds(), src, bounds.Min, draw.Src)
	return dst
}

// scaleDevicePetAbout keeps the anchor pixel in place. Growing about the
// canvas center would push a bottom-standing pet's feet off the frame.
func scaleDevicePetAbout(src *image.NRGBA, scale, ax, ay float64) *image.NRGBA {
	if src == nil || scale <= 0 || math.IsNaN(scale) {
		return src
	}
	bounds := src.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	dst := image.NewNRGBA(image.Rect(0, 0, width, height))
	dw := int(math.Round(float64(width) * scale))
	dh := int(math.Round(float64(height) * scale))
	if dw < 1 {
		dw = 1
	}
	if dh < 1 {
		dh = 1
	}
	x0 := int(math.Round(ax * (1 - scale)))
	y0 := int(math.Round(ay * (1 - scale)))
	xdraw.CatmullRom.Scale(dst, image.Rect(x0, y0, x0+dw, y0+dh), src, bounds, draw.Over, nil)
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
