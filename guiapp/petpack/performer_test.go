package petpack

import (
	"fmt"
	"image"
	"image/color"
	"testing"
)

func performanceTestRig() *Rig {
	clips := map[string]RigClip{}
	for _, name := range []string{
		"idle_in", "idle_loop", "idle_out", "listen_in", "listen_loop", "listen_out", "think_in", "think_loop", "think_out",
		"speak_in", "speak_loop", "speak_out", "done_in", "done_loop", "done_out", "alert_in", "alert_loop", "alert_out", "quiet_in", "quiet_loop", "quiet_out",
		"expr", "gaze", "react",
	} {
		clips[name] = RigClip{DurationMS: 1000, Loop: true}
	}
	for i := 0; i < 6; i++ {
		clips[fmt.Sprintf("expr_%d", i)] = RigClip{DurationMS: 1000, Loop: true}
	}
	for i := 0; i < 4; i++ {
		clips[fmt.Sprintf("gaze_%d", i)] = RigClip{DurationMS: 1000, Loop: true}
	}
	return &Rig{Version: 1, Bones: []RigBone{{Name: "root"}}, Slots: []RigSlot{{Name: "body", Bone: "root", Texture: "rig/body.png"}}, Clips: clips}
}

func performanceTestPerformer() *Performer {
	p := &Performer{Version: 1, Moods: []string{"calm", "curious", "focused", "pleased", "concerned", "tired"}, Layers: []string{"body", "expression", "gaze", "secondary"}, Behaviors: map[string]PerformerBehavior{}, States: map[string]PerformerState{}, Events: map[string]PerformerEvent{}, Reactions: map[string]PerformerReaction{}, Rules: PerformerRules{NoRepeatLast: 3, CrossfadeMS: 180, MaxInterruptMS: 800}}
	pool := make([]string, 0, 10)
	for i := 0; i < 10; i++ {
		name := fmt.Sprintf("idle_%d", i)
		pool = append(pool, name)
		p.Behaviors[name] = PerformerBehavior{Enter: "idle_in", Loop: "idle_loop", Exit: "idle_out", Weight: i + 1, MinMS: 2500, MaxMS: 3500, CooldownMS: 1000}
	}
	p.States["idle"] = PerformerState{BehaviorPool: pool, Expression: "expr_0", Gaze: "gaze_0"}
	for i, state := range []struct {
		name  string
		clips [3]string
	}{
		{"listening", [3]string{"listen_in", "listen_loop", "listen_out"}},
		{"thinking", [3]string{"think_in", "think_loop", "think_out"}},
		{"speaking", [3]string{"speak_in", "speak_loop", "speak_out"}},
		{"done", [3]string{"done_in", "done_loop", "done_out"}},
		{"alert", [3]string{"alert_in", "alert_loop", "alert_out"}},
		{"quiet", [3]string{"quiet_in", "quiet_loop", "quiet_out"}},
	} {
		expression := fmt.Sprintf("expr_%d", (i+1)%6)
		p.States[state.name] = PerformerState{Enter: state.clips[0], Loop: state.clips[1], Exit: state.clips[2], Expression: expression, Gaze: fmt.Sprintf("gaze_%d", (i+1)%4)}
	}
	for _, event := range []string{"click", "hover", "drag_start", "drag_end", "task_started", "task_done", "task_failed", "long_idle"} {
		p.Events[event] = PerformerEvent{Play: "react", Interrupt: "soft", CooldownMS: 500}
	}
	for i, event := range []string{"click", "click", "hover", "drag_start", "drag_end", "task_started", "task_started", "task_done", "task_done", "task_failed", "task_failed", "long_idle"} {
		p.Reactions[fmt.Sprintf("reaction_%d", i)] = PerformerReaction{Event: event, Play: "react", Interrupt: "soft", CooldownMS: 500, Weight: 1}
	}
	return p
}

func TestValidatePerformerAcceptsRichDeclaredCharacter(t *testing.T) {
	p := performanceTestPerformer()
	if err := ValidatePerformer(p, performanceTestRig()); err != nil {
		t.Fatalf("ValidatePerformer: %v", err)
	}
	d := NewPerformanceDirector(p, performanceTestRig(), 7)
	first := d.SelectState(StateIdle, 1000)
	second := d.SelectState(StateIdle, 9000)
	if first.Body == "" || second.Body == "" {
		t.Fatal("idle director did not select behavior")
	}
	if event := d.SelectEvent("task_done", 12000); event.Body != "react" {
		t.Fatalf("event=%+v", event)
	}
}

func TestValidatePerformerRejectsMechanicalIdlePool(t *testing.T) {
	p := performanceTestPerformer()
	delete(p.Behaviors, "idle_9")
	if err := ValidatePerformer(p, performanceTestRig()); err == nil {
		t.Fatal("expected small idle pool rejection")
	}
}

func TestPerformanceDirectorAvoidsRecentIdlesAndUsesReactionVariants(t *testing.T) {
	p := performanceTestPerformer()
	d := NewPerformanceDirector(p, performanceTestRig(), 17)
	seen := make([]string, 0, 4)
	for i := 0; i < 4; i++ {
		step := d.SelectState(StateIdle, int64(10000+i*5000))
		if step.Body == "" {
			t.Fatal("idle behavior not selected")
		}
		name := d.lastBehaviors[len(d.lastBehaviors)-1]
		for _, earlier := range seen {
			if name == earlier {
				t.Fatalf("selected recent idle %q from %v", name, seen)
			}
		}
		seen = append(seen, name)
		if len(seen) > 3 {
			seen = seen[1:]
		}
	}
	if step := d.SelectEvent("click", 40000); step.Body != "react" {
		t.Fatalf("reaction=%+v", step)
	}
}

func TestValidatePerformerRejectsIdlePoolWithoutCalmBehavior(t *testing.T) {
	p := performanceTestPerformer()
	for name, behavior := range p.Behaviors {
		behavior.Moods = []string{"tired"}
		p.Behaviors[name] = behavior
	}
	if err := ValidatePerformer(p, performanceTestRig()); err == nil {
		t.Fatal("idle pool without a calm behavior should be rejected")
	}
}

func TestValidatePerformerRejectsUnachievableIdleRepeatRule(t *testing.T) {
	p := performanceTestPerformer()
	p.Rules.NoRepeatLast = len(p.States["idle"].BehaviorPool)
	if err := ValidatePerformer(p, performanceTestRig()); err == nil {
		t.Fatal("expected idle pool without a non-recent fallback to be rejected")
	}
}

func TestPerformanceDirectorResetsImplicitIdleMoodToCalm(t *testing.T) {
	p := performanceTestPerformer()
	for name, behavior := range p.Behaviors {
		behavior.Moods = []string{"calm"}
		p.Behaviors[name] = behavior
	}
	d := NewPerformanceDirector(p, performanceTestRig(), 53)
	d.currentMood = "concerned"
	if step := d.SelectState(StateIdle, 1000); step.isZero() || d.currentMood != "calm" {
		t.Fatalf("implicit idle did not reset to calm: step=%+v mood=%q", step, d.currentMood)
	}
}

func TestPerformanceDirectorSelectsReactionVariantsDeterministically(t *testing.T) {
	p := performanceTestPerformer()
	p.Reactions = map[string]PerformerReaction{
		"reaction_b": {Event: "click", Play: "react_b", Weight: 2},
		"reaction_a": {Event: "click", Play: "react_a", Weight: 1},
		"reaction_c": {Event: "click", Play: "react_c", Weight: 3},
	}
	rig := performanceTestRig()
	d1, d2 := NewPerformanceDirector(p, rig, 91), NewPerformanceDirector(p, rig, 91)
	for _, nowMS := range []int64{1000, 2000, 3000, 4000, 5000} {
		got, want := d1.SelectEvent("click", nowMS), d2.SelectEvent("click", nowMS)
		if got.Body == "" || got.Body != want.Body {
			t.Fatalf("now=%d: non-deterministic reactions: got=%q want=%q", nowMS, got.Body, want.Body)
		}
	}
}

func TestCharacterRendererReactionReturnsToSemanticState(t *testing.T) {
	rig := performanceTestRig()
	texture := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	texture.SetNRGBA(0, 0, color.NRGBA{R: 80, G: 140, B: 220, A: 255})
	clips, fallback := rigClips(rig)
	renderer := &CharacterRenderer{
		rig:      &RigRenderer{rig: rig, textures: map[string]*image.NRGBA{"rig/body.png": texture}, parentOrder: []int{0}, slots: rig.SortedSlots(), boneIndex: map[string]int{"root": 0}, clips: clips, fallbackClip: fallback},
		director: NewPerformanceDirector(performanceTestPerformer(), rig, 23),
	}
	if renderer.RenderState(StateIdle, 1000, 16) == nil {
		t.Fatal("idle frame is nil")
	}
	if !renderer.TriggerEvent("click", 2000) {
		t.Fatal("click reaction was rejected")
	}
	if renderer.step.Loop != "" {
		t.Fatal("reaction must be a one-shot")
	}
	if renderer.RenderState(StateIdle, 3100, 16) == nil {
		t.Fatal("post-reaction frame is nil")
	}
	if renderer.step.Body == "react" {
		t.Fatal("reaction did not return to the semantic state")
	}
}

func TestCharacterRendererDefersAfterLoopEventUntilLoopBoundary(t *testing.T) {
	rig := performanceTestRig()
	texture := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	texture.SetNRGBA(0, 0, color.NRGBA{R: 80, G: 140, B: 220, A: 255})
	clips, fallback := rigClips(rig)
	p := performanceTestPerformer()
	p.Events["task_done"] = PerformerEvent{Play: "react", Interrupt: "after_loop", CooldownMS: 500}
	for name, reaction := range p.Reactions {
		if reaction.Event == "task_done" {
			reaction.Interrupt = "after_loop"
			p.Reactions[name] = reaction
		}
	}
	renderer := &CharacterRenderer{
		rig:      &RigRenderer{rig: rig, textures: map[string]*image.NRGBA{"rig/body.png": texture}, parentOrder: []int{0}, slots: rig.SortedSlots(), boneIndex: map[string]int{"root": 0}, clips: clips, fallbackClip: fallback},
		director: NewPerformanceDirector(p, rig, 29),
	}
	if renderer.RenderState(StateIdle, 1000, 16) == nil {
		t.Fatal("idle frame is nil")
	}
	if !renderer.TriggerEvent("task_done", 1600) || renderer.pending == nil {
		t.Fatal("after_loop event was not queued")
	}
	if renderer.RenderState(StateIdle, 1900, 16) == nil || renderer.step.Body == "react" {
		t.Fatal("after_loop reaction started before its loop boundary")
	}
	if renderer.RenderState(StateIdle, 2800, 16) == nil || renderer.step.Body != "idle_out" || !renderer.exiting {
		t.Fatalf("after_loop event did not play the authored exit at the loop boundary: step=%+v pending=%+v", renderer.step, renderer.pending)
	}
	if renderer.RenderState(StateIdle, 3800, 16) == nil || renderer.step.Body != "react" || renderer.pending != nil {
		t.Fatalf("after_loop event did not start after exit: step=%+v pending=%+v", renderer.step, renderer.pending)
	}
}

func TestCharacterRendererForcesAfterLoopAtInterruptDeadline(t *testing.T) {
	rig := performanceTestRig()
	texture := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	texture.SetNRGBA(0, 0, color.NRGBA{R: 80, G: 140, B: 220, A: 255})
	clips, fallback := rigClips(rig)
	p := performanceTestPerformer()
	p.Rules.MaxInterruptMS = 700
	p.Events["task_done"] = PerformerEvent{Play: "react", Interrupt: "after_loop", CooldownMS: 500}
	for name, reaction := range p.Reactions {
		if reaction.Event == "task_done" {
			reaction.Interrupt = "after_loop"
			p.Reactions[name] = reaction
		}
	}
	renderer := &CharacterRenderer{
		rig:      &RigRenderer{rig: rig, textures: map[string]*image.NRGBA{"rig/body.png": texture}, parentOrder: []int{0}, slots: rig.SortedSlots(), boneIndex: map[string]int{"root": 0}, clips: clips, fallbackClip: fallback},
		director: NewPerformanceDirector(p, rig, 61),
	}
	if renderer.RenderState(StateIdle, 1000, 16) == nil || renderer.RenderState(StateIdle, 2000, 16) == nil {
		t.Fatal("could not enter idle loop")
	}
	if !renderer.TriggerEvent("task_done", 2150) || renderer.pending == nil {
		t.Fatal("after_loop event was not queued")
	}
	if renderer.RenderState(StateIdle, 2750, 16) == nil || renderer.step.Body == "idle_out" {
		t.Fatal("after_loop event advanced before its configured deadline")
	}
	if renderer.RenderState(StateIdle, 2850, 16) == nil || renderer.step.Body != "idle_out" || !renderer.exiting {
		t.Fatalf("after_loop event did not start its exit at deadline: step=%+v pending=%+v", renderer.step, renderer.pending)
	}
}

func TestCharacterRendererDirectEventCancelsObsoleteExitTarget(t *testing.T) {
	rig := performanceTestRig()
	texture := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	texture.SetNRGBA(0, 0, color.NRGBA{R: 80, G: 140, B: 220, A: 255})
	clips, fallback := rigClips(rig)
	renderer := &CharacterRenderer{
		rig:      &RigRenderer{rig: rig, textures: map[string]*image.NRGBA{"rig/body.png": texture}, parentOrder: []int{0}, slots: rig.SortedSlots(), boneIndex: map[string]int{"root": 0}, clips: clips, fallbackClip: fallback},
		director: NewPerformanceDirector(performanceTestPerformer(), rig, 37),
	}
	if renderer.RenderState(StateThinking, 1000, 16) == nil || renderer.RenderState(StateIdle, 1200, 16) == nil {
		t.Fatal("could not start state exit")
	}
	if renderer.step.Body != "think_out" || !renderer.exiting {
		t.Fatalf("expected thinking exit, got %+v", renderer.step)
	}
	if !renderer.TriggerEvent("click", 1300) || renderer.step.Body != "react" || renderer.exiting || renderer.pending != nil {
		t.Fatalf("direct event did not replace state exit: step=%+v pending=%+v", renderer.step, renderer.pending)
	}
}

func TestCharacterRendererAfterLoopWaitsForOneShotReaction(t *testing.T) {
	rig := performanceTestRig()
	texture := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	texture.SetNRGBA(0, 0, color.NRGBA{R: 80, G: 140, B: 220, A: 255})
	clips, fallback := rigClips(rig)
	p := performanceTestPerformer()
	p.Events["task_done"] = PerformerEvent{Play: "react", Interrupt: "after_loop", CooldownMS: 500}
	for name, reaction := range p.Reactions {
		if reaction.Event == "task_done" {
			reaction.Interrupt = "after_loop"
			p.Reactions[name] = reaction
		}
	}
	renderer := &CharacterRenderer{
		rig:      &RigRenderer{rig: rig, textures: map[string]*image.NRGBA{"rig/body.png": texture}, parentOrder: []int{0}, slots: rig.SortedSlots(), boneIndex: map[string]int{"root": 0}, clips: clips, fallbackClip: fallback},
		director: NewPerformanceDirector(p, rig, 41),
	}
	if renderer.RenderState(StateIdle, 1000, 16) == nil || !renderer.TriggerEvent("click", 2000) {
		t.Fatal("could not start direct reaction")
	}
	if !renderer.TriggerEvent("task_done", 2100) || renderer.pending == nil {
		t.Fatal("after_loop reaction was not queued behind one-shot")
	}
	if renderer.RenderState(StateIdle, 2500, 16) == nil || renderer.step.Body != "react" || renderer.pending == nil {
		t.Fatalf("one-shot reaction was interrupted early: step=%+v pending=%+v", renderer.step, renderer.pending)
	}
	if renderer.RenderState(StateIdle, 3100, 16) == nil || renderer.step.Body != "react" || renderer.pending != nil {
		t.Fatalf("queued reaction did not start after one-shot completion: step=%+v pending=%+v", renderer.step, renderer.pending)
	}
}

func TestCharacterRendererDefersFirstHostEventUntilInitialStateExists(t *testing.T) {
	rig := performanceTestRig()
	texture := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	texture.SetNRGBA(0, 0, color.NRGBA{R: 80, G: 140, B: 220, A: 255})
	clips, fallback := rigClips(rig)
	renderer := &CharacterRenderer{
		rig:      &RigRenderer{rig: rig, textures: map[string]*image.NRGBA{"rig/body.png": texture}, parentOrder: []int{0}, slots: rig.SortedSlots(), boneIndex: map[string]int{"root": 0}, clips: clips, fallbackClip: fallback},
		director: NewPerformanceDirector(performanceTestPerformer(), rig, 43),
	}
	if !renderer.TriggerEvent("click", 900) || renderer.deferredEvent != "click" {
		t.Fatal("first host event was not retained during lazy initialization")
	}
	if renderer.RenderState(StateIdle, 1000, 16) == nil || renderer.deferredEvent != "" || renderer.step.Body != "react" {
		t.Fatalf("deferred event did not run after initial state: step=%+v deferred=%q", renderer.step, renderer.deferredEvent)
	}
}

func TestCharacterRendererDeferredSoftEventDoesNotAdvanceImmediately(t *testing.T) {
	rig := performanceTestRig()
	texture := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	texture.SetNRGBA(0, 0, color.NRGBA{R: 80, G: 140, B: 220, A: 255})
	clips, fallback := rigClips(rig)
	renderer := &CharacterRenderer{
		rig:      &RigRenderer{rig: rig, textures: map[string]*image.NRGBA{"rig/body.png": texture}, parentOrder: []int{0}, slots: rig.SortedSlots(), boneIndex: map[string]int{"root": 0}, clips: clips, fallbackClip: fallback},
		director: NewPerformanceDirector(performanceTestPerformer(), rig, 47),
	}
	if !renderer.TriggerEvent("click", 900) || renderer.RenderState(StateIdle, 1000, 16) == nil {
		t.Fatal("could not render first deferred soft event")
	}
	if renderer.step.Body != "react" {
		t.Fatalf("deferred soft reaction advanced away on its first frame: %+v", renderer.step)
	}
}

func TestCharacterRendererCompletesStateExitBeforeEnteringTarget(t *testing.T) {
	rig := performanceTestRig()
	texture := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	texture.SetNRGBA(0, 0, color.NRGBA{R: 80, G: 140, B: 220, A: 255})
	clips, fallback := rigClips(rig)
	renderer := &CharacterRenderer{
		rig:      &RigRenderer{rig: rig, textures: map[string]*image.NRGBA{"rig/body.png": texture}, parentOrder: []int{0}, slots: rig.SortedSlots(), boneIndex: map[string]int{"root": 0}, clips: clips, fallbackClip: fallback},
		director: NewPerformanceDirector(performanceTestPerformer(), rig, 31),
	}
	if renderer.RenderState(StateThinking, 1000, 16) == nil {
		t.Fatal("thinking frame is nil")
	}
	if renderer.RenderState(StateIdle, 1200, 16) == nil || renderer.step.Body != "think_out" || !renderer.exiting {
		t.Fatalf("state transition did not start authored exit: step=%+v pending=%+v", renderer.step, renderer.pending)
	}
	if renderer.RenderState(StateIdle, 1600, 16) == nil || renderer.step.Body != "think_out" || !renderer.exiting {
		t.Fatalf("timer frame restarted or canceled authored exit: step=%+v pending=%+v", renderer.step, renderer.pending)
	}
	if renderer.RenderState(StateIdle, 2200, 16) == nil || renderer.step.Body == "think_out" || renderer.pending != nil {
		t.Fatalf("state transition did not enter idle after exit: step=%+v pending=%+v", renderer.step, renderer.pending)
	}
}

func TestValidatePerformerRequiresExpressionAndGazeVariety(t *testing.T) {
	p := performanceTestPerformer()
	for name, state := range p.States {
		state.Expression, state.Gaze = "expr_0", "gaze_0"
		p.States[name] = state
	}
	if err := ValidatePerformer(p, performanceTestRig()); err == nil {
		t.Fatal("expected expression/gaze variety rejection")
	}
}
