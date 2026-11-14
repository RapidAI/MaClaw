package petpack

import (
	"image"
	"testing"
)

func TestRagdollIsTheOnlyBundledCharacter(t *testing.T) {
	reg := NewRegistry(t.TempDir(), BundledFS())
	if err := reg.Scan(); err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.Get(LegacyClawMatePackID); ok {
		t.Fatal("retired clawmate must not be scanned")
	}
	for _, info := range reg.List() {
		if info.ID == LegacyClawMatePackID {
			t.Fatal("retired clawmate must not be listed")
		}
	}
	manifest, ok := reg.Get(DefaultPackID)
	if !ok || manifest == nil {
		t.Fatal("missing bundled ragdoll")
	}
	if manifest.Status != StatusOK || manifest.Renderer != RendererCharacter || !manifest.Capabilities.PetPerformanceV3 {
		t.Fatalf("ragdoll status=%s renderer=%s err=%s", manifest.Status, manifest.Renderer, manifest.Error)
	}
	if got := SanitizeSkinID(LegacyClawMatePackID, true, reg.Allowlist()); got != DefaultPackID {
		t.Fatalf("saved clawmate id = %q, want ragdoll", got)
	}
}

func TestRagdollLargeActionsAndCues(t *testing.T) {
	reg := NewRegistry(t.TempDir(), BundledFS())
	if err := reg.Scan(); err != nil {
		t.Fatal(err)
	}
	resolved, err := reg.Resolve(DefaultPackID, VariantDefault)
	if err != nil {
		t.Fatal(err)
	}
	rig, err := LoadRig(resolved)
	if err != nil {
		t.Fatal(err)
	}
	walk := rig.Clips["walk_loop"]
	if walk.TravelX < 80 || walk.Cue != "step" || !walk.SoundLoop {
		t.Fatalf("walk_loop = %+v", walk)
	}
	run := rig.Clips["run_loop"]
	if run.TravelX <= walk.TravelX || run.Cue != "run" || !run.SoundLoop {
		t.Fatalf("run_loop = %+v", run)
	}
	if rig.Clips["jump_in"].Cue != "jump" || rig.Clips["jump_out"].Cue != "land" {
		t.Fatal("jump cues")
	}
	if rig.Clips["bend_in"].Cue != "bend" {
		t.Fatal("bend cue")
	}
	if rig.Clips["speak_loop"].Cue != "sing" || !rig.Clips["speak_loop"].SoundLoop || rig.Clips["sing_loop"].Cue != "sing" {
		t.Fatal("sing cue")
	}
	if think := rig.Clips["think_loop"]; think.TravelX < 40 || think.Cue != "step" || !think.SoundLoop {
		t.Fatalf("think_loop = %+v", think)
	}
	if breathe := rig.Clips["idle_breathe_loop"]; breathe.TravelX < 40 || breathe.Cue != "" {
		t.Fatalf("idle_breathe_loop = %+v", breathe)
	}
	if listen := rig.Clips["listen_loop"]; listen.Cue != "" || listen.TravelX != 0 {
		t.Fatal("listening must stay still and silent")
	}

	renderer, err := NewRigRenderer(resolved)
	if err != nil || renderer == nil {
		t.Fatal(err)
	}
	idle := renderer.RenderClips([]string{"idle_breathe_loop", "expr_calm", "gaze_center", "secondary_idle"}, 0, 256)
	jump := renderer.RenderClips([]string{"jump_loop", "expr_pleased", "gaze_center", "secondary_idle"}, 200, 256)
	bend := renderer.RenderClips([]string{"bend_loop", "expr_calm", "gaze_center", "secondary_idle"}, 400, 256)
	if idle == nil || jump == nil || bend == nil {
		t.Fatal("missing pose frame")
	}
	if top := opaqueTop(jump); top > opaqueTop(idle)-12 {
		t.Fatalf("jump top %d is not clearly above idle top %d", top, opaqueTop(idle))
	}
	if bendX, idleX := centroidX(bend), centroidX(idle); bendX < idleX+6 {
		t.Fatalf("bend centroid %0.1f, idle %0.1f", bendX, idleX)
	}

	character, err := NewCharacterRenderer(resolved)
	if err != nil || character == nil {
		t.Fatal(err)
	}
	if character.RenderState(StateIdle, 0, 128) == nil {
		t.Fatal("idle frame")
	}
	if !character.TriggerEvent("click", 40) {
		t.Fatal("click")
	}
	cue, _ := character.TakeActionSound(40)
	if cue != "greet" && cue != "notice" {
		t.Fatalf("click cue = %q", cue)
	}
	if again, tones := character.TakeActionSound(40); again != "" || len(tones) > 0 {
		t.Fatalf("click cue repeated: %q %v", again, tones)
	}

	speaker, err := NewCharacterRenderer(resolved)
	if err != nil || speaker == nil {
		t.Fatal(err)
	}
	if speaker.RenderState(StateSpeaking, 0, 128) == nil {
		t.Fatal("speak frame")
	}
	if speaker.RenderState(StateSpeaking, 450, 128) == nil {
		t.Fatal("sing frame")
	}
	if cue, _ := speaker.TakeActionSound(450); cue != "sing" {
		t.Fatalf("speaking cue = %q, want sing", cue)
	}
}

func TestActionSoundCatalogAndTravelClamp(t *testing.T) {
	jump := ResolveActionSound("jump", nil)
	step := ResolveActionSound("step", nil)
	sing := ResolveActionSound("sing", nil)
	if len(jump) == 0 || len(step) == 0 || len(sing) == 0 {
		t.Fatal("missing cues")
	}
	if jump[0] == step[0] || sing[0] == step[0] || sing[len(sing)-1].Hz == jump[len(jump)-1].Hz {
		t.Fatalf("cues are not distinct: jump=%v step=%v sing=%v", jump, step, sing)
	}
	if !IsActionCue("sing") || IsActionCue("meow") {
		t.Fatal("cue lookup")
	}
	next, hit := StepDesktopTravel(10, 50, 0, 40)
	if next != 40 || !hit {
		t.Fatalf("clamp = %d hit=%v", next, hit)
	}
	next, hit = StepDesktopTravel(10, -30, 0, 40)
	if next != 0 || !hit {
		t.Fatalf("left clamp = %d hit=%v", next, hit)
	}
	next, hit = StepDesktopTravel(10, 4, 0, 40)
	if next != 14 || hit {
		t.Fatalf("free step = %d hit=%v", next, hit)
	}

	rig := &Rig{
		Version: 1,
		Bones:   []RigBone{{Name: "root"}},
		Slots:   []RigSlot{{Name: "body", Bone: "root", Texture: "rig/body.png"}},
		Clips: map[string]RigClip{
			"bad_travel":   {DurationMS: 400, TravelX: 999},
			"bad_travel_y": {DurationMS: 400, TravelY: 999},
			"bad_cue":      {DurationMS: 400, Cue: "meow"},
			"bad_loop":     {DurationMS: 400, SoundLoop: true},
		},
	}
	assets := &PetPackRigAssets{Definition: "rig/pet-rig.json", Textures: []string{"rig/body.png"}}
	for _, name := range []string{"bad_travel", "bad_travel_y", "bad_cue", "bad_loop"} {
		one := *rig
		one.Clips = map[string]RigClip{name: rig.Clips[name]}
		if err := ValidateRig(&one, assets); err == nil {
			t.Fatalf("expected %s to be rejected", name)
		}
	}
}

func opaqueTop(img *image.NRGBA) int {
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if img.Pix[img.PixOffset(x, y)+3] > 24 {
				return y
			}
		}
	}
	return b.Max.Y
}

func centroidX(img *image.NRGBA) float64 {
	b := img.Bounds()
	var sum float64
	var n int
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if img.Pix[img.PixOffset(x, y)+3] > 24 {
				sum += float64(x)
				n++
			}
		}
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n)
}
