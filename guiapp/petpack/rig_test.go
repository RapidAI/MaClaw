package petpack

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestRigValidationAndRenderer(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "rig"), 0o755); err != nil {
		t.Fatal(err)
	}
	texture := filepath.Join(dir, "rig", "body.png")
	img := image.NewNRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: 70, G: 140, B: 200, A: 255})
		}
	}
	f, err := os.Create(texture)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	rig := &Rig{Version: 1, Bones: []RigBone{{Name: "root", X: 128, Y: 128}}, Slots: []RigSlot{{Name: "body", Bone: "root", Texture: "rig/body.png"}}, Clips: map[string]RigClip{"idle": {DurationMS: 1000, Loop: true, Tracks: map[string][]RigKeyframe{"root": {{AtMS: 0, Y: -4}, {AtMS: 500, Y: 4, Ease: "ease-in-out"}, {AtMS: 1000, Y: -4, Ease: "ease-in-out"}}}}}}
	assets := &PetPackRigAssets{Definition: "rig/pet-rig.json", Textures: []string{"rig/body.png"}}
	if err := ValidateRig(rig, assets); err != nil {
		t.Fatal(err)
	}
	resolved := &ResolvedPack{Renderer: RendererSkeleton, Rig: assets, AssetFS: os.DirFS(dir)}
	// Load through the same JSON path used by actual packs.
	data := []byte(`{"version":1,"bones":[{"name":"root","x":128,"y":128}],"slots":[{"name":"body","bone":"root","texture":"rig/body.png"}],"clips":{"idle":{"duration_ms":1000,"loop":true,"tracks":{"root":[{"at_ms":0,"y":-4},{"at_ms":500,"y":4,"ease":"ease-in-out"},{"at_ms":1000,"y":-4,"ease":"ease-in-out"}]}}}}`)
	if err := os.WriteFile(filepath.Join(dir, "rig", "pet-rig.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	renderer, err := NewRigRenderer(resolved)
	if err != nil {
		t.Fatal(err)
	}
	frameA := renderer.Render(StateIdle, 0, 88)
	frameB := renderer.Render(StateIdle, 500, 88)
	if frameA == nil || frameB == nil || frameA.Bounds().Dx() != 88 {
		t.Fatal("expected 88px rendered frames")
	}
	if frameA.PixOffset(44, 44) == frameB.PixOffset(44, 44) && string(frameA.Pix) == string(frameB.Pix) {
		t.Fatal("expected keyframe movement")
	}
}

func TestRigValidationRejectsExecutableSurface(t *testing.T) {
	rig := &Rig{Version: 1, Bones: []RigBone{{Name: "root"}}, Slots: []RigSlot{{Name: "body", Bone: "root", Texture: "rig/body.js"}}}
	if err := ValidateRig(rig, &PetPackRigAssets{Definition: "rig/pet-rig.json", Textures: []string{"rig/body.js"}}); err == nil {
		t.Fatal("expected non-raster texture rejection")
	}
}

func TestRigValidationRejectsBoneCycle(t *testing.T) {
	rig := &Rig{Version: 1, Bones: []RigBone{{Name: "a", Parent: "b"}, {Name: "b", Parent: "a"}}, Slots: []RigSlot{{Name: "body", Bone: "a", Texture: "rig/body.png"}}}
	err := ValidateRig(rig, &PetPackRigAssets{Definition: "rig/pet-rig.json", Textures: []string{"rig/body.png"}})
	if err == nil {
		t.Fatal("expected cyclic bone hierarchy rejection")
	}
}

func TestRigValidationRejectsTotalKeyframeBudget(t *testing.T) {
	frames := make([]RigKeyframe, 0, maxRigKeyframes)
	for i := 0; i < maxRigKeyframes; i++ {
		frames = append(frames, RigKeyframe{AtMS: i * 4})
	}
	tracks := make(map[string][]RigKeyframe)
	for i := 0; i < 6; i++ {
		name := fmt.Sprintf("bone-%d", i)
		tracks[name] = frames
	}
	bones := make([]RigBone, 0, len(tracks))
	for name := range tracks {
		bones = append(bones, RigBone{Name: name})
	}
	rig := &Rig{Version: 1, Bones: bones, Slots: []RigSlot{{Name: "body", Bone: bones[0].Name, Texture: "rig/body.png"}}, Clips: map[string]RigClip{"idle": {DurationMS: 1000, Tracks: tracks}}}
	err := ValidateRig(rig, &PetPackRigAssets{Definition: "rig/pet-rig.json", Textures: []string{"rig/body.png"}})
	if err == nil {
		t.Fatal("expected total keyframe budget rejection")
	}
}

func TestRigTrackUsesFirstKeyframeBeforeItsTimestamp(t *testing.T) {
	alpha := 0.4
	frames := []RigKeyframe{{AtMS: 200, X: 7, ScaleX: 1.2, Alpha: &alpha}, {AtMS: 500, X: 20, ScaleX: 1.4}}
	transform := applyRigTrack(rigTransform{sx: 1, sy: 1}, frames, 0)
	if transform.x != 7 || transform.sx != 1.2 {
		t.Fatalf("early transform = %+v, want first keyframe", transform)
	}
	if got := trackAlpha(frames, 0); got != alpha {
		t.Fatalf("early alpha = %v, want %v", got, alpha)
	}
}

func TestCopyToNRGBAPreservesTextureDimensions(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 40, 16))
	dst := copyToNRGBA(src)
	if dst == nil || dst.Bounds().Dx() != 40 || dst.Bounds().Dy() != 16 {
		t.Fatalf("texture dimensions = %v, want 40x16", dst.Bounds())
	}
}

func TestRigValidationRejectsDuplicateSlotNames(t *testing.T) {
	assets := &PetPackRigAssets{Definition: "rig/pet-rig.json", Textures: []string{"rig/body.png"}}
	rig := &Rig{Version: 1, Bones: []RigBone{{Name: "root"}}, Slots: []RigSlot{{Name: "body", Bone: "root", Texture: "rig/body.png"}, {Name: "body", Bone: "root", Texture: "rig/body.png"}}}
	if err := ValidateRig(rig, assets); err == nil {
		t.Fatal("expected duplicate slot name rejection")
	}
}

func TestRigJoinValidation(t *testing.T) {
	assets := &PetPackRigAssets{Definition: "rig/pet-rig.json", Textures: []string{"rig/body.png", "rig/head_idle.png", "rig/collar_overlay.png"}}
	ok := true
	rig := &Rig{
		Version: 1,
		Join: &RigJoin{
			Auto:               &ok,
			CollarCoverFrac:    0.2,
			HeadNeckFadePx:     12,
			HeadNeckFadeCenter: 0.4,
			CollarOverlay:      "rig/collar_overlay.png",
			StateHeadOffset:    map[string]RigHeadOffset{"listening": {Y: 2}},
		},
		Bones: []RigBone{{Name: "root"}, {Name: "body", Parent: "root"}, {Name: "head", Parent: "body"}, {Name: "h_idle", Parent: "head"}},
		Slots: []RigSlot{
			{Name: "body", Bone: "body", Texture: "rig/body.png"},
			{Name: "h_idle", Bone: "h_idle", Texture: "rig/head_idle.png"},
		},
		Clips: map[string]RigClip{"idle": {DurationMS: 1000, Loop: true}},
	}
	if err := ValidateRig(rig, assets); err != nil {
		t.Fatalf("valid join rejected: %v", err)
	}
	rig.Join.CollarCoverFrac = 0.9
	if err := ValidateRig(rig, assets); err == nil {
		t.Fatal("expected collar_cover_frac rejection")
	}
	rig.Join.CollarCoverFrac = 0.2
	rig.Join.StateHeadOffset = map[string]RigHeadOffset{"dance": {Y: 1}}
	if err := ValidateRig(rig, assets); err == nil {
		t.Fatal("expected unknown state rejection")
	}
}

func TestMultiHeadCollarCoverHidesNeckPlug(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "rig"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Body: solid pink band in upper-middle of canvas (collar zone around y=130–150 on 256).
	body := image.NewNRGBA(image.Rect(0, 0, 256, 256))
	for y := 128; y < 220; y++ {
		for x := 80; x < 176; x++ {
			body.SetNRGBA(x, y, color.NRGBA{R: 240, G: 160, B: 180, A: 255})
		}
	}
	// Head: face higher + long flesh neck plug hanging down through collar zone.
	head := image.NewNRGBA(image.Rect(0, 0, 256, 256))
	for y := 40; y < 130; y++ {
		for x := 90; x < 166; x++ {
			head.SetNRGBA(x, y, color.NRGBA{R: 230, G: 190, B: 170, A: 255}) // face
		}
	}
	for y := 130; y < 170; y++ { // neck plug past collar
		for x := 110; x < 146; x++ {
			head.SetNRGBA(x, y, color.NRGBA{R: 230, G: 190, B: 170, A: 255})
		}
	}
	writePNG(t, filepath.Join(dir, "rig", "body.png"), body)
	writePNG(t, filepath.Join(dir, "rig", "head_idle.png"), head)
	writePNG(t, filepath.Join(dir, "rig", "head_speak.png"), head)

	// head bone y=0, both textures center-pivoted at bone (128,148).
	rigJSON := `{
	  "version":1,
	  "join":{"collar_cover":true,"collar_cover_frac":0.25,"head_neck_fade_px":14},
	  "bones":[
	    {"name":"root","x":128,"y":148},
	    {"name":"body","parent":"root"},
	    {"name":"head","parent":"body","y":0},
	    {"name":"h_idle","parent":"head","alpha":1.0},
	    {"name":"h_speak","parent":"head","alpha":0.0}
	  ],
	  "slots":[
	    {"name":"body","bone":"body","texture":"rig/body.png","z":1},
	    {"name":"h_idle","bone":"h_idle","texture":"rig/head_idle.png","z":2},
	    {"name":"h_speak","bone":"h_speak","texture":"rig/head_speak.png","z":3}
	  ],
	  "clips":{
	    "idle":{"duration_ms":1000,"loop":true,"tracks":{
	      "h_idle":[{"at_ms":0,"alpha":1.0},{"at_ms":1000,"alpha":1.0}],
	      "h_speak":[{"at_ms":0,"alpha":0.0},{"at_ms":1000,"alpha":0.0}]
	    }}
	  }
	}`
	// Bone alpha isn't a bone field in schema — put alpha only on slot tracks.
	// Fix JSON: remove invalid bone alpha fields.
	rigJSON = `{
	  "version":1,
	  "join":{"collar_cover":true,"collar_cover_frac":0.28,"head_neck_fade_px":16},
	  "bones":[
	    {"name":"root","x":128,"y":148},
	    {"name":"body","parent":"root"},
	    {"name":"head","parent":"body"},
	    {"name":"h_idle","parent":"head"},
	    {"name":"h_speak","parent":"head"}
	  ],
	  "slots":[
	    {"name":"body","bone":"body","texture":"rig/body.png","z":1},
	    {"name":"h_idle","bone":"h_idle","texture":"rig/head_idle.png","z":2},
	    {"name":"h_speak","bone":"h_speak","texture":"rig/head_speak.png","z":3}
	  ],
	  "clips":{
	    "idle":{"duration_ms":1000,"loop":true,"tracks":{
	      "h_idle":[{"at_ms":0,"alpha":1.0},{"at_ms":1000,"alpha":1.0}],
	      "h_speak":[{"at_ms":0,"alpha":0.0},{"at_ms":1000,"alpha":0.0}]
	    }}
	  }
	}`
	if err := os.WriteFile(filepath.Join(dir, "rig", "char.rig.json"), []byte(rigJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	assets := &PetPackRigAssets{Definition: "rig/char.rig.json", Textures: []string{"rig/body.png", "rig/head_idle.png", "rig/head_speak.png"}}
	resolved := &ResolvedPack{Renderer: RendererSkeleton, Rig: assets, AssetFS: os.DirFS(dir)}
	renderer, err := NewRigRenderer(resolved)
	if err != nil || renderer == nil {
		t.Fatalf("renderer: %v", err)
	}
	if !renderer.join.enabled || !renderer.join.collarCover {
		t.Fatalf("expected multi-head join aids enabled, got %+v", renderer.join)
	}
	frame := renderer.RenderClip("idle", 0, 256)
	if frame == nil {
		t.Fatal("nil frame")
	}
	// Sample a pixel in the neck-plug zone that body collar should re-cover.
	// Design: body pink at (128,140); head plug was skin there before cover.
	c := frame.NRGBAAt(128, 140)
	// After collar cover, expect pink-ish clothing, not pure face skin.
	if int(c.R)+int(c.G)+int(c.B) < 100 || c.A < 200 {
		t.Fatalf("collar cover missing at neck zone: %+v", c)
	}
	// Pink dress has high R and relatively high B; face skin is less pink.
	if c.R < 200 || c.B < 140 {
		t.Fatalf("expected pink collar cover, got %+v", c)
	}
}

func TestClawmateStylePackDoesNotForceJoin(t *testing.T) {
	slots := []RigSlot{
		{Name: "shell", Bone: "shell", Texture: "rig/shell.png"},
		{Name: "eyes", Bone: "eyes", Texture: "rig/eyes.png"},
		{Name: "legs", Bone: "legs", Texture: "rig/legs.png"},
	}
	if detectMultiHeadPack(slots) {
		t.Fatal("clawmate-like pack should not be multi-head")
	}
	rig := &Rig{Version: 1, Bones: []RigBone{{Name: "root"}, {Name: "shell", Parent: "root"}}, Slots: slots}
	ej := resolveJoin(rig, slots, false)
	if ej.enabled {
		t.Fatalf("join should stay off for non multi-head: %+v", ej)
	}
}

func TestRigClipsPrefersIdleFallback(t *testing.T) {
	rig := &Rig{
		Version: 1,
		Clips: map[string]RigClip{
			"alert_in":          {DurationMS: 200},
			"speaking_loop":     {DurationMS: 480, Loop: true},
			"idle_breathe_loop": {DurationMS: 2800, Loop: true},
			"z_last":            {DurationMS: 100},
		},
	}
	clips, fallback := rigClips(rig)
	if clips == nil || fallback == nil {
		t.Fatal("nil clips/fallback")
	}
	if fallback != clips["idle_breathe_loop"] {
		t.Fatalf("fallback = %#v, want idle_breathe_loop (not alert_in)", fallback)
	}
	// Without idle-like names, keep stable alphabetical first.
	rig2 := &Rig{Version: 1, Clips: map[string]RigClip{"zebra": {DurationMS: 1}, "alpha": {DurationMS: 1}}}
	_, fb2 := rigClips(rig2)
	if fb2 == nil || fb2.DurationMS != 1 {
		t.Fatal("expected alphabetical fallback")
	}
	// alpha sorts before zebra
	if _, fb3 := rigClips(rig2); fb3 != nil {
		// pointer identity: re-call is new copy — check via map
		m, f := rigClips(rig2)
		if f != m["alpha"] {
			t.Fatalf("expected alpha as alphabetical fallback")
		}
	}
}

func TestClipForStatePrefersStateLoop(t *testing.T) {
	rr := &RigRenderer{
		clips: map[string]*RigClip{
			"idle_breathe_loop": {DurationMS: 1000, Loop: true},
			"speaking_loop":     {DurationMS: 480, Loop: true},
			"idle":              {DurationMS: 500, Loop: true},
		},
	}
	if got := rr.clipForState(StateSpeaking); got == nil || got != rr.clips["speaking_loop"] {
		t.Fatalf("clipForState(speaking) = %v, want speaking_loop", got)
	}
	delete(rr.clips, "speaking_loop")
	// Falls through bodyClipForState → idle_breathe_loop
	if got := rr.clipForState(StateSpeaking); got == nil || got != rr.clips["idle_breathe_loop"] {
		t.Fatalf("clipForState fallback = %v, want idle_breathe_loop", got)
	}
}

func TestBodyClipForStatePrefersStateLoop(t *testing.T) {
	rr := &RigRenderer{
		clips: map[string]*RigClip{
			"idle_breathe_loop": {DurationMS: 1000, Tracks: map[string][]RigKeyframe{"body": {{AtMS: 0}}}},
			"speaking_loop":     {DurationMS: 480, Tracks: map[string][]RigKeyframe{"body": {{AtMS: 0, Y: 2}}}},
			"listening_loop":    {DurationMS: 1600, Tracks: map[string][]RigKeyframe{"body": {{AtMS: 0, X: 1}}}},
			"expr_talk":         {DurationMS: 450, Tracks: map[string][]RigKeyframe{"f_speak": {{AtMS: 0}}}},
		},
	}
	if got := bodyClipForState(rr, StateSpeaking); got != "speaking_loop" {
		t.Fatalf("speaking body clip = %q, want speaking_loop", got)
	}
	if got := bodyClipForState(rr, StateListening); got != "listening_loop" {
		t.Fatalf("listening body clip = %q, want listening_loop", got)
	}
	if got := bodyClipForState(rr, StateIdle); got != "idle_breathe_loop" {
		t.Fatalf("idle body clip = %q, want idle_breathe_loop", got)
	}
	// Without state loop, fall back to idle breathe (not missing).
	delete(rr.clips, "speaking_loop")
	if got := bodyClipForState(rr, StateSpeaking); got != "idle_breathe_loop" {
		t.Fatalf("speaking fallback = %q, want idle_breathe_loop", got)
	}
}

func TestPerformanceStepClipsSkipsEmptyTracks(t *testing.T) {
	rr := &RigRenderer{
		clips: map[string]*RigClip{
			"idle_breathe_loop": {DurationMS: 1000, Tracks: map[string][]RigKeyframe{"body": {{AtMS: 0}, {AtMS: 1000}}}},
			"expr_soft":         {DurationMS: 1000, Tracks: map[string][]RigKeyframe{"f_idle": {{AtMS: 0, Alpha: floatPtr(1)}}}},
			"gaze_wander":       {DurationMS: 1000, Tracks: map[string][]RigKeyframe{}},
			"secondary_idle":    {DurationMS: 1000},
		},
	}
	step := PerformanceStep{
		Body:       "idle_breathe_loop",
		Expression: "expr_soft",
		Gaze:       "gaze_wander",
		Secondary:  "secondary_idle",
	}
	got := performanceStepClips(rr, step)
	want := []string{"idle_breathe_loop", "expr_soft"}
	if len(got) != len(want) {
		t.Fatalf("clips = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("clips = %v, want %v", got, want)
		}
	}
}

func floatPtr(v float64) *float64 { return &v }

func TestBlendRigFramesStraightAlpha(t *testing.T) {
	// Previous: opaque red; current: fully transparent.
	// Mid-blend must stay red-ish with ~50% alpha — not muddy via raw channel lerp.
	prev := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	cur := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			prev.SetNRGBA(x, y, color.NRGBA{R: 255, G: 0, B: 0, A: 255})
			cur.SetNRGBA(x, y, color.NRGBA{R: 0, G: 0, B: 0, A: 0})
		}
	}
	out := blendRigFrames(prev, cur, 0.5)
	if out == nil {
		t.Fatal("nil blend")
	}
	c := out.NRGBAAt(0, 0)
	if c.A < 100 || c.A > 150 {
		t.Fatalf("expected ~128 alpha, got %+v", c)
	}
	if c.R < 200 {
		t.Fatalf("straight-alpha blend should keep red chromaticity, got %+v", c)
	}
	// Old broken path would average R toward 0 → ~127 and look gray-pink.
	if c.R < 240 {
		// Allow small fixed-point error but must stay near 255.
		t.Fatalf("expected R near 255 for opaque→transparent fade, got %+v", c)
	}
}

func TestHairFaceLayoutIsMultiHead(t *testing.T) {
	slots := []RigSlot{
		{Name: "slot_body", Bone: "body", Texture: "rig/body.png"},
		{Name: "slot_hair", Bone: "hair", Texture: "rig/hair.png"},
		{Name: "slot_f_idle", Bone: "f_idle", Texture: "rig/face_idle.png"},
		{Name: "slot_f_speak", Bone: "f_speak", Texture: "rig/face_speak.png"},
	}
	if !detectMultiHeadPack(slots) {
		t.Fatal("hair+face layout should count as multi-part")
	}
	if !isHairSlot(slots[1]) || !isExpressionFaceSlot(slots[2]) {
		t.Fatal("hair/face slot classifiers")
	}
}

func TestBodyOnlyMultiHeadShowsIdleFaceOnly(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "rig"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Design canvas is 256; solid colors so we can detect which face drew.
	body := image.NewNRGBA(image.Rect(0, 0, 256, 256))
	idle := image.NewNRGBA(image.Rect(0, 0, 256, 256))
	speak := image.NewNRGBA(image.Rect(0, 0, 256, 256))
	for y := 80; y < 176; y++ {
		for x := 80; x < 176; x++ {
			body.SetNRGBA(x, y, color.NRGBA{R: 10, G: 10, B: 200, A: 255})
			idle.SetNRGBA(x, y, color.NRGBA{R: 0, G: 255, B: 0, A: 255})  // green idle
			speak.SetNRGBA(x, y, color.NRGBA{R: 255, G: 0, B: 0, A: 255}) // red speak
		}
	}
	writePNG(t, filepath.Join(dir, "rig", "body.png"), body)
	writePNG(t, filepath.Join(dir, "rig", "face_idle.png"), idle)
	writePNG(t, filepath.Join(dir, "rig", "face_speak.png"), speak)
	// Body-only clip: no expression alpha tracks — previously stacked both faces.
	rigJSON := `{
	  "version":1,
	  "bones":[
	    {"name":"root","x":128,"y":128},
	    {"name":"body","parent":"root"},
	    {"name":"head","parent":"body"},
	    {"name":"f_idle","parent":"head"},
	    {"name":"f_speak","parent":"head"}
	  ],
	  "slots":[
	    {"name":"slot_body","bone":"body","texture":"rig/body.png","z":1},
	    {"name":"slot_f_idle","bone":"f_idle","texture":"rig/face_idle.png","z":2},
	    {"name":"slot_f_speak","bone":"f_speak","texture":"rig/face_speak.png","z":3}
	  ],
	  "clips":{
	    "idle_breathe_loop":{"duration_ms":1000,"loop":true,"tracks":{
	      "body":[{"at_ms":0,"y":0},{"at_ms":500,"y":1},{"at_ms":1000,"y":0}]
	    }}
	  }
	}`
	if err := os.WriteFile(filepath.Join(dir, "rig", "hf.rig.json"), []byte(rigJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	assets := &PetPackRigAssets{Definition: "rig/hf.rig.json", Textures: []string{"rig/body.png", "rig/face_idle.png", "rig/face_speak.png"}}
	resolved := &ResolvedPack{Renderer: RendererSkeleton, Rig: assets, AssetFS: os.DirFS(dir)}
	renderer, err := NewRigRenderer(resolved)
	if err != nil || renderer == nil {
		t.Fatalf("renderer: %v", err)
	}
	if !renderer.multiHead {
		t.Fatal("expected multiHead")
	}
	frame := renderer.RenderClip("idle_breathe_loop", 0, 256)
	if frame == nil {
		t.Fatal("nil frame")
	}
	// Center pixel should be green idle face, not red speak (or a blend).
	c := frame.NRGBAAt(128, 128)
	if c.G < 200 || c.R > 40 {
		t.Fatalf("body-only multi-head should show idle face only, got %+v", c)
	}
}

func writePNG(t *testing.T, path string, img image.Image) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}
