package petpack

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	_ "image/png"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	_ "golang.org/x/image/webp"
	xdraw "golang.org/x/image/draw"
)

// FrameKey identifies a decoded pack frame.
type FrameKey struct {
	PackID    string
	Variant   string
	State     PetRuntimeState
	Size      int
}

// FrameCache caches decoded+scaled pack frames.
type FrameCache struct {
	mu    sync.RWMutex
	items map[FrameKey]*image.NRGBA
}

// NewFrameCache constructs an empty cache.
func NewFrameCache() *FrameCache {
	return &FrameCache{items: make(map[FrameKey]*image.NRGBA)}
}

// Get returns a cached frame if present.
func (c *FrameCache) Get(k FrameKey) *image.NRGBA {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.items[k]
}

// Put stores a frame.
func (c *FrameCache) Put(k FrameKey, img *image.NRGBA) {
	if c == nil || img == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.items == nil {
		c.items = make(map[FrameKey]*image.NRGBA)
	}
	// Soft cap: drop roughly half the entries instead of wiping the entire cache.
	if len(c.items) > 256 {
		drop := len(c.items) / 2
		if drop < 1 {
			drop = 1
		}
		n := 0
		for key := range c.items {
			delete(c.items, key)
			n++
			if n >= drop {
				break
			}
		}
	}
	c.items[k] = img
}

// LoadNativeFrame loads and scales a state frame for the resolved pack.
// Returns nil when the variant is procedural or the frame file is missing/undecodable.
func LoadNativeFrame(resolved *ResolvedPack, state PetRuntimeState, size int) (*image.NRGBA, error) {
	if resolved == nil {
		return nil, nil
	}
	if resolved.Renderer == RendererProcedural || strings.EqualFold(resolved.VariantID, VariantClassic) {
		// Classic variant intentionally uses procedural draw; optional native may still exist.
		// Prefer native if present, else nil → caller falls back.
	}
	rel := pickStatePath(resolved.Native, state)
	if rel == "" {
		return nil, nil
	}
	raw, err := readAsset(resolved, rel)
	if err != nil {
		return nil, err
	}
	img, err := decodeImage(raw)
	if err != nil {
		return nil, err
	}
	if size <= 0 {
		size = 88
	}
	return scaleToNRGBA(img, size), nil
}

// ResolveAndLoad is a convenience that Resolve + LoadNativeFrame with caching.
func (r *Registry) ResolveAndLoad(packID, variant string, state PetRuntimeState, size int, cache *FrameCache) (*image.NRGBA, *ResolvedPack, error) {
	if r == nil {
		return nil, nil, fmt.Errorf("nil registry")
	}
	resolved, err := r.Resolve(packID, variant)
	if err != nil || resolved == nil {
		return nil, resolved, err
	}
	key := FrameKey{PackID: resolved.Manifest.ID, Variant: resolved.VariantID, State: state, Size: size}
	if cache != nil {
		if hit := cache.Get(key); hit != nil {
			return hit, resolved, nil
		}
	}
	frame, err := LoadNativeFrame(resolved, state, size)
	if err != nil {
		return nil, resolved, err
	}
	if frame != nil && cache != nil {
		cache.Put(key, frame)
	}
	return frame, resolved, nil
}

func pickStatePath(native map[string]string, state PetRuntimeState) string {
	if len(native) == 0 {
		return ""
	}
	// exact
	if p := native[string(state)]; p != "" {
		return p
	}
	// fallback chain
	switch state {
	case StateDone, StateAlert, StateQuiet, StateListening, StateThinking, StateSpeaking:
		if p := native[string(StateIdle)]; p != "" {
			return p
		}
	}
	// any idle-like
	for _, k := range []string{"idle", "default", "base"} {
		if p := native[k]; p != "" {
			return p
		}
	}
	// first available
	for _, p := range native {
		if p != "" {
			return p
		}
	}
	return ""
}

func readAsset(resolved *ResolvedPack, rel string) ([]byte, error) {
	rel = safeRel(rel)
	if rel == "" {
		return nil, fmt.Errorf("unsafe asset path")
	}
	if resolved.AssetFS != nil {
		return fs.ReadFile(resolved.AssetFS, rel)
	}
	if resolved.AssetRoot == "" {
		return nil, fmt.Errorf("no asset source")
	}
	full := filepath.Join(resolved.AssetRoot, filepath.FromSlash(rel))
	if err := pathUnderRoot(resolved.AssetRoot, full); err != nil {
		return nil, err
	}
	return os.ReadFile(full)
}

func decodeImage(raw []byte) (image.Image, error) {
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	return img, nil
}

func scaleToNRGBA(src image.Image, size int) *image.NRGBA {
	if size <= 0 {
		size = 88
	}
	dst := image.NewNRGBA(image.Rect(0, 0, size, size))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)
	return dst
}

// HasNativeFrame reports whether Resolve would find a loadable native path for state.
func (r *Registry) HasNativeFrame(packID, variant string, state PetRuntimeState) bool {
	resolved, err := r.Resolve(packID, variant)
	if err != nil || resolved == nil {
		return false
	}
	if resolved.Renderer == RendererProcedural && ResolveVariantForRuntime(variant) == VariantClassic {
		// classic may still have native; check map
	}
	return pickStatePath(resolved.Native, state) != ""
}
