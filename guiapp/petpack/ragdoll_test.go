package petpack

import (
	"image"
	"math"
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

func TestRagdollRearingAndTaskReminders(t *testing.T) {
	reg := NewRegistry(t.TempDir(), BundledFS())
	if err := reg.Scan(); err != nil {
		t.Fatal(err)
	}
	manifest, ok := reg.Get(DefaultPackID)
	if !ok || manifest == nil {
		t.Fatal("missing bundled ragdoll")
	}
	if !manifest.Capabilities.SupportsTaskDuty {
		t.Fatal("ragdoll does not declare task duty")
	}
	stages := manifest.Rearing.Stages
	if len(stages) != 3 {
		t.Fatalf("stages = %d", len(stages))
	}
	if stages[0].ID != "familiar" || stages[0].AfterMS != 7_200_000 || math.Abs(stages[0].Scale-1.04) > 0.001 || stages[0].Cue != "notice" || stages[0].Variant != "" || len(stages[0].Adornments) != 1 || stages[0].Adornments[0] != "mouse-charm" {
		t.Fatalf("familiar = %+v", stages[0])
	}
	if stages[1].ID != "grown" || stages[1].AfterMS != 86_400_000 || math.Abs(stages[1].Scale-1.1) > 0.001 || stages[1].Cue != "celebrate" || len(stages[1].Adornments) != 2 || stages[1].Adornments[0] != "mouse-charm" || stages[1].Adornments[1] != "slippers" {
		t.Fatalf("grown = %+v", stages[1])
	}
	if stages[2].ID != "settled" || stages[2].AfterMS != 259_200_000 || math.Abs(stages[2].Scale-1.18) > 0.001 || stages[2].Cue != "celebrate" || len(stages[2].Adornments) != 3 || stages[2].Adornments[2] != "mouse-pet" {
		t.Fatalf("settled = %+v", stages[2])
	}
	if slots := ActiveAdornmentSlots(manifest, 0); len(slots) != 0 {
		t.Fatalf("a new ragdoll wears %v", slots)
	}
	if slots := ActiveAdornmentSlots(manifest, 7_200_000); len(slots) != 1 || slots[0] != "mouse_charm" {
		t.Fatalf("familiar slots = %v", slots)
	}
	if slots := ActiveAdornmentSlots(manifest, 86_400_000); len(slots) != 3 || slots[0] != "mouse_charm" || slots[1] != "slipper_l" || slots[2] != "slipper_r" {
		t.Fatalf("grown slots = %v", slots)
	}
	if slots := ActiveAdornmentSlots(manifest, 259_200_000); len(slots) != 4 || slots[3] != "mouse_pet" {
		t.Fatalf("settled slots = %v", slots)
	}
	if _, found := ActiveRearingStage(stages, 0); found {
		t.Fatal("a new ragdoll already has a stage")
	}
	if stage, found := ActiveRearingStage(stages, 7_200_000); !found || stage.ID != "familiar" {
		t.Fatalf("2h stage = %+v found=%v", stage, found)
	}
	if stage, found := ActiveRearingStage(stages, 259_200_000); !found || stage.ID != "settled" {
		t.Fatalf("3d stage = %+v found=%v", stage, found)
	}

	resolved, err := reg.Resolve(DefaultPackID, VariantDefault)
	if err != nil {
		t.Fatal(err)
	}
	character, err := NewCharacterRenderer(resolved)
	if err != nil || character == nil {
		t.Fatal(err)
	}
	if character.RenderState(StateIdle, 0, 128) == nil {
		t.Fatal("idle frame")
	}
	checks := []struct {
		event string
		cue   string
		play  string
	}{
		{"task_started", "focus", "react_task_focus"},
		{"task_needs_you", "notice", "react_task_listen"},
		{"task_done", "celebrate", "react_task_celebrate"},
		{"task_failed", "concern", "react_task_concern"},
	}
	for _, check := range checks {
		cue, bound := character.EventCue(check.event)
		if !bound || cue != check.cue {
			t.Fatalf("%s cue = %q bound=%v", check.event, cue, bound)
		}
		if !character.TriggerEvent(check.event, 1000) {
			t.Fatalf("%s did not start", check.event)
		}
		frame := character.RenderState(StateThinking, 1100, 128)
		if frame == nil {
			t.Fatalf("%s produced no frame", check.event)
		}
	}
	rig, err := LoadRig(resolved)
	if err != nil {
		t.Fatal(err)
	}
	clip := rig.Clips["react_task_listen"]
	if clip.DurationMS != 920 || clip.Cue != "notice" || len(clip.Tracks["root"]) == 0 || len(clip.Tracks["arm_r"]) == 0 {
		t.Fatalf("listen clip = %+v", clip)
	}
	raw, err := readAsset(resolved, resolved.Character.Definition)
	if err != nil {
		t.Fatal(err)
	}
	performer, err := ParsePerformer(raw)
	if err != nil {
		t.Fatal(err)
	}
	director := NewPerformanceDirector(performer, rig, 1)
	step := director.SelectEvent("task_needs_you", 1000)
	if step.Body != "react_task_listen" || step.Interrupt != "soft" {
		t.Fatalf("needs-you step = %+v", step)
	}
	started := director.SelectEvent("task_started", 1000)
	if started.Body != "react_task_focus" || started.Interrupt != "soft" {
		t.Fatalf("started step = %+v", started)
	}
	done := director.SelectEvent("task_done", 1000)
	if done.Body != "react_task_celebrate" || done.Interrupt != "soft" {
		t.Fatalf("done step = %+v", done)
	}

	// The pose and the reminder arrive together. The gesture has to survive
	// the pose change, then the new state takes over when the clip ends.
	pose, err := NewCharacterRenderer(resolved)
	if err != nil || pose == nil {
		t.Fatal(err)
	}
	if pose.RenderState(StateIdle, 0, 128) == nil || !pose.TriggerEvent("task_needs_you", 40) {
		t.Fatal("needs-you did not start from idle")
	}
	if pose.RenderState(StateListening, 80, 128) == nil || pose.step.Body != "react_task_listen" {
		t.Fatalf("needs-you was replaced by the listening pose: %+v", pose.step)
	}
	if pose.RenderState(StateListening, 40+920, 128) == nil || pose.step.Body == "react_task_listen" {
		t.Fatalf("needs-you did not return to listening: %+v", pose.step)
	}
}

func TestRagdollAdornmentsStayHiddenUntilTheStage(t *testing.T) {
	reg := NewRegistry(t.TempDir(), BundledFS())
	if err := reg.Scan(); err != nil {
		t.Fatal(err)
	}
	resolved, err := reg.Resolve(DefaultPackID, VariantDefault)
	if err != nil {
		t.Fatal(err)
	}
	character, err := NewCharacterRenderer(resolved)
	if err != nil || character == nil {
		t.Fatal(err)
	}
	bare := opaquePixels(character.RenderState(StateIdle, 0, 256))
	if bare == 0 {
		t.Fatal("idle frame is empty")
	}
	character.SetVisibleAdornmentSlots([]string{"mouse_charm"})
	charm := opaquePixels(character.RenderState(StateIdle, 0, 256))
	if charm <= bare {
		t.Fatalf("charm did not add pixels: bare=%d charm=%d", bare, charm)
	}
	character.SetVisibleAdornmentSlots([]string{"mouse_charm", "slipper_l", "slipper_r", "mouse_pet"})
	all := opaquePixels(character.RenderState(StateIdle, 0, 256))
	if all <= charm {
		t.Fatalf("later props did not add pixels: charm=%d all=%d", charm, all)
	}
	character.SetVisibleAdornmentSlots(nil)
	if got := opaquePixels(character.RenderState(StateIdle, 0, 256)); got != bare {
		t.Fatalf("hiding props left %d pixels, bare=%d", got, bare)
	}
}

func opaquePixels(img *image.NRGBA) int {
	if img == nil {
		return 0
	}
	n := 0
	for i := 3; i < len(img.Pix); i += 4 {
		if img.Pix[i] >= 16 {
			n++
		}
	}
	return n
}

// TestRagdollAdornmentsSitOnHandFeetAndSide checks each prop against the
// resting pose. An opaque-count increase can be a single prop; the charm has
// to sit on the hand, the slippers on the feet, and the mouse beside her,
// inside the canvas.
func TestRagdollAdornmentsSitOnHandFeetAndSide(t *testing.T) {
	reg := NewRegistry(t.TempDir(), BundledFS())
	if err := reg.Scan(); err != nil {
		t.Fatal(err)
	}
	resolved, err := reg.Resolve(DefaultPackID, VariantDefault)
	if err != nil {
		t.Fatal(err)
	}
	rig, err := NewRigRenderer(resolved)
	if err != nil || rig == nil {
		t.Fatal(err)
	}
	bare := rig.RenderClip("idle_breathe_loop", 0, 256)
	charm := renderAdornedClip(rig, []string{"mouse_charm"})
	feet := renderAdornedClip(rig, []string{"slipper_l", "slipper_r"})
	mouse := renderAdornedClip(rig, []string{"mouse_pet"})

	cn, cminX, cminY, cmaxX, cmaxY := changedPixelBox(bare, charm)
	if cn < 120 || cminX < 60 || cmaxX > 150 || cminY < 150 || cmaxY > 240 {
		t.Fatalf("charm box n=%d x=%d..%d y=%d..%d, want the hand", cn, cminX, cmaxX, cminY, cmaxY)
	}
	fn, fminX, fminY, fmaxX, fmaxY := changedPixelBox(bare, feet)
	if fn < 80 || fminY < 200 || fmaxY > 255 || fmaxX-fminX < 24 {
		t.Fatalf("slipper box n=%d x=%d..%d y=%d..%d, want both feet", fn, fminX, fmaxX, fminY, fmaxY)
	}
	mn, mminX, mminY, mmaxX, mmaxY := changedPixelBox(bare, mouse)
	if mn < 200 || mmaxX > 140 || mminY < 160 || mmaxY > 255 || mmaxY-mminY < 20 {
		t.Fatalf("mouse box n=%d x=%d..%d y=%d..%d, want beside the feet", mn, mminX, mmaxX, mminY, mmaxY)
	}
}

func renderAdornedClip(rig *RigRenderer, slots []string) *image.NRGBA {
	rig.SetVisibleAdornmentSlots(slots)
	return rig.RenderClip("idle_breathe_loop", 0, 256)
}

func changedPixelBox(before, after *image.NRGBA) (n, minX, minY, maxX, maxY int) {
	minX, minY = 1<<30, 1<<30
	maxX, maxY = -1, -1
	if before == nil || after == nil || !before.Bounds().Eq(after.Bounds()) {
		return 0, 0, 0, 0, 0
	}
	b := after.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			i := after.PixOffset(x, y)
			if before.Pix[i] == after.Pix[i] && before.Pix[i+1] == after.Pix[i+1] &&
				before.Pix[i+2] == after.Pix[i+2] && before.Pix[i+3] == after.Pix[i+3] {
				continue
			}
			n++
			if x < minX {
				minX = x
			}
			if y < minY {
				minY = y
			}
			if x > maxX {
				maxX = x
			}
			if y > maxY {
				maxY = y
			}
		}
	}
	if n == 0 {
		return 0, 0, 0, 0, 0
	}
	return n, minX, minY, maxX, maxY
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
	if think := rig.Clips["think_loop"]; think.TravelX != 0 || think.TravelY != 0 || think.Cue != "" || think.SoundLoop || len(think.Tracks["arm_l"]) == 0 {
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

func TestThinkingTravelDoesNotMoveTheWindow(t *testing.T) {
	reg := NewRegistry(t.TempDir(), BundledFS())
	if err := reg.Scan(); err != nil {
		t.Fatal(err)
	}
	resolved, err := reg.Resolve(DefaultPackID, VariantDefault)
	if err != nil {
		t.Fatal(err)
	}
	renderer, err := NewCharacterRenderer(resolved)
	if err != nil || renderer == nil {
		t.Fatal(err)
	}
	if renderer.RenderState(StateThinking, 0, 64) == nil {
		t.Fatal("think enter")
	}
	if renderer.RenderState(StateThinking, 2500, 64) == nil || renderer.step.Body != "think_loop" {
		t.Fatalf("thinking body = %s", renderer.step.Body)
	}
	clip := renderer.rig.Clip(renderer.step.Body)
	if clip == nil {
		t.Fatal("missing think clip")
	}
	clip.TravelX = 96
	if vx, vy := renderer.DesktopTravelDesignPerMS(2500); vx != 0 || vy != 0 {
		t.Fatalf("thinking velocity = %v,%v", vx, vy)
	}
	if renderer.RenderState(StateIdle, 2600, 64) == nil {
		t.Fatal("idle")
	}
	idleClip := renderer.rig.Clip(renderer.step.Body)
	if idleClip == nil {
		t.Fatal("missing idle clip")
	}
	idleClip.TravelX = 80
	if idleClip.DurationMS < 1 {
		idleClip.DurationMS = 1000
	}
	if vx, _ := renderer.DesktopTravelDesignPerMS(2600); vx == 0 {
		t.Fatalf("idle body %s did not roam", renderer.step.Body)
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
