package petpack

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// LoadPreviewBytes returns raw image bytes and MIME type for a pack preview.
// Preference: preview → assets.preview → native idle (variant default then top-level).
func (r *Registry) LoadPreviewBytes(packID string) (data []byte, contentType string, err error) {
	m, err := r.getManifest(packID)
	if err != nil {
		return nil, "", err
	}
	candidates := previewCandidateRels(m)
	if len(candidates) == 0 {
		return nil, "", fmt.Errorf("pack %q has no preview asset", packID)
	}
	return r.readFirstAsset(m, candidates)
}

// LoadStateFrameBytes returns image bytes for settings preview / diagnostics.
// Preference order:
//  1. Live rig / character render (so Join v1 collar cover is visible in settings)
//  2. Declared native/<state>.png raster fallback
//
// The retired presentation selector resolves to the default runtime variant.
func (r *Registry) LoadStateFrameBytes(packID, state, variant string) (data []byte, contentType string, err error) {
	m, err := r.getManifest(packID)
	if err != nil {
		return nil, "", err
	}
	st := NormalizeState(state)

	// Prefer live multi-part / skeleton render when the pack can load a rig.
	if pngBytes, liveErr := r.renderLiveStateFramePNG(packID, variant, st, 256); liveErr == nil && len(pngBytes) > 0 {
		return pngBytes, "image/png", nil
	}

	candidates := stateFrameCandidateRels(m, string(st))
	if len(candidates) == 0 {
		return nil, "", fmt.Errorf("pack %q has no frame for state %q", packID, st)
	}
	return r.readFirstAsset(m, candidates)
}

// renderLiveStateFramePNG composites one rest-pose frame through the same
// RigRenderer / CharacterRenderer path the floating pet uses (including join).
func (r *Registry) renderLiveStateFramePNG(packID, variant string, state PetRuntimeState, size int) ([]byte, error) {
	if r == nil {
		return nil, fmt.Errorf("nil registry")
	}
	if size <= 0 {
		size = 256
	}
	resolved, err := r.Resolve(packID, variant)
	if err != nil || resolved == nil {
		return nil, fmt.Errorf("resolve pack: %w", err)
	}
	if resolved.Renderer != RendererCharacter && resolved.Renderer != RendererSkeleton {
		return nil, fmt.Errorf("renderer %q has no live rig path", resolved.Renderer)
	}

	// Prefer full character director (expression + body clips) when available.
	if resolved.Renderer == RendererCharacter {
		if cr, err := NewCharacterRenderer(resolved); err == nil && cr != nil {
			if img := cr.RenderState(state, 0, size); img != nil {
				return encodePNG(img)
			}
		}
	}

	// Skeleton path, or character fallback when performer failed to load.
	skel := *resolved
	skel.Renderer = RendererSkeleton
	rr, err := NewRigRenderer(&skel)
	if err != nil || rr == nil {
		return nil, fmt.Errorf("rig renderer unavailable")
	}
	img := renderStateWithExpression(rr, state, size)
	if img == nil {
		return nil, fmt.Errorf("skeleton render returned nil")
	}
	return encodePNG(img)
}

func renderStateWithExpression(rr *RigRenderer, state PetRuntimeState, size int) *image.NRGBA {
	if rr == nil {
		return nil
	}
	bodyClip := bodyClipForState(rr, state)
	exprClip := expressionClipForState(rr, state)
	if bodyClip != "" && exprClip != "" {
		if img := rr.RenderClips([]string{bodyClip, exprClip}, 0, size); img != nil {
			return img
		}
	}
	if bodyClip != "" {
		if img := rr.RenderClip(bodyClip, 0, size); img != nil {
			return img
		}
	}
	// Avoid recursion through Render's multiHead path; fall back to raw state clip.
	if clip := rr.clipForState(state); clip != nil {
		return rr.renderClip(clip, 0, size)
	}
	return nil
}

func firstExistingClip(rr *RigRenderer, names ...string) string {
	if rr == nil {
		return ""
	}
	for _, name := range names {
		if name != "" && rr.clips[name] != nil {
			return name
		}
	}
	return ""
}

// bodyClipForState picks a body motion clip for settings/skeleton multi-part
// previews. Prefer state loops (listening_loop, speaking_loop, …) so changing
// the state does more than swap expression faces.
func bodyClipForState(rr *RigRenderer, state PetRuntimeState) string {
	if rr == nil {
		return ""
	}
	st := string(state)
	candidates := map[PetRuntimeState][]string{
		StateIdle:      {"idle_breathe_loop", "idle_breathe", "idle"},
		StateListening: {"listening_loop", "listening", "idle_breathe_loop", "idle"},
		StateThinking:  {"thinking_loop", "thinking", "idle_breathe_loop", "idle"},
		StateSpeaking:  {"speaking_loop", "speaking", "idle_breathe_loop", "idle"},
		StateDone:      {"done_loop", "done", "idle_breathe_loop", "idle"},
		StateAlert:     {"alert_loop", "alert", "idle_breathe_loop", "idle"},
		StateQuiet:     {"quiet_loop", "quiet", "idle_breathe_loop", "idle"},
	}
	if list := candidates[state]; len(list) > 0 {
		return firstExistingClip(rr, list...)
	}
	return firstExistingClip(rr, st+"_loop", st, "idle_breathe_loop", "idle")
}

func expressionClipForState(rr *RigRenderer, state PetRuntimeState) string {
	if rr == nil || rr.clips == nil {
		return ""
	}
	// Common performer mapping used by multi-part packs.
	candidates := map[PetRuntimeState][]string{
		StateIdle:      {"expr_soft", "expr_calm", "expr_idle"},
		StateListening: {"expr_focus", "expr_listen", "expr_curious"},
		StateThinking:  {"expr_think", "expr_focus"},
		StateSpeaking:  {"expr_talk", "expr_speak"},
		StateDone:      {"expr_pleased", "expr_done"},
		StateAlert:     {"expr_concerned", "expr_alert"},
		StateQuiet:     {"expr_tired", "expr_quiet"},
	}
	for _, name := range candidates[state] {
		if rr.clips[name] != nil {
			return name
		}
	}
	return ""
}

func encodePNG(img image.Image) ([]byte, error) {
	if img == nil {
		return nil, fmt.Errorf("nil image")
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (r *Registry) getManifest(packID string) (*PetPackManifest, error) {
	if r == nil {
		return nil, fmt.Errorf("nil registry")
	}
	packID = strings.TrimSpace(packID)
	if packID == "" {
		return nil, fmt.Errorf("empty pack id")
	}
	r.mu.RLock()
	m, ok := r.packs[packID]
	r.mu.RUnlock()
	if !ok || m == nil {
		return nil, fmt.Errorf("pack %q not found", packID)
	}
	return m, nil
}

func (r *Registry) readFirstAsset(m *PetPackManifest, candidates []string) ([]byte, string, error) {
	if m == nil || len(candidates) == 0 {
		return nil, "", fmt.Errorf("no candidates")
	}
	// User / disk first
	if m.Dir != "" && (m.Scope == ScopeUser || filepath.IsAbs(m.Dir)) {
		for _, rel := range candidates {
			rel = safeRel(rel)
			if rel == "" {
				continue
			}
			full := filepath.Join(m.Dir, filepath.FromSlash(rel))
			if err := pathUnderRoot(m.Dir, full); err != nil {
				continue
			}
			raw, readErr := os.ReadFile(full)
			if readErr != nil || len(raw) == 0 {
				continue
			}
			return raw, mimeFromExt(rel), nil
		}
	}

	// Bundled embed
	r.mu.RLock()
	bundled := r.bundled
	r.mu.RUnlock()
	if bundled != nil {
		base := m.Dir
		if base == "" {
			base = m.ID
		}
		roots := []string{base, filepath.ToSlash(filepath.Join("bundled", m.ID)), m.ID}
		for _, root := range roots {
			sub, subErr := fs.Sub(bundled, filepath.ToSlash(root))
			if subErr != nil {
				for _, rel := range candidates {
					path := filepath.ToSlash(filepath.Join(root, rel))
					raw, readErr := fs.ReadFile(bundled, path)
					if readErr != nil || len(raw) == 0 {
						continue
					}
					return raw, mimeFromExt(rel), nil
				}
				continue
			}
			for _, rel := range candidates {
				raw, readErr := fs.ReadFile(sub, filepath.ToSlash(rel))
				if readErr != nil || len(raw) == 0 {
					continue
				}
				return raw, mimeFromExt(rel), nil
			}
		}
	}
	return nil, "", fmt.Errorf("asset not readable for pack %q", m.ID)
}

func previewCandidateRels(m *PetPackManifest) []string {
	if m == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	add := func(rel string) {
		rel = safeRel(rel)
		if rel == "" || seen[rel] {
			return
		}
		seen[rel] = true
		out = append(out, rel)
	}
	add(m.Preview)
	add(m.Assets.Preview)
	add(DefaultNativeAssets(m)["idle"])
	return out
}

func stateFrameCandidateRels(m *PetPackManifest, state string) []string {
	if m == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	add := func(rel string) {
		rel = safeRel(rel)
		if rel == "" || seen[rel] {
			return
		}
		seen[rel] = true
		out = append(out, rel)
	}
	// The runtime either uses the default variant's native map in full or the
	// top-level map in full; never combine their states.
	addNative := func(native map[string]string) {
		if native == nil {
			return
		}
		add(native[state])
		if state != string(StateIdle) {
			add(native[string(StateIdle)])
		}
	}
	addNative(DefaultNativeAssets(m))
	return out
}

func mimeFromExt(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".webp":
		return "image/webp"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	default:
		return "image/png"
	}
}
