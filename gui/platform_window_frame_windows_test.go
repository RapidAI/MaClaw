//go:build windows

package main

import (
	"image"
	"image/color"
	"math"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/gui/petpack"
)

func TestWindowsFramelessShellIsOpaque(t *testing.T) {
	app := &App{}
	webviewTransparent, windowTranslucent := app.PlatformTransparencyFlags()
	if webviewTransparent || windowTranslucent {
		t.Fatalf("Win10-safe frameless shell must be opaque, got webviewTransparent=%t windowTranslucent=%t", webviewTransparent, windowTranslucent)
	}
}

func TestNormalizeFramelessTopInsetUsesCSSPixels(t *testing.T) {
	tests := []struct {
		name     string
		physical int
		dpi      int
		want     int
	}{
		{name: "100 percent", physical: 8, dpi: 96, want: 8},
		{name: "125 percent", physical: 10, dpi: 120, want: 8},
		{name: "150 percent", physical: 12, dpi: 144, want: 8},
		{name: "invalid values", physical: -1, dpi: 144, want: 0},
		{name: "excessive inset", physical: 40, dpi: 96, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeFramelessTopInset(tt.physical, tt.dpi); got != tt.want {
				t.Fatalf("normalizeFramelessTopInset(%d, %d) = %d, want %d", tt.physical, tt.dpi, got, tt.want)
			}
		})
	}
}

func TestRenderAnimatedPetFrameCrossfadesStateImages(t *testing.T) {
	previous := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	current := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			previous.SetNRGBA(x, y, color.NRGBA{R: 255, A: 255})
			current.SetNRGBA(x, y, color.NRGBA{B: 255, A: 255})
		}
	}

	dst := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	renderAnimatedPetFrame(dst, current, previous, 1, 0, 0, 0.5)
	pixel := dst.NRGBAAt(4, 4)
	if pixel.R < 110 || pixel.B < 110 || pixel.A != 255 {
		t.Fatalf("crossfade pixel = %#v, want opaque blend of both frames", pixel)
	}
}

func TestAlphaOverNRGBAHonorsOpacity(t *testing.T) {
	dst := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	dst.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
	src := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	src.SetNRGBA(0, 0, color.NRGBA{B: 255, A: 255})

	alphaOverNRGBA(dst, src, 0.25)
	pixel := dst.NRGBAAt(0, 0)
	if pixel.R < 180 || pixel.B < 50 || pixel.B > 80 || pixel.A != 255 {
		t.Fatalf("opacity blend pixel = %#v, want 25%% blue over red", pixel)
	}
}

func TestPetStateTransitionPoseIsMeaningfulAndSettles(t *testing.T) {
	tests := []struct {
		state string
		check func(scale, x, y float64) bool
	}{
		{"listening", func(scale, x, y float64) bool { return scale > 1 && x < 0 && y > 0 }},
		{"thinking", func(scale, x, y float64) bool { return scale < 1 && x > 0 && y > 0 }},
		{"speaking", func(scale, x, y float64) bool { return scale > 1 && y > 0 }},
		{"done", func(scale, x, y float64) bool { return scale > 1 && y < 0 }},
		{"alert", func(scale, x, y float64) bool { return scale > 1 && x > 0 }},
	}
	for _, tt := range tests {
		t.Run(tt.state, func(t *testing.T) {
			scale, x, y := petStateTransitionPose(tt.state, 0, 88, 0.85)
			if !tt.check(scale, x, y) {
				t.Fatalf("entry pose = scale %.3f, x %.3f, y %.3f", scale, x, y)
			}
			settledScale, settledX, settledY := petStateTransitionPose(tt.state, 1, 88, 0.85)
			if settledScale != 1 || settledX != 0 || settledY != 0 {
				t.Fatalf("settled pose = scale %.3f, x %.3f, y %.3f", settledScale, settledX, settledY)
			}
		})
	}
}

func TestResolvePetMotionAmplitudeHonorsMotionPreferences(t *testing.T) {
	if got := resolvePetMotionAmplitude("missing-pack", "balanced", false, true, true, true); got != 0 {
		t.Fatalf("reduced motion amplitude = %v, want 0", got)
	}
	if got := resolvePetMotionAmplitude("missing-pack", "balanced", false, false, false, true); got != 0 {
		t.Fatalf("motion disabled amplitude = %v, want 0", got)
	}

	reg := petpack.NewRegistry(t.TempDir(), petpack.BundledFS())
	if err := reg.Scan(); err != nil {
		t.Fatal(err)
	}
	petpack.SetGlobalForTest(reg)
	if got := resolvePetMotionAmplitude("clawmate", "balanced", false, false, true, true); got <= 0 || got > 1 {
		t.Fatalf("pack amplitude = %v, want value in (0, 1]", got)
	}
}

func TestPetRuntimeStateExpiryUsesTransitionPath(t *testing.T) {
	w := &windowsFloatingWindow{
		petRuntimeState:  "speaking",
		petStateDeadline: time.Now().Add(-time.Millisecond),
	}
	if got := w.CurrentPetRuntimeState(); got != "idle" {
		t.Fatalf("expired state = %q, want idle", got)
	}
	if w.petPreviousState != "speaking" || w.petStateChangedAt.IsZero() {
		t.Fatalf("expiry should preserve outgoing state for transition, previous=%q changedAt=%v", w.petPreviousState, w.petStateChangedAt)
	}
}

func TestMotionConfigRevisionRejectsStaleLookup(t *testing.T) {
	w := &windowsFloatingWindow{motionConfigRevision: 2, petMotionAmplitude: 0.7}
	if w.applyPetMotionAmplitude(1, 0.2) {
		t.Fatal("stale lookup should be rejected")
	}
	if w.petMotionAmplitude != 0.7 {
		t.Fatalf("stale lookup overwrote newer amplitude: %v", w.petMotionAmplitude)
	}
	if !w.applyPetMotionAmplitude(2, 0.2) || w.petMotionAmplitude != 0.2 {
		t.Fatalf("current lookup should update amplitude: %v", w.petMotionAmplitude)
	}
}

func TestMotionConfigRestoresRequestedSoundAfterQuietMode(t *testing.T) {
	w := &windowsFloatingWindow{petMotionSoundRequested: true, petMotionSound: true}
	w.UpdateMotionConfig(true, true, false, "balanced", "missing-pack", "default")
	if w.petMotionSound || !w.petMotionSoundRequested {
		t.Fatalf("quiet mode sound = effective:%v requested:%v, want false/true", w.petMotionSound, w.petMotionSoundRequested)
	}
	w.UpdateMotionConfig(true, false, false, "balanced", "missing-pack", "default")
	if !w.petMotionSound {
		t.Fatal("sound preference should resume after quiet mode is disabled")
	}
}

func TestSoundUpdateKeepsPetFrameCaches(t *testing.T) {
	key := petFrameCacheKey{State: "idle"}
	frame := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	frames := map[petFrameCacheKey]*image.NRGBA{key: frame}
	packFrames := petpack.NewFrameCache()
	w := &windowsFloatingWindow{
		petMotionSoundRequested: true,
		petMotionSound:          true,
		petMotionEnabled:        true,
		petSkin:                 "missing-pack",
		petInteractionMode:      "balanced",
		petFrameCache:           frames,
		packFrameCache:          packFrames,
	}
	w.UpdateSoundConfig(false, "soft")
	if w.petFrameCache[key] != frame || w.packFrameCache != packFrames {
		t.Fatal("sound-only update must keep decoded and rendered pet frame caches")
	}
}

func TestMotionUpdateKeepsPetFrameCaches(t *testing.T) {
	key := petFrameCacheKey{State: "idle"}
	frame := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	frames := map[petFrameCacheKey]*image.NRGBA{key: frame}
	packFrames := petpack.NewFrameCache()
	w := &windowsFloatingWindow{
		petMotionSoundRequested: true,
		petMotionSound:          true,
		petMotionEnabled:        true,
		petSkin:                 "missing-pack",
		petInteractionMode:      "balanced",
		petFrameCache:           frames,
		packFrameCache:          packFrames,
	}
	w.UpdateMotionConfig(true, false, false, "active", "missing-pack", "default")
	if w.petFrameCache[key] != frame || w.packFrameCache != packFrames {
		t.Fatal("motion-only update must keep decoded and rendered pet frame caches")
	}
}

func TestProceduralPetFramesKeepAnimationBuckets(t *testing.T) {
	w := &windowsFloatingWindow{
		petFrameCache:           make(map[petFrameCacheKey]*image.NRGBA),
		petNativeFrameAvailable: make(map[petFrameCacheKey]bool),
		packFrameCache:          petpack.NewFrameCache(),
	}
	first := w.cachedPetFrame(32, "missing-pack", "default", "idle", "balanced", 0)
	second := w.cachedPetFrame(32, "missing-pack", "default", "idle", "balanced", math.Pi)
	if first == nil || second == nil || first == second {
		t.Fatal("procedural fallback should retain distinct phase-bucket frames")
	}
}

func TestPetAnimationBucketBudgetStaysBounded(t *testing.T) {
	if petAnimationFrameBuckets > 24 {
		t.Fatalf("procedural animation bucket budget = %d, want at most 24", petAnimationFrameBuckets)
	}
}

func TestPetFrameCacheEvictsWholeProceduralCycle(t *testing.T) {
	w := &windowsFloatingWindow{
		petFrameCache:           make(map[petFrameCacheKey]*image.NRGBA),
		petNativeFrameAvailable: make(map[petFrameCacheKey]bool),
	}
	oldCycle := petFrameCacheKey{Size: 32, Skin: "old", Variant: "default", State: "idle", Mode: "balanced"}
	for bucket := 0; bucket < petAnimationFrameBuckets; bucket++ {
		key := oldCycle
		key.Bucket = bucket
		w.petFrameCache[key] = image.NewNRGBA(image.Rect(0, 0, 1, 1))
	}
	w.petNativeFrameAvailable[oldCycle] = false
	for i := len(w.petFrameCache); i < petFrameCacheLimit; i++ {
		key := petFrameCacheKey{Size: 32, Skin: "native", Variant: "default", State: string(rune(i)), Mode: "balanced"}
		w.petFrameCache[key] = image.NewNRGBA(image.Rect(0, 0, 1, 1))
		w.petNativeFrameAvailable[key] = true
	}

	w.evictPetFrameCacheCycleLocked(petFrameCacheKey{Size: 32, Skin: "new", Variant: "default", State: "idle", Mode: "balanced"})
	for bucket := 0; bucket < petAnimationFrameBuckets; bucket++ {
		key := oldCycle
		key.Bucket = bucket
		if w.petFrameCache[key] != nil {
			t.Fatalf("old procedural bucket %d should be evicted with its cycle", bucket)
		}
	}
	if _, known := w.petNativeFrameAvailable[oldCycle]; known {
		t.Fatal("evicted procedural cycle should not retain native-availability metadata")
	}
}

func TestPetFrameCacheDoesNotEvictIncomingProceduralCycle(t *testing.T) {
	w := &windowsFloatingWindow{
		petFrameCache:           make(map[petFrameCacheKey]*image.NRGBA),
		petNativeFrameAvailable: make(map[petFrameCacheKey]bool),
	}
	incoming := petFrameCacheKey{Size: 32, Skin: "incoming", Variant: "default", State: "idle", Mode: "balanced"}
	for bucket := 0; bucket < petAnimationFrameBuckets; bucket++ {
		key := incoming
		key.Bucket = bucket
		w.petFrameCache[key] = image.NewNRGBA(image.Rect(0, 0, 1, 1))
	}
	for i := len(w.petFrameCache); i < petFrameCacheLimit; i++ {
		key := petFrameCacheKey{Size: 32, Skin: "old", Variant: "default", State: string(rune(i + 100)), Mode: "balanced"}
		w.petFrameCache[key] = image.NewNRGBA(image.Rect(0, 0, 1, 1))
	}

	incoming.Bucket = petAnimationFrameBuckets - 1
	w.evictPetFrameCacheCycleLocked(incoming)
	for bucket := 0; bucket < petAnimationFrameBuckets; bucket++ {
		key := incoming
		key.Bucket = bucket
		if w.petFrameCache[key] == nil {
			t.Fatalf("incoming procedural bucket %d should remain cached", bucket)
		}
	}
}

func TestPetFrameCacheEvictsNativeAvailabilityWithNativeFrame(t *testing.T) {
	w := &windowsFloatingWindow{
		petFrameCache:           make(map[petFrameCacheKey]*image.NRGBA),
		petNativeFrameAvailable: make(map[petFrameCacheKey]bool),
	}
	for i := 0; i < petFrameCacheLimit; i++ {
		key := petFrameCacheKey{Size: 32, Skin: "native", Variant: "default", State: string(rune(i + 1000)), Mode: "balanced"}
		w.petFrameCache[key] = image.NewNRGBA(image.Rect(0, 0, 1, 1))
		w.petNativeFrameAvailable[key] = true
	}

	w.evictPetFrameCacheCycleLocked(petFrameCacheKey{Size: 32, Skin: "new", Variant: "default", State: "idle", Mode: "balanced"})
	if len(w.petFrameCache) != petFrameCacheLimit-1 {
		t.Fatalf("native cache size = %d, want %d", len(w.petFrameCache), petFrameCacheLimit-1)
	}
	for key := range w.petNativeFrameAvailable {
		if w.petFrameCache[key] == nil {
			t.Fatalf("stale native availability remained for evicted frame %#v", key)
		}
	}
}

func TestCachedPetFrameReloadsWhenNativeAvailabilityOutlivesFrame(t *testing.T) {
	key := petFrameCacheKey{Size: 32, Skin: "missing-pack", Variant: "default", State: "idle", Mode: "balanced"}
	w := &windowsFloatingWindow{
		petFrameCache:           make(map[petFrameCacheKey]*image.NRGBA),
		petNativeFrameAvailable: map[petFrameCacheKey]bool{key: true},
		petNativeFrameLoading:   make(map[petFrameCacheKey]bool),
		packFrameCache:          petpack.NewFrameCache(),
	}
	frame := w.cachedPetFrame(32, "missing-pack", "default", "idle", "balanced", 0)
	if frame == nil {
		t.Fatal("stale native availability should reload or fall back to a visible frame")
	}
	if w.petNativeFrameAvailable[key] {
		t.Fatal("failed native reload should replace stale availability with procedural fallback")
	}
}

func TestStaticPetRenderSkipsUnchangedTimerFrames(t *testing.T) {
	w := &windowsFloatingWindow{petReducedMotion: true}
	w.mu.Lock()
	w.renderDirty = false
	w.mu.Unlock()
	w.renderFrame()
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.renderDirty {
		t.Fatal("unchanged static pet frame should remain clean after timer tick")
	}
}

func TestLogoOnlyRenderSkipsUnchangedTimerFrames(t *testing.T) {
	w := &windowsFloatingWindow{}
	w.mu.Lock()
	w.renderDirty = false
	w.mu.Unlock()
	w.renderFrame()
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.renderDirty {
		t.Fatal("unchanged logo-only frame should remain clean after timer tick")
	}
}

func TestPetRuntimeStateRefreshDoesNotRestartTransition(t *testing.T) {
	changedAt := time.Now().Add(-time.Second)
	w := &windowsFloatingWindow{
		petRuntimeState:      "thinking",
		petLastRenderedState: "thinking",
		petStateChangedAt:    changedAt,
	}
	w.SetPetRuntimeState("thinking", 500)
	if !w.petStateChangedAt.Equal(changedAt) {
		t.Fatal("repeating the active state should renew its TTL without restarting its transition")
	}
	if w.renderDirty {
		t.Fatal("repeating an already rendered state should not request a redundant static redraw")
	}
	if w.petStateDeadline.Before(time.Now().Add(400 * time.Millisecond)) {
		t.Fatal("repeating an active state should renew its TTL")
	}
}

func TestQuietStateMarksStaticFrameDirtyOnce(t *testing.T) {
	w := &windowsFloatingWindow{petQuietMode: true, petRuntimeState: "speaking", petLastRenderedState: "speaking", renderDirty: false}
	w.renderFrame()
	if w.renderDirty {
		t.Fatal("quiet state frame should be marked dirty and consumed in the same render")
	}
	if w.petLastRenderedState != "quiet" {
		t.Fatalf("last rendered state = %q, want quiet", w.petLastRenderedState)
	}
}
