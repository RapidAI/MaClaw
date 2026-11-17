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

// TestRagdollDragBraceStaysLifted checks the picked-up pose is visible and
// that letting go starts from that same height. Hover leans and lifts a hand
// without becoming the full click wave.
func TestRagdollDragBraceStaysLifted(t *testing.T) {
	reg := NewRegistry(t.TempDir(), BundledFS())
	if err := reg.Scan(); err != nil {
		t.Fatal(err)
	}
	resolved, err := reg.Resolve(DefaultPackID, VariantDefault)
	if err != nil {
		t.Fatal(err)
	}
	rig, err := LoadRig(resolved)
	if err != nil || rig == nil {
		t.Fatal(err)
	}
	brace := rig.Clips["react_drag_brace"]
	release := rig.Clips["react_drag_release"]
	if brace.DurationMS < 5000 || brace.Cue != "brace" || release.Cue != "release" {
		t.Fatalf("brace=%+v release cue %q", brace.DurationMS, release.Cue)
	}
	root := brace.Tracks["root"]
	if len(root) < 3 || root[1].Y > -24 || root[len(root)-1].Y != root[1].Y {
		t.Fatalf("brace root = %+v", root)
	}
	if release.Tracks["root"][0].Y != root[len(root)-1].Y {
		t.Fatalf("release starts at %v, hold is %v", release.Tracks["root"][0].Y, root[len(root)-1].Y)
	}
	if maxAbsRigRotate(brace.Tracks["arm_l"]) < 60 || maxAbsRigRotate(brace.Tracks["arm_r"]) < 60 {
		t.Fatal("brace arms stay close to the body")
	}
	if len(brace.Tracks["leg_l"]) == 0 || len(brace.Tracks["leg_r"]) == 0 {
		t.Fatal("brace does not tuck the legs")
	}
	head := brace.Tracks["head"]
	braid := brace.Tracks["braid_l"]
	if maxAbsRigRotate(head) < 6 || len(braid) == 0 {
		t.Fatal("brace head tilt did not move the braids")
	}
	for _, key := range head {
		if math.Abs(key.Rotate) < 6 {
			continue
		}
		local, ok := rigRotateAt(braid, key.AtMS)
		if !ok || key.Rotate*local >= 0 {
			t.Fatalf("brace braid %v does not oppose head %v", local, key.Rotate)
		}
	}

	renderer, err := NewRigRenderer(resolved)
	if err != nil || renderer == nil {
		t.Fatal(err)
	}
	idle := renderer.RenderClips([]string{"idle_breathe_loop"}, 0, 256)
	lifted := renderer.RenderClips([]string{"react_drag_brace"}, 400, 256)
	held := renderer.RenderClips([]string{"react_drag_brace"}, 4000, 256)
	letGo := renderer.RenderClips([]string{"react_drag_release"}, 0, 256)
	if idle == nil || lifted == nil || held == nil || letGo == nil {
		t.Fatal("missing drag pose")
	}
	if top, idleTop := opaqueTop(lifted), opaqueTop(idle); top > idleTop-16 {
		t.Fatalf("brace top %d is not clearly above idle %d", top, idleTop)
	}
	if d := opaqueTop(held) - opaqueTop(lifted); d < -2 || d > 2 {
		t.Fatalf("held top %d drifted from lifted %d", opaqueTop(held), opaqueTop(lifted))
	}
	if opaqueTop(letGo) > opaqueTop(idle)-16 {
		t.Fatalf("release starts at top %d, idle %d", opaqueTop(letGo), opaqueTop(idle))
	}

	peek := rig.Clips["react_hover_peek"]
	hoverRoot := 0.0
	for _, key := range peek.Tracks["root"] {
		if key.Y < hoverRoot {
			hoverRoot = key.Y
		}
	}
	arm := maxAbsRigRotate(peek.Tracks["arm_r"])
	if maxAbsRigRotate(peek.Tracks["head"]) < 18 || hoverRoot >= -4 || hoverRoot <= -14 || arm < 36 || arm >= 100 {
		t.Fatalf("hover peek head=%v root=%v arm=%v", maxAbsRigRotate(peek.Tracks["head"]), hoverRoot, arm)
	}
	if len(peek.Tracks["braid_l"]) == 0 {
		t.Fatal("hover tilt did not move the braids")
	}
}

// TestRagdollBraidsSwingSofterThanTheHead checks the authored follow-through.
// The tip of each plait swings farther and later than the upper bone, the two
// sides do not copy each other, and a deep head tilt does not take the braid
// with it at the same angle.
func TestRagdollBraidsSwingSofterThanTheHead(t *testing.T) {
	reg := NewRegistry(t.TempDir(), BundledFS())
	if err := reg.Scan(); err != nil {
		t.Fatal(err)
	}
	resolved, err := reg.Resolve(DefaultPackID, VariantDefault)
	if err != nil {
		t.Fatal(err)
	}
	rig, err := LoadRig(resolved)
	if err != nil || rig == nil {
		t.Fatal(err)
	}
	idle := rig.Clips["secondary_idle"]
	upper := idle.Tracks["braid_l"]
	tip := idle.Tracks["braid_l_tip"]
	right := idle.Tracks["braid_r"]
	if len(upper) < 4 || len(tip) < 4 || len(right) < 4 {
		t.Fatalf("secondary_idle braid keys upper=%d tip=%d right=%d", len(upper), len(tip), len(right))
	}
	if upper[0].AtMS != 0 || upper[len(upper)-1].AtMS != idle.DurationMS || upper[0].Rotate != upper[len(upper)-1].Rotate {
		t.Fatalf("braid loop does not close: %+v", upper)
	}
	upperAmp := maxAbsRigRotate(upper)
	tipAmp := maxAbsRigRotate(tip)
	if tipAmp <= upperAmp {
		t.Fatalf("tip amplitude %0.2f is not larger than upper %0.2f", tipAmp, upperAmp)
	}
	if tipAt, upperAt := maxRigRotateAt(tip), maxRigRotateAt(upper); tipAt <= upperAt {
		t.Fatalf("tip peaks at %dms, upper at %dms", tipAt, upperAt)
	}
	differ := false
	n := len(upper)
	if len(right) < n {
		n = len(right)
	}
	for i := 1; i < n-1; i++ {
		if math.Abs(upper[i].Rotate-right[i].Rotate) > 1 {
			differ = true
			break
		}
	}
	if !differ {
		t.Fatal("left and right braids swing together")
	}

	bend := rig.Clips["bend_loop"]
	head := bend.Tracks["head"]
	braid := bend.Tracks["braid_l"]
	if len(head) == 0 || len(braid) == 0 {
		t.Fatal("bend_loop is missing the head or the braid")
	}
	checked := false
	for _, key := range head {
		if math.Abs(key.Rotate) < 6 {
			continue
		}
		local, ok := rigRotateAt(braid, key.AtMS)
		if !ok {
			t.Fatalf("bend_loop braid has no key at %dms", key.AtMS)
		}
		if key.Rotate*local >= 0 {
			t.Fatalf("braid %0.2f does not oppose head %0.2f", local, key.Rotate)
		}
		world := key.Rotate + local
		if math.Abs(world) >= math.Abs(key.Rotate)*0.7 {
			t.Fatalf("braid world tilt %0.2f is not softer than head %0.2f", world, key.Rotate)
		}
		checked = true
	}
	if !checked {
		t.Fatal("bend_loop head never tilts")
	}
}

// TestRagdollIdlePoolCoversTheNamedFloor checks the official idle pool
// includes the named kinds from the pack spec, and that the five added
// poses stay in place, lead with the body, and do not use the notice cue.
func TestRagdollMissClipsReturnAndGrow(t *testing.T) {
	reg := NewRegistry(t.TempDir(), BundledFS())
	if err := reg.Scan(); err != nil {
		t.Fatal(err)
	}
	resolved, err := reg.Resolve(DefaultPackID, VariantDefault)
	if err != nil {
		t.Fatal(err)
	}
	rig, err := LoadRig(resolved)
	if err != nil || rig == nil {
		t.Fatal(err)
	}
	rest := rig.Clips["react_long_idle_rest"]
	sulk := rig.Clips["react_long_idle_sulk"]
	pout := rig.Clips["react_long_idle_pout"]
	if rest.DurationMS < 1 || rest.TravelX != 0 || rest.Cue != "rest" {
		t.Fatalf("rest = %+v", rest)
	}
	for _, clip := range []RigClip{sulk, pout} {
		if clip.DurationMS < 1 || clip.TravelX != 0 || clip.Cue != "" {
			t.Fatalf("miss clip = %+v", clip)
		}
		for _, bone := range []string{"root", "head", "torso"} {
			frames := clip.Tracks[bone]
			if len(frames) < 2 {
				t.Fatalf("%s missing %s", clip.Cue, bone)
			}
			last := frames[len(frames)-1]
			if last.AtMS != clip.DurationMS || last.X != 0 || last.Y != 0 || last.Rotate != 0 {
				t.Fatalf("clip does not return to neutral on %s: %+v", bone, last)
			}
		}
	}
	if maxAbsRigRotate(sulk.Tracks["head"]) < 12 || maxAbsRigRotate(pout.Tracks["head"]) <= maxAbsRigRotate(sulk.Tracks["head"]) {
		t.Fatalf("sulk head %v pout head %v", maxAbsRigRotate(sulk.Tracks["head"]), maxAbsRigRotate(pout.Tracks["head"]))
	}
	if len(sulk.Tracks["arm_l"]) < 2 || len(sulk.Tracks["arm_r"]) < 2 || len(sulk.Tracks["tail"]) < 2 || len(pout.Tracks["arm_r"]) < 2 || len(pout.Tracks["tail"]) < 2 {
		t.Fatal("sulk and pout do not move both arms and the tail")
	}
	for _, clip := range []RigClip{sulk, pout, rest} {
		for _, bone := range []string{"arm_l", "arm_r", "tail"} {
			frames := clip.Tracks[bone]
			if len(frames) == 0 {
				continue
			}
			last := frames[len(frames)-1]
			if last.AtMS != clip.DurationMS || last.X != 0 || last.Y != 0 || last.Rotate != 0 {
				t.Fatalf("%s does not return %s: %+v", clip.Cue, bone, last)
			}
		}
	}
	for _, name := range []string{"expr_wait", "expr_sulk", "expr_pout"} {
		face := rig.Clips[name]
		mouth := face.Tracks["mouth"]
		if !face.Loop || len(mouth) < 2 {
			t.Fatalf("%s is not a held mouth: %+v", name, face)
		}
	}
	if mouthScale(rig.Clips["expr_wait"]) <= 0 || mouthScale(rig.Clips["expr_wait"]) >= 1 {
		t.Fatalf("wait mouth %v", mouthScale(rig.Clips["expr_wait"]))
	}
	if mouthScale(rig.Clips["expr_sulk"]) >= 0 || mouthScale(rig.Clips["expr_pout"]) >= mouthScale(rig.Clips["expr_sulk"]) {
		t.Fatalf("sulk mouth %v pout mouth %v", mouthScale(rig.Clips["expr_sulk"]), mouthScale(rig.Clips["expr_pout"]))
	}
	if len(rig.Clips["secondary_idle"].Tracks["mouse"]) < 3 {
		t.Fatal("settled mouse does not breathe on the idle secondary")
	}
	renderer, err := NewRigRenderer(resolved)
	if err != nil || renderer == nil {
		t.Fatal(err)
	}
	for _, sample := range []struct {
		name string
		at   int
	}{
		{"react_long_idle_sulk", 860},
		{"react_long_idle_pout", 1040},
	} {
		img := renderer.RenderClips([]string{sample.name, "expr_calm", "gaze_wander"}, sample.at, 256)
		minX, minY, maxX, maxY, n := opaqueBounds(img)
		if n == 0 || minX < 0 || minY < 0 || maxX >= 256 || maxY >= 256 {
			t.Fatalf("%s@%d bounds %d,%d-%d,%d n=%d", sample.name, sample.at, minX, minY, maxX, maxY, n)
		}
	}
}

func TestRagdollCompanionFeelHoldsTheFace(t *testing.T) {
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
	calm := character.RenderState(StateIdle, 1000, 256)
	if !character.SetCompanionFeel("expr_pout", "gaze_center", "tired") {
		t.Fatal("pout face did not apply")
	}
	if character.SetCompanionFeel("expr_pout", "gaze_center", "tired") {
		t.Fatal("the same face reported a change")
	}
	pout := character.RenderState(StateIdle, 1000, 256)
	if n, _, _, _, _ := changedPixelBox(calm, pout); n < 20 {
		t.Fatalf("pout face changed %d pixels", n)
	}
	if !character.TriggerClip("react_long_idle_pout", 5000) {
		t.Fatal("pout clip did not play")
	}
	held := character.RenderState(StateIdle, 5000+1600, 256)
	if !character.SetCompanionFeel("", "", "") {
		t.Fatal("clearing the face did not apply")
	}
	cleared := character.RenderState(StateIdle, 5000+1600, 256)
	if n, _, _, _, _ := changedPixelBox(held, cleared); n < 20 {
		t.Fatalf("clearing the face changed %d pixels", n)
	}
	if character.SetCompanionFeel("expr_missing", "gaze_center", "tired") != true {
		t.Fatal("a known gaze and mood should still hold when the expression clip is missing")
	}
	if character.heldExpression != "" || character.heldMood != "tired" {
		t.Fatalf("missing expression stuck as %q mood %q", character.heldExpression, character.heldMood)
	}
}

func TestRagdollIdlePoolCoversTheNamedFloor(t *testing.T) {
	reg := NewRegistry(t.TempDir(), BundledFS())
	if err := reg.Scan(); err != nil {
		t.Fatal(err)
	}
	resolved, err := reg.Resolve(DefaultPackID, VariantDefault)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := readAsset(resolved, resolved.Character.Definition)
	if err != nil {
		t.Fatal(err)
	}
	performer, err := ParsePerformer(raw)
	if err != nil {
		t.Fatal(err)
	}
	rig, err := LoadRig(resolved)
	if err != nil || rig == nil {
		t.Fatal(err)
	}
	pool := map[string]bool{}
	for _, name := range performer.States["idle"].BehaviorPool {
		pool[name] = true
	}
	for _, name := range []string{
		"idle_breathe", "idle_blink", "idle_look_left", "idle_look_right",
		"idle_shift", "idle_drift", "idle_stretch", "idle_groom",
		"idle_rest", "idle_attend", "idle_notice",
	} {
		if !pool[name] {
			t.Fatalf("idle pool missing %s", name)
		}
		behavior := performer.Behaviors[name]
		if len(behavior.Moods) > 0 && !containsString(behavior.Moods, "calm") {
			t.Fatalf("%s has no calm mood: %v", name, behavior.Moods)
		}
	}
	for _, name := range []string{"idle_walk", "idle_run", "idle_jump", "idle_sing", "idle_bend", "idle_attend", "idle_notice"} {
		if containsString(performer.Behaviors[name].Moods, "tired") {
			t.Fatalf("%s still plays while she is waiting", name)
		}
	}
	tired := 0
	for _, name := range performer.States["idle"].BehaviorPool {
		if containsString(performer.Behaviors[name].Moods, "tired") {
			tired++
		}
	}
	if tired < 4 {
		t.Fatalf("waiting idle has %d behaviors", tired)
	}
	for _, reaction := range performer.Reactions {
		switch reaction.Play {
		case "react_long_idle_sulk", "react_long_idle_pout", "expr_wait", "expr_sulk", "expr_pout":
			t.Fatalf("complaint face is in the reaction pool: %+v", reaction)
		}
	}
	director := NewPerformanceDirector(performer, rig, 11)
	director.SetPinnedMood("tired")
	for i := 0; i < 8; i++ {
		step := director.SelectState(StateIdle, int64(1000+i*30000))
		if step.isZero() {
			t.Fatal("waiting idle stalled")
		}
		name := director.lastBehaviors[len(director.lastBehaviors)-1]
		if !containsString(performer.Behaviors[name].Moods, "tired") {
			t.Fatalf("waiting idle played %s", name)
		}
	}
	for _, name := range []string{"idle_shift", "idle_drift", "idle_groom", "idle_attend", "idle_notice"} {
		behavior := performer.Behaviors[name]
		for _, clipName := range []string{behavior.Enter, behavior.Loop, behavior.Exit} {
			clip := rig.Clips[clipName]
			if clip.DurationMS < 1 || clip.TravelX != 0 || clip.Cue != "" {
				t.Fatalf("%s = %+v", clipName, clip)
			}
			for bone, frames := range clip.Tracks {
				if len(frames) == 0 || frames[len(frames)-1].AtMS != clip.DurationMS {
					t.Fatalf("%s %s does not end on the duration", clipName, bone)
				}
			}
		}
		loop := rig.Clips[behavior.Loop]
		for _, bone := range []string{"torso", "head", "root"} {
			frames := loop.Tracks[bone]
			if len(frames) < 2 {
				continue
			}
			first, last := frames[0], frames[len(frames)-1]
			if first.Rotate != last.Rotate || first.X != last.X || first.Y != last.Y {
				t.Fatalf("%s loop seam on %s", behavior.Loop, bone)
			}
		}
	}
	groom := rig.Clips["idle_groom_loop"]
	if maxAbsRigRotate(groom.Tracks["head"]) < 6 || len(groom.Tracks["braid_l"]) == 0 {
		t.Fatal("groom does not swing the braids")
	}
	for _, key := range groom.Tracks["head"] {
		if math.Abs(key.Rotate) < 6 {
			continue
		}
		local, ok := rigRotateAt(groom.Tracks["braid_l"], key.AtMS)
		if !ok || key.Rotate*local >= 0 {
			t.Fatalf("groom braid does not oppose head %+v local=%v ok=%v", key, local, ok)
		}
	}
	if !bodyLeads(rig.Clips["idle_notice_in"].Tracks["torso"], rig.Clips["idle_notice_in"].Tracks["head"], 4) {
		t.Fatal("environment response turns the head before the torso")
	}
	if !bodyLeads(rig.Clips["idle_shift_in"].Tracks["torso"], rig.Clips["idle_shift_in"].Tracks["head"], 4) {
		t.Fatal("weight shift moves the head before the torso")
	}
	if !bodyLeads(rig.Clips["idle_groom_in"].Tracks["arm_l"], rig.Clips["idle_groom_in"].Tracks["head"], 6) {
		t.Fatal("groom tilts the head before the arm rises")
	}
	if !oppositePrepRotate(rig.Clips["idle_shift_in"].Tracks["torso"]) || !oppositePrepRotate(rig.Clips["idle_notice_in"].Tracks["torso"]) || !oppositePrepRotate(rig.Clips["idle_groom_in"].Tracks["arm_l"]) {
		t.Fatal("shift, notice, or groom is missing the opposite preparation")
	}
	drift := rig.Clips["idle_drift_in"]
	settledAt := firstRootYAt(drift.Tracks["root"], 4)
	headAt := firstAbsRotateAt(drift.Tracks["head"], 6)
	prepAt := firstRootYBelow(drift.Tracks["root"], 0)
	if prepAt < 100 || prepAt > 300 || settledAt < 0 || headAt <= settledAt {
		t.Fatalf("drift prep=%d settled=%d head=%d", prepAt, settledAt, headAt)
	}
	attendIn := rig.Clips["idle_attend_in"]
	attendRoot := attendIn.Tracks["root"]
	if len(attendRoot) < 2 || attendRoot[0].Y <= attendRoot[len(attendRoot)-1].Y {
		t.Fatal("attend does not rise out of the slump")
	}
	attendHead := rig.Clips["idle_attend_out"].Tracks["head"]
	if len(attendHead) == 0 || attendHead[len(attendHead)-1].Rotate != 0 || attendHead[len(attendHead)-1].Y != 0 {
		t.Fatal("attend does not return forward")
	}
	for _, name := range []string{"idle_shift", "idle_drift", "idle_groom", "idle_attend", "idle_notice"} {
		behavior := performer.Behaviors[name]
		enter, loop, exit := rig.Clips[behavior.Enter], rig.Clips[behavior.Loop], rig.Clips[behavior.Exit]
		if !clipMeets(enter, loop) || !clipMeets(loop, exit) {
			t.Fatalf("%s enter/loop/exit poses do not meet", name)
		}
	}
	renderer, err := NewRigRenderer(resolved)
	if err != nil || renderer == nil {
		t.Fatal(err)
	}
	for _, sample := range []struct {
		name string
		at   int
	}{
		{"idle_shift_in", 160},
		{"idle_shift_loop", 1400},
		{"idle_drift_in", 140},
		{"idle_drift_loop", 1200},
		{"idle_groom_in", 640},
		{"idle_groom_loop", 800},
		{"idle_attend_loop", 1000},
		{"idle_notice_loop", 1100},
	} {
		frame := renderer.RenderClips([]string{sample.name, "expr_calm", "gaze_wander", "secondary_idle"}, sample.at, 256)
		minX, minY, maxX, maxY, n := opaqueBounds(frame)
		if n == 0 || minX < 0 || minY < 0 || maxX >= 256 || maxY >= 256 {
			t.Fatalf("%s@%d left the canvas: %d,%d-%d,%d pixels=%d", sample.name, sample.at, minX, minY, maxX, maxY, n)
		}
	}
}

// clipMeets reports that the pose at the end of left is the pose at the start
// of right. Enter joins the loop with a hard cut, so those frames have to match.
func clipMeets(left, right RigClip) bool {
	if len(left.Tracks) == 0 || len(right.Tracks) == 0 {
		return false
	}
	for bone, frames := range left.Tracks {
		next := right.Tracks[bone]
		if len(frames) == 0 || len(next) == 0 || !sameRigPose(frames[len(frames)-1], next[0]) {
			return false
		}
	}
	for bone, frames := range right.Tracks {
		prev := left.Tracks[bone]
		if len(frames) == 0 || len(prev) == 0 || !sameRigPose(prev[len(prev)-1], frames[0]) {
			return false
		}
	}
	return true
}

func sameRigPose(a, b RigKeyframe) bool {
	const tol = 0.05
	return math.Abs(a.X-b.X) < tol && math.Abs(a.Y-b.Y) < tol && math.Abs(a.Rotate-b.Rotate) < tol && math.Abs(a.ScaleX-b.ScaleX) < tol && math.Abs(a.ScaleY-b.ScaleY) < tol
}

// oppositePrepRotate reports a 100–300ms move whose sign is opposite the final pose.
func oppositePrepRotate(frames []RigKeyframe) bool {
	if len(frames) < 3 {
		return false
	}
	final := frames[len(frames)-1].Rotate
	if final == 0 {
		return false
	}
	for _, frame := range frames[1 : len(frames)-1] {
		if frame.AtMS < 100 || frame.AtMS > 300 {
			continue
		}
		if frame.Rotate*final < 0 {
			return true
		}
	}
	return false
}

func firstRootYAt(frames []RigKeyframe, min float64) int {
	for _, frame := range frames {
		if frame.Y >= min {
			return frame.AtMS
		}
	}
	return -1
}

func firstRootYBelow(frames []RigKeyframe, max float64) int {
	for _, frame := range frames {
		if frame.Y < max {
			return frame.AtMS
		}
	}
	return -1
}

func opaqueBounds(img *image.NRGBA) (minX, minY, maxX, maxY, n int) {
	if img == nil {
		return 0, 0, 0, 0, 0
	}
	b := img.Bounds()
	minX, minY = b.Max.X, b.Max.Y
	maxX, maxY = b.Min.X, b.Min.Y
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if img.Pix[img.PixOffset(x, y)+3] <= 24 {
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
	return minX, minY, maxX, maxY, n
}

func bodyLeads(body, head []RigKeyframe, min float64) bool {
	bodyAt := firstAbsRotateAt(body, min)
	headAt := firstAbsRotateAt(head, min)
	return bodyAt >= 0 && headAt > bodyAt
}

func firstAbsRotateAt(frames []RigKeyframe, min float64) int {
	for _, frame := range frames {
		if math.Abs(frame.Rotate) >= min {
			return frame.AtMS
		}
	}
	return -1
}

func mouthScale(clip RigClip) float64 {
	min := 1.0
	for _, key := range clip.Tracks["mouth"] {
		if key.ScaleY < min {
			min = key.ScaleY
		}
	}
	return min
}

func maxAbsRigRotate(frames []RigKeyframe) float64 {
	max := 0.0
	for _, frame := range frames {
		if a := math.Abs(frame.Rotate); a > max {
			max = a
		}
	}
	return max
}

func maxRigRotateAt(frames []RigKeyframe) int {
	best := frames[0].Rotate
	at := frames[0].AtMS
	for _, frame := range frames[1:] {
		if frame.Rotate > best {
			best = frame.Rotate
			at = frame.AtMS
		}
	}
	return at
}

func rigRotateAt(frames []RigKeyframe, at int) (float64, bool) {
	for _, frame := range frames {
		if frame.AtMS == at {
			return frame.Rotate, true
		}
	}
	return 0, false
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
