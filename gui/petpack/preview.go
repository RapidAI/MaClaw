package petpack

import (
	"fmt"
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

// LoadStateFrameBytes returns native state frame bytes for settings preview / diagnostics.
// variant empty resolves as classic at runtime, but for figurative frames prefer default/figurative assets.
// Missing state falls back to idle among the same candidate set.
func (r *Registry) LoadStateFrameBytes(packID, state, variant string) (data []byte, contentType string, err error) {
	m, err := r.getManifest(packID)
	if err != nil {
		return nil, "", err
	}
	st := string(NormalizeState(state))
	// The retired quality-style selector resolves every legacy variant to the
	// pack default, so preview always serves the pack's raster frames.
	candidates := stateFrameCandidateRels(m, st)
	if len(candidates) == 0 {
		return nil, "", fmt.Errorf("pack %q has no frame for state %q", packID, st)
	}
	return r.readFirstAsset(m, candidates)
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
	for _, v := range m.Variants {
		if v.ID == VariantDefault || v.Tier == "figurative" {
			if v.Assets.Native != nil {
				add(v.Assets.Native["idle"])
			}
		}
	}
	if m.Assets.Native != nil {
		add(m.Assets.Native["idle"])
	}
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
	// Prefer figurative/default variant assets for the requested state, then idle, then top-level.
	addNative := func(native map[string]string) {
		if native == nil {
			return
		}
		add(native[state])
		if state != string(StateIdle) {
			add(native[string(StateIdle)])
		}
	}
	for _, v := range m.Variants {
		if v.ID == VariantDefault || v.Tier == "figurative" || v.Renderer == RendererNative {
			addNative(v.Assets.Native)
		}
	}
	addNative(m.Assets.Native)
	// Any variant idle as last resort
	for _, v := range m.Variants {
		if v.Assets.Native != nil {
			add(v.Assets.Native[state])
			add(v.Assets.Native[string(StateIdle)])
		}
	}
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
