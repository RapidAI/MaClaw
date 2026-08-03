package petpack

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	_ "image/png"
	"io"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	_ "golang.org/x/image/webp"
)

// Rig is the restricted v2 animation format. It is deliberately small: a
// bone hierarchy, named local raster slots, and numeric keyframes only.
// Expressions, callbacks, URLs, shaders, and script execution are absent.
type Rig struct {
	Version int                `json:"version"`
	// Join controls multi-part neck/shoulder compositing aids. Optional and
	// ignored by packs that do not use body+expression-head paper-dolls.
	Join    *RigJoin           `json:"join,omitempty"`
	Bones   []RigBone          `json:"bones"`
	Slots   []RigSlot          `json:"slots"`
	Clips   map[string]RigClip `json:"clips"`
}

// RigJoin is the engine-side neck join contract for multi-part characters.
// It is pure data (no scripts). Defaults apply only when a multi-head pack is
// detected and Join is omitted — clawmate-style multi-limb packs are untouched.
//
// collar_cover: after body+heads are drawn, re-draw the upper body strip so
// clothing covers any residual neck plug (the most common multi-part failure).
// head_neck_fade_px: soft-clear the center-bottom of expression-head textures
// so a long flesh stump is less likely to stick out of the collar.
// chin_inset_px: reserved for pack tools; the live renderer currently relies
// on authored head bone Y + collar_cover (kept for schema forward-compat).
// state_head_offset: optional per runtime-state head bone Y delta (px on the
// 256 design canvas). Applied when the matching expression head slot is the
// only head with alpha > 0.5.
type RigJoin struct {
	// Auto is the pack opt-in/out for engine join aids.
	// omit / true: enable for multi-head packs (default). false: force off.
	Auto *bool `json:"auto,omitempty"`
	// CollarCover redraws the body top after heads. Default true for multi-head.
	CollarCover *bool `json:"collar_cover,omitempty"`
	// CollarCoverFrac is the fraction of body texture height re-drawn from the
	// top (0.08–0.35). Default 0.18.
	CollarCoverFrac float64 `json:"collar_cover_frac,omitempty"`
	// CollarOverlay is an optional declared texture drawn last at the body bone
	// (e.g. rig/collar_overlay.png). When set it is used instead of auto body-top.
	CollarOverlay string `json:"collar_overlay,omitempty"`
	// HeadNeckFadePx soft-fades the center-bottom of head_* textures (0–48).
	// Default 10 for multi-head packs.
	HeadNeckFadePx int `json:"head_neck_fade_px,omitempty"`
	// HeadNeckFadeCenter is the horizontal fraction of the head texture treated
	// as "center neck" (0.2–0.8). Default 0.42.
	HeadNeckFadeCenter float64 `json:"head_neck_fade_center,omitempty"`
	// ChinInsetPx documents the authored chin tuck in design pixels (0–32).
	ChinInsetPx int `json:"chin_inset_px,omitempty"`
	// StateHeadOffset maps runtime state id → head bone Y delta in design px.
	// Keys: idle, listening, thinking, speaking, done, alert, quiet.
	StateHeadOffset map[string]RigHeadOffset `json:"state_head_offset,omitempty"`
}

// RigHeadOffset is a small per-state pose tweak for the head bone.
type RigHeadOffset struct {
	X float64 `json:"x,omitempty"`
	Y float64 `json:"y,omitempty"`
}

type RigBone struct {
	Name   string  `json:"name"`
	Parent string  `json:"parent,omitempty"`
	X      float64 `json:"x,omitempty"`
	Y      float64 `json:"y,omitempty"`
	Rotate float64 `json:"rotate,omitempty"`
	ScaleX float64 `json:"scale_x,omitempty"`
	ScaleY float64 `json:"scale_y,omitempty"`
}

type RigSlot struct {
	Name    string  `json:"name"`
	Bone    string  `json:"bone"`
	Texture string  `json:"texture"`
	X       float64 `json:"x,omitempty"`
	Y       float64 `json:"y,omitempty"`
	Rotate  float64 `json:"rotate,omitempty"`
	ScaleX  float64 `json:"scale_x,omitempty"`
	ScaleY  float64 `json:"scale_y,omitempty"`
	Z       int     `json:"z,omitempty"`
}

type RigClip struct {
	DurationMS int                      `json:"duration_ms"`
	Loop       bool                     `json:"loop,omitempty"`
	Tracks     map[string][]RigKeyframe `json:"tracks,omitempty"`
}

type RigKeyframe struct {
	AtMS   int      `json:"at_ms"`
	X      float64  `json:"x,omitempty"`
	Y      float64  `json:"y,omitempty"`
	Rotate float64  `json:"rotate,omitempty"`
	ScaleX float64  `json:"scale_x,omitempty"`
	ScaleY float64  `json:"scale_y,omitempty"`
	Alpha  *float64 `json:"alpha,omitempty"`
	Ease   string   `json:"ease,omitempty"`
}

const (
	maxRigBones          = 24
	maxRigSlots          = 32
	maxRigTextures       = 32
	maxRigKeyframes      = 240
	maxRigTotalKeyframes = 1200
	// A pet is rendered at 56–136px. These bounds keep an apparently small,
	// compressed image from expanding into an unreasonable amount of memory.
	maxRigTextureBytes   = 512 * 1024
	maxRigTextureEdge    = 1024
	maxRigTexturePixels  = 4 * 1024 * 1024
	maxStaticFrameBytes  = 512 * 1024
	maxStaticFrameEdge   = 1024
	maxStaticFramePixels = 4 * 1024 * 1024
)

// LoadRig validates and reads a resolved pack's rig. The caller keeps the
// native frame path as a fallback when this returns an error.
func LoadRig(resolved *ResolvedPack) (*Rig, error) {
	if resolved == nil || resolved.Rig == nil {
		return nil, nil
	}
	definition := safeRel(resolved.Rig.Definition)
	if definition == "" || !strings.HasSuffix(strings.ToLower(definition), ".json") {
		return nil, fmt.Errorf("invalid rig definition")
	}
	if resolved.AssetFS == nil {
		return nil, fmt.Errorf("rig asset filesystem unavailable")
	}
	raw, err := fs.ReadFile(resolved.AssetFS, definition)
	if err != nil {
		return nil, fmt.Errorf("read rig definition: %w", err)
	}
	var rig Rig
	if err := json.Unmarshal(raw, &rig); err != nil {
		return nil, fmt.Errorf("parse rig definition: %w", err)
	}
	if err := ValidateRig(&rig, resolved.Rig); err != nil {
		return nil, err
	}
	return &rig, nil
}

// ValidateStaticFrameData validates the raster fallback shared by desktop
// scans and Pet Store uploads. DecodeConfig avoids allocating full image
// buffers merely to reject oversized or malformed frames.
func ValidateStaticFrameData(raw []byte) error {
	if len(raw) == 0 || len(raw) > maxStaticFrameBytes {
		return fmt.Errorf("static frame exceeds %d bytes", maxStaticFrameBytes)
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("decode static frame: %w", err)
	}
	if format != "png" && format != "webp" {
		return fmt.Errorf("static frame is not PNG/WebP")
	}
	if cfg.Width < 1 || cfg.Height < 1 || cfg.Width > maxStaticFrameEdge || cfg.Height > maxStaticFrameEdge || cfg.Width*cfg.Height > maxStaticFramePixels {
		return fmt.Errorf("static frame dimensions exceed %dx%d", maxStaticFrameEdge, maxStaticFrameEdge)
	}
	return nil
}

// ValidateStaticFrame reads and validates one declared native fallback frame
// without allowing a direct filesystem scan to allocate an unbounded file.
func ValidateStaticFrame(fsys fs.FS, path string) error {
	path = safeRel(path)
	if fsys == nil || path == "" {
		return fmt.Errorf("static frame is unavailable")
	}
	file, err := fsys.Open(path)
	if err != nil {
		return err
	}
	raw, readErr := io.ReadAll(io.LimitReader(file, maxStaticFrameBytes+1))
	closeErr := file.Close()
	if readErr != nil {
		return readErr
	}
	if closeErr != nil {
		return closeErr
	}
	return ValidateStaticFrameData(raw)
}

// ValidateRig is public so the desktop importer, Pet Store, and build guide
// examples can use the exact same semantic contract.
func ValidateRig(rig *Rig, assets *PetPackRigAssets) error {
	if rig == nil || rig.Version != 1 {
		return fmt.Errorf("unsupported pet-rig version")
	}
	if len(rig.Bones) == 0 || len(rig.Bones) > maxRigBones {
		return fmt.Errorf("rig must contain 1-%d bones", maxRigBones)
	}
	if len(rig.Slots) == 0 || len(rig.Slots) > maxRigSlots {
		return fmt.Errorf("rig must contain 1-%d slots", maxRigSlots)
	}
	if assets == nil || len(assets.Textures) == 0 || len(assets.Textures) > maxRigTextures {
		return fmt.Errorf("rig must declare 1-%d textures", maxRigTextures)
	}
	bones := make(map[string]bool, len(rig.Bones))
	for _, bone := range rig.Bones {
		if !validRigName(bone.Name) || bones[bone.Name] {
			return fmt.Errorf("invalid or duplicate bone %q", bone.Name)
		}
		bones[bone.Name] = true
	}
	parents := make(map[string]string, len(rig.Bones))
	for _, bone := range rig.Bones {
		if bone.Parent != "" && !bones[bone.Parent] {
			return fmt.Errorf("bone %q references missing parent %q", bone.Name, bone.Parent)
		}
		parents[bone.Name] = bone.Parent
	}
	// Parent existence is not enough: a cycle would otherwise pass validation
	// and only fail later in the frame renderer. Reject it during install/scan
	// so desktop and Pet Store report the same actionable error.
	for _, bone := range rig.Bones {
		seen := map[string]bool{bone.Name: true}
		parent := bone.Parent
		for parent != "" {
			if seen[parent] {
				return fmt.Errorf("bone hierarchy contains a cycle at %q", parent)
			}
			seen[parent] = true
			parent = parents[parent]
		}
	}
	textures := make(map[string]bool, len(assets.Textures))
	for _, texture := range assets.Textures {
		texture = safeRel(texture)
		if texture == "" || textures[texture] || !isRigTexture(texture) {
			return fmt.Errorf("invalid or duplicate rig texture %q", texture)
		}
		textures[texture] = true
	}
	slotNames := make(map[string]bool, len(rig.Slots))
	for _, slot := range rig.Slots {
		if !validRigName(slot.Name) || !bones[slot.Bone] {
			return fmt.Errorf("slot %q references invalid bone", slot.Name)
		}
		if slotNames[slot.Name] {
			return fmt.Errorf("duplicate slot name %q", slot.Name)
		}
		slotNames[slot.Name] = true
		if !textures[safeRel(slot.Texture)] {
			return fmt.Errorf("slot %q references undeclared texture", slot.Name)
		}
	}
	totalKeyframes := 0
	for clipName, clip := range rig.Clips {
		if !validRigName(clipName) || clip.DurationMS < 1 || clip.DurationMS > 60000 {
			return fmt.Errorf("invalid clip %q", clipName)
		}
		for track, frames := range clip.Tracks {
			if !bones[track] && !slotNames[track] {
				return fmt.Errorf("clip %q references unknown track %q", clipName, track)
			}
			if len(frames) > maxRigKeyframes {
				return fmt.Errorf("clip %q track %q exceeds %d keyframes", clipName, track, maxRigKeyframes)
			}
			totalKeyframes += len(frames)
			if totalKeyframes > maxRigTotalKeyframes {
				return fmt.Errorf("rig exceeds %d total keyframes", maxRigTotalKeyframes)
			}
			last := -1
			for _, frame := range frames {
				if frame.AtMS < 0 || frame.AtMS > clip.DurationMS || frame.AtMS <= last {
					return fmt.Errorf("clip %q track %q keyframes must be strictly increasing", clipName, track)
				}
				if frame.Ease != "" && frame.Ease != "linear" && frame.Ease != "ease-out" && frame.Ease != "ease-in-out" {
					return fmt.Errorf("clip %q track %q has unsupported easing", clipName, track)
				}
				last = frame.AtMS
			}
		}
	}
	if err := validateRigJoin(rig.Join, textures); err != nil {
		return err
	}
	return nil
}

func validateRigJoin(join *RigJoin, textures map[string]bool) error {
	if join == nil {
		return nil
	}
	if join.CollarCoverFrac != 0 && (join.CollarCoverFrac < 0.05 || join.CollarCoverFrac > 0.40) {
		return fmt.Errorf("join.collar_cover_frac must be in 0.05–0.40")
	}
	if join.HeadNeckFadePx < 0 || join.HeadNeckFadePx > 48 {
		return fmt.Errorf("join.head_neck_fade_px must be in 0–48")
	}
	if join.HeadNeckFadeCenter != 0 && (join.HeadNeckFadeCenter < 0.15 || join.HeadNeckFadeCenter > 0.85) {
		return fmt.Errorf("join.head_neck_fade_center must be in 0.15–0.85")
	}
	if join.ChinInsetPx < 0 || join.ChinInsetPx > 40 {
		return fmt.Errorf("join.chin_inset_px must be in 0–40")
	}
	if overlay := safeRel(join.CollarOverlay); overlay != "" {
		if !textures[overlay] {
			return fmt.Errorf("join.collar_overlay references undeclared texture %q", overlay)
		}
	}
	for state, off := range join.StateHeadOffset {
		if !isRuntimeStateName(state) {
			return fmt.Errorf("join.state_head_offset unknown state %q", state)
		}
		if mathAbs(off.X) > 32 || mathAbs(off.Y) > 32 {
			return fmt.Errorf("join.state_head_offset %q offset exceeds ±32", state)
		}
	}
	return nil
}

func isRuntimeStateName(state string) bool {
	switch state {
	case "idle", "listening", "thinking", "speaking", "done", "alert", "quiet":
		return true
	default:
		return false
	}
}

func mathAbs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// ValidateRigTextureData validates the decoded-image budget shared by local
// install, bundled scans, and Pet Store upload review. The manifest's texture
// allowlist must be supplied as entries in data; no renderer gets to discover
// arbitrary files on demand.
func ValidateRigTextureData(assets *PetPackRigAssets, data map[string][]byte) error {
	if assets == nil {
		return fmt.Errorf("native-skeleton pack is missing rig assets")
	}
	var totalPixels int64
	for _, texture := range assets.Textures {
		texture = safeRel(texture)
		raw, ok := data[texture]
		if !ok || len(raw) == 0 {
			return fmt.Errorf("rig texture not found: %s", texture)
		}
		if len(raw) > maxRigTextureBytes {
			return fmt.Errorf("rig texture exceeds %d bytes: %s", maxRigTextureBytes, texture)
		}
		cfg, format, err := image.DecodeConfig(bytes.NewReader(raw))
		if err != nil {
			return fmt.Errorf("decode rig texture %s: %w", texture, err)
		}
		if format != "png" && format != "webp" {
			return fmt.Errorf("rig texture is not PNG/WebP: %s", texture)
		}
		if cfg.Width < 1 || cfg.Height < 1 || cfg.Width > maxRigTextureEdge || cfg.Height > maxRigTextureEdge {
			return fmt.Errorf("rig texture dimensions exceed %dx%d: %s", maxRigTextureEdge, maxRigTextureEdge, texture)
		}
		totalPixels += int64(cfg.Width) * int64(cfg.Height)
		if totalPixels > maxRigTexturePixels {
			return fmt.Errorf("rig textures exceed %d total pixels", maxRigTexturePixels)
		}
	}
	return nil
}

// ReadRigTextureData reads only declared local textures and applies the same
// byte budget before handing them to the image decoder.
func ReadRigTextureData(fsys fs.FS, assets *PetPackRigAssets) (map[string][]byte, error) {
	if fsys == nil || assets == nil {
		return nil, fmt.Errorf("rig asset filesystem unavailable")
	}
	data := make(map[string][]byte, len(assets.Textures))
	for _, texture := range assets.Textures {
		texture = safeRel(texture)
		if texture == "" {
			return nil, fmt.Errorf("invalid rig texture")
		}
		file, err := fsys.Open(texture)
		if err != nil {
			return nil, fmt.Errorf("rig texture not found: %s", texture)
		}
		raw, readErr := io.ReadAll(io.LimitReader(file, maxRigTextureBytes+1))
		closeErr := file.Close()
		if readErr != nil || closeErr != nil {
			return nil, fmt.Errorf("read rig texture: %s", texture)
		}
		data[texture] = raw
	}
	if err := ValidateRigTextureData(assets, data); err != nil {
		return nil, err
	}
	return data, nil
}

func validRigName(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

func isRigTexture(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".png" || ext == ".webp"
}

// SortedSlots gives renderers a stable painter order.
func (r *Rig) SortedSlots() []RigSlot {
	if r == nil {
		return nil
	}
	slots := append([]RigSlot(nil), r.Slots...)
	sort.SliceStable(slots, func(i, j int) bool { return slots[i].Z < slots[j].Z })
	return slots
}
