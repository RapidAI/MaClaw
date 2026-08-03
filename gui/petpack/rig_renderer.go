package petpack

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// RigRenderer owns decoded texture assets for one resolved v2 pack. It is
// immutable after construction and therefore safe to share with a render loop.
type RigRenderer struct {
	rig          *Rig
	textures     map[string]*image.NRGBA
	parentOrder  []int
	slots        []RigSlot
	boneIndex    map[string]int
	clips        map[string]*RigClip
	fallbackClip *RigClip
	// multiHead is true when the pack uses expression-head paper-doll slots
	// (head_*.png / h_idle…). Join aids only activate for these packs unless
	// rig.join.auto is forced on/off.
	multiHead bool
	// join resolved once at load (defaults applied).
	join effectiveJoin
	// collarCoverTex is a precomputed top-of-body strip (or nil when unused /
	// explicit overlay). Built once at load so frames do not re-scan body pixels.
	collarCoverTex *image.NRGBA
}

// effectiveJoin is the resolved join policy after defaults.
type effectiveJoin struct {
	enabled            bool
	collarCover        bool
	collarCoverFrac    float64
	collarOverlay      string
	headNeckFadePx     int
	headNeckFadeCenter float64
	stateHeadOffset    map[string]RigHeadOffset
	// bodySlot / headBone used for auto collar cover and state offsets.
	bodySlot  string
	bodyBone  string
	headBone  string
	headSlots []string // expression head slot names
}

type rigTransform struct{ x, y, rotation, sx, sy float64 }

// NewRigRenderer loads the rig plus all declared textures once. Any failure is
// intentionally recoverable by callers through native/idle fallback frames.
func NewRigRenderer(resolved *ResolvedPack) (*RigRenderer, error) {
	if resolved == nil || (resolved.Renderer != RendererSkeleton && resolved.Renderer != RendererCharacter) {
		return nil, nil
	}
	rig, err := LoadRig(resolved)
	if err != nil || rig == nil {
		return nil, err
	}
	textureData, err := ReadRigTextureData(resolved.AssetFS, resolved.Rig)
	if err != nil {
		return nil, err
	}
	textures := make(map[string]*image.NRGBA, len(textureData))
	for rel, raw := range textureData {
		img, _, decodeErr := image.Decode(bytes.NewReader(raw))
		if decodeErr != nil {
			return nil, fmt.Errorf("decode rig texture %s: %w", rel, decodeErr)
		}
		textures[rel] = copyToNRGBA(img)
	}
	parentOrder, boneIndex, err := rigParentOrder(rig)
	if err != nil {
		return nil, err
	}
	clips, fallbackClip := rigClips(rig)
	slots := rig.SortedSlots()
	multiHead := detectMultiHeadPack(slots)
	join := resolveJoin(rig, slots, multiHead)
	// Pre-bake neck fade into full heads / face ovals (not fixed hair).
	if join.enabled && join.headNeckFadePx > 0 {
		for _, name := range join.headSlots {
			for i := range slots {
				if slots[i].Name != name {
					continue
				}
				// Never fade the fixed hair layer — it is the stable silhouette.
				if isHairSlot(slots[i]) {
					continue
				}
				key := safeRel(slots[i].Texture)
				if tex := textures[key]; tex != nil {
					// Face-only layers use a milder center fade (already short chin).
					fadePx := join.headNeckFadePx
					if isExpressionFaceSlot(slots[i]) && fadePx > 6 {
						fadePx = 6
					}
					textures[key] = applyHeadNeckFade(tex, fadePx, join.headNeckFadeCenter)
				}
			}
		}
	}
	rr := &RigRenderer{
		rig:          rig,
		textures:     textures,
		parentOrder:  parentOrder,
		slots:        slots,
		boneIndex:    boneIndex,
		clips:        clips,
		fallbackClip: fallbackClip,
		multiHead:    multiHead,
		join:         join,
	}
	// Precompute collar cover strip once (hot path is per-frame).
	if join.enabled && join.collarCover && join.collarOverlay == "" && join.bodySlot != "" {
		for i := range slots {
			if slots[i].Name != join.bodySlot {
				continue
			}
			if tex := textures[safeRel(slots[i].Texture)]; tex != nil {
				rr.collarCoverTex = bodyTopStrip(tex, join.collarCoverFrac)
			}
			break
		}
	}
	return rr, nil
}

func detectMultiHeadPack(slots []RigSlot) bool {
	heads := 0
	faces := 0
	hasHair := false
	for _, slot := range slots {
		if isExpressionHeadSlot(slot) {
			heads++
		}
		if isExpressionFaceSlot(slot) {
			faces++
		}
		if isHairSlot(slot) {
			hasHair = true
		}
	}
	// Classic body+head_* paper-doll, or hair+face_* layout.
	return heads >= 2 || (hasHair && faces >= 2) || faces >= 2
}

func isExpressionHeadSlot(slot RigSlot) bool {
	tex := strings.ToLower(filepath.Base(safeRel(slot.Texture)))
	name := strings.ToLower(slot.Name)
	if strings.HasPrefix(tex, "head_") || strings.Contains(tex, "head_idle") || strings.Contains(tex, "head_speak") {
		return true
	}
	if strings.HasPrefix(name, "h_") || strings.HasPrefix(name, "slot_h_") || strings.Contains(name, "head_") {
		return true
	}
	// face_* is handled separately for fade policy, but still counts as expression layer.
	return isExpressionFaceSlot(slot)
}

// isExpressionFaceSlot is the hair+face layout: only the face oval swaps by mood.
func isExpressionFaceSlot(slot RigSlot) bool {
	tex := strings.ToLower(filepath.Base(safeRel(slot.Texture)))
	name := strings.ToLower(slot.Name)
	if strings.HasPrefix(tex, "face_") {
		return true
	}
	if strings.HasPrefix(name, "f_") || strings.HasPrefix(name, "slot_f_") || strings.Contains(name, "face_") {
		return true
	}
	return false
}

func isHairSlot(slot RigSlot) bool {
	tex := strings.ToLower(filepath.Base(safeRel(slot.Texture)))
	name := strings.ToLower(slot.Name)
	return tex == "hair.png" || strings.HasSuffix(tex, "/hair.png") || name == "hair" || name == "slot_hair"
}

// isIdleExpressionSlot is the default-visible face/head when no expression
// clip is driving alpha (body-only pose).
func isIdleExpressionSlot(slot RigSlot) bool {
	tex := strings.ToLower(filepath.Base(safeRel(slot.Texture)))
	name := strings.ToLower(slot.Name)
	if strings.Contains(tex, "idle") || strings.HasSuffix(name, "_idle") || name == "h_idle" || name == "f_idle" || name == "slot_h_idle" || name == "slot_f_idle" {
		return isExpressionHeadSlot(slot)
	}
	return false
}

// clipsDriveExpressionAlpha reports whether any active clip keys alpha (or any
// track) on an expression head/face slot or its bone — i.e. performer expression
// layer is present. Body-only clips leave this false.
func clipsDriveExpressionAlpha(clips []*RigClip, slots []RigSlot) bool {
	if len(clips) == 0 || len(slots) == 0 {
		return false
	}
	exprKeys := make(map[string]bool, len(slots)*2)
	for _, slot := range slots {
		if !isExpressionHeadSlot(slot) || isHairSlot(slot) {
			continue
		}
		if slot.Name != "" {
			exprKeys[slot.Name] = true
		}
		if slot.Bone != "" {
			exprKeys[slot.Bone] = true
		}
	}
	if len(exprKeys) == 0 {
		return false
	}
	for _, clip := range clips {
		if clip == nil {
			continue
		}
		for name, frames := range clip.Tracks {
			if !exprKeys[name] || len(frames) == 0 {
				continue
			}
			// Any keyframed track on an expression bone/slot counts as driven
			// (alpha or transform). Packs put expression visibility on alpha.
			return true
		}
	}
	return false
}

func resolveJoin(rig *Rig, slots []RigSlot, multiHead bool) effectiveJoin {
	ej := effectiveJoin{
		collarCoverFrac:    0.18,
		headNeckFadePx:     10,
		headNeckFadeCenter: 0.42,
		stateHeadOffset:    map[string]RigHeadOffset{},
	}
	// Locate body + head bone + head slots.
	for _, slot := range slots {
		if isExpressionHeadSlot(slot) {
			ej.headSlots = append(ej.headSlots, slot.Name)
			if ej.headBone == "" {
				// Prefer parent of expression bone if present (h_idle → head).
				if bone := findBone(rig, slot.Bone); bone != nil && bone.Parent != "" {
					ej.headBone = bone.Parent
				} else {
					ej.headBone = slot.Bone
				}
			}
		}
		tex := strings.ToLower(filepath.Base(safeRel(slot.Texture)))
		if ej.bodySlot == "" && (tex == "body.png" || strings.HasSuffix(tex, "/body.png") || slot.Name == "body" || slot.Name == "slot_body") {
			ej.bodySlot = slot.Name
			ej.bodyBone = slot.Bone
		}
	}
	if ej.bodySlot == "" {
		// Fallback: lowest-z non-head slot as body.
		for _, slot := range slots {
			if !isExpressionHeadSlot(slot) {
				ej.bodySlot = slot.Name
				ej.bodyBone = slot.Bone
				break
			}
		}
	}
	cfg := rig.Join
	auto := multiHead
	if cfg != nil && cfg.Auto != nil {
		auto = *cfg.Auto
	}
	ej.enabled = auto
	if !ej.enabled {
		return ej
	}
	ej.collarCover = multiHead
	if cfg != nil {
		if cfg.CollarCover != nil {
			ej.collarCover = *cfg.CollarCover
		}
		if cfg.CollarCoverFrac > 0 {
			ej.collarCoverFrac = cfg.CollarCoverFrac
		}
		if cfg.CollarOverlay != "" {
			ej.collarOverlay = safeRel(cfg.CollarOverlay)
			ej.collarCover = true
		}
		if cfg.HeadNeckFadePx > 0 {
			ej.headNeckFadePx = cfg.HeadNeckFadePx
		}
		if cfg.HeadNeckFadeCenter > 0 {
			ej.headNeckFadeCenter = cfg.HeadNeckFadeCenter
		}
		for k, v := range cfg.StateHeadOffset {
			ej.stateHeadOffset[k] = v
		}
	}
	// No body → nothing to cover with.
	if ej.bodySlot == "" || ej.bodyBone == "" {
		ej.collarCover = false
	}
	return ej
}

func findBone(rig *Rig, name string) *RigBone {
	if rig == nil {
		return nil
	}
	for i := range rig.Bones {
		if rig.Bones[i].Name == name {
			return &rig.Bones[i]
		}
	}
	return nil
}

// applyHeadNeckFade soft-clears the center-bottom of an expression head so a
// long flesh neck stump is less likely to stick out of the collar. Side hair
// is preserved. Operates on a copy.
func applyHeadNeckFade(src *image.NRGBA, fadePx int, centerFrac float64) *image.NRGBA {
	if src == nil || fadePx < 1 {
		return src
	}
	if centerFrac < 0.15 {
		centerFrac = 0.42
	}
	b := src.Bounds()
	out := image.NewNRGBA(b)
	copy(out.Pix, src.Pix)
	w, h := b.Dx(), b.Dy()
	if fadePx > h/3 {
		fadePx = h / 3
	}
	cx := float64(w) / 2
	half := float64(w) * centerFrac / 2
	if half < 1 {
		half = 1
	}
	// Find content bottom via raw alpha.
	pix := out.Pix
	stride := out.Stride
	bot := -1
	for y := h - 1; y >= 0; y-- {
		row := y * stride
		for x := 0; x < w; x++ {
			if pix[row+x*4+3] > 20 {
				bot = y
				break
			}
		}
		if bot >= 0 {
			break
		}
	}
	if bot < 0 {
		return out
	}
	start := bot - fadePx + 1
	if start < 0 {
		start = 0
	}
	for y := start; y <= bot; y++ {
		t := float64(bot-y) / float64(fadePx) // 1 at top of fade, 0 at bottom
		row := y * stride
		for x := 0; x < w; x++ {
			nx := math.Abs(float64(x)-cx) / half
			if nx >= 1 {
				continue // side hair untouched
			}
			// Center weight: 1 at middle, 0 at edge of center band.
			centerW := 1 - nx
			// Fade amount grows toward bottom.
			fade := (1 - t) * (0.35 + 0.65*centerW)
			if fade <= 0 {
				continue
			}
			ai := row + x*4 + 3
			a := pix[ai]
			if a == 0 {
				continue
			}
			na := int(float64(a)*(1-fade) + 0.5)
			if na < 0 {
				na = 0
			}
			if na > 255 {
				na = 255
			}
			pix[ai] = uint8(na)
		}
	}
	return out
}

// CharacterRenderer runs the restricted v3 performance director over a
// validated rig renderer. It owns no external capability: semantic state and
// allowlisted events are its only inputs.
type CharacterRenderer struct {
	rig              *RigRenderer
	director         *PerformanceDirector
	mu               sync.Mutex
	state            PetRuntimeState
	step             PerformanceStep
	startedMS        int64
	looping          bool
	pending          *PerformanceStep
	pendingAfterLoop bool
	// pendingSinceMS is set only for an after_loop event. It makes the
	// performer rule's max_interrupt_ms an actual upper bound rather than a
	// decorative schema field when a render loop misses a natural boundary.
	pendingSinceMS int64
	// deferredEvent preserves the newest allowlisted host event that arrives
	// before a caller has selected the first semantic state.
	deferredEvent   string
	exiting         bool
	previous        PerformanceStep
	previousAt      int64
	previousElapsed int64
	idleSinceMS     int64
}

func NewCharacterRenderer(resolved *ResolvedPack) (*CharacterRenderer, error) {
	if resolved == nil || resolved.Renderer != RendererCharacter || resolved.Character == nil {
		return nil, nil
	}
	rigRenderer, err := NewRigRenderer(resolved)
	if err != nil || rigRenderer == nil {
		return nil, err
	}
	raw, err := readAsset(resolved, resolved.Character.Definition)
	if err != nil {
		return nil, fmt.Errorf("read character definition: %w", err)
	}
	performer, err := ParsePerformer(raw)
	if err != nil {
		return nil, err
	}
	if err := ValidatePerformer(performer, rigRenderer.rig); err != nil {
		return nil, err
	}
	seed := uint64(1469598103934665603)
	for _, b := range []byte(resolved.Manifest.ID) {
		seed ^= uint64(b)
		seed *= 1099511628211
	}
	return &CharacterRenderer{rig: rigRenderer, director: NewPerformanceDirector(performer, rigRenderer.rig, seed)}, nil
}

// RenderState selects a behavior only when a semantic state starts or its
// selected behavior expires, avoiding per-frame randomization and mechanical
// repetition. Four declared layers are composed in stable order.
func (r *CharacterRenderer) RenderState(state PetRuntimeState, nowMS int64, size int) *image.NRGBA {
	if r == nil || r.rig == nil || r.director == nil {
		return nil
	}
	r.mu.Lock()
	if state == "" {
		state = StateIdle
	}
	if r.step.Body == "" || r.state != state {
		wasIdle := r.state == StateIdle
		// The semantic state is the desired destination, even while an authored
		// exit is playing. Keep the pending target intact on later timer frames;
		// otherwise RenderState would repeatedly cancel the exit and restart it.
		if r.state != state || r.step.Body == "" {
			r.state = state
			target := r.director.SelectState(state, nowMS)
			if r.exiting {
				// Preserve the exit already on screen, but let a newer semantic
				// state retarget where it lands instead of using stale intent.
				copy := target
				r.pending = &copy
			} else if !target.isZero() {
				r.pending = nil
				r.pendingAfterLoop = false
				r.pendingSinceMS = 0
				r.transitionToLocked(target, nowMS)
			}
		}
		if state == StateIdle && !wasIdle {
			r.idleSinceMS = nowMS
		} else if state != StateIdle {
			r.idleSinceMS = 0
		}
	} else if state == StateIdle && r.idleSinceMS == 0 {
		r.idleSinceMS = nowMS
	}
	if r.deferredEvent != "" && r.step.Body != "" {
		event := r.deferredEvent
		r.deferredEvent = ""
		r.triggerEventLocked(event, nowMS)
	}
	// A direct/soft reaction may have replaced a semantic transition above.
	// Its duration is measured from this frame, so do not immediately advance
	// the newly installed step through the rest of the state machine.
	if r.step.Body != "" && r.startedMS == nowMS && !r.exiting {
		step, started, previous, previousAt, previousElapsed := r.step, r.startedMS, r.previous, r.previousAt, r.previousElapsed
		r.mu.Unlock()
		return r.renderPerformanceStep(step, started, previous, previousAt, previousElapsed, nowMS, size)
	}
	if r.exiting && r.step.DurationMS > 0 && nowMS-r.startedMS >= int64(r.step.DurationMS) {
		target := r.pending
		r.pending = nil
		r.pendingAfterLoop = false
		r.pendingSinceMS = 0
		r.exiting = false
		if target != nil {
			r.transitionToLocked(*target, nowMS)
		}
	} else if !r.exiting && r.pending != nil && r.pendingAfterLoop && (r.canFinishCurrentStep(nowMS) || r.afterLoopDeadlineReached(nowMS)) {
		target := *r.pending
		r.pending = nil
		r.pendingAfterLoop = false
		r.pendingSinceMS = 0
		r.transitionToLocked(target, nowMS)
	} else if !r.looping && r.step.Loop != "" && nowMS-r.startedMS >= int64(r.step.EntryMS) {
		r.step.Body = r.step.Loop
		r.step.DurationMS = r.step.DwellMS
		r.startedMS = nowMS
		r.looping = true
	} else if !r.looping && r.step.Loop == "" && r.step.DurationMS > 0 && nowMS-r.startedMS >= int64(r.step.DurationMS) {
		// One-shot event reactions settle back into the current semantic state
		// instead of leaving their final pose on screen indefinitely.
		r.transitionToLocked(r.director.SelectState(state, nowMS), nowMS)
	} else if r.looping && state == StateIdle && r.step.DwellMS > 0 && nowMS-r.startedMS >= int64(r.step.DwellMS) {
		// Idle behavior dwell time is intentionally non-uniform. Once the loop has
		// settled, request a new weighted behavior; cooldown/repeat rules decide it.
		r.transitionToLocked(r.director.SelectState(state, nowMS), nowMS)
	}
	if state == StateIdle && r.idleSinceMS > 0 && nowMS-r.idleSinceMS >= 20000 && r.pending == nil && !r.exiting && r.canFinishCurrentStep(nowMS) {
		r.triggerEventLocked("long_idle", nowMS)
		r.idleSinceMS = nowMS
	}
	step, started, previous, previousAt, previousElapsed := r.step, r.startedMS, r.previous, r.previousAt, r.previousElapsed
	r.mu.Unlock()
	return r.renderPerformanceStep(step, started, previous, previousAt, previousElapsed, nowMS, size)
}

func (r *CharacterRenderer) renderPerformanceStep(step PerformanceStep, started int64, previous PerformanceStep, previousAt, previousElapsed, nowMS int64, size int) *image.NRGBA {
	elapsed := int(nowMS - started)
	clips := performanceStepClips(r.rig, step)
	current := r.rig.RenderClips(clips, elapsed, size)
	if current == nil || previous.Body == "" || step.CrossfadeMS < 1 || nowMS-previousAt >= int64(step.CrossfadeMS) {
		return current
	}
	previousClips := performanceStepClips(r.rig, previous)
	old := r.rig.RenderClips(previousClips, int(previousElapsed+nowMS-previousAt), size)
	if old == nil {
		return current
	}
	return blendRigFrames(old, current, float64(nowMS-previousAt)/float64(step.CrossfadeMS))
}

// performanceStepClips lists body + layer clips that actually carry tracks.
// Packs declare empty gaze_*/secondary_* placeholders; including them only
// multiplies default alpha=1 and wastes a full bone pass.
func performanceStepClips(rr *RigRenderer, step PerformanceStep) []string {
	clips := make([]string, 0, 4)
	if step.Body != "" {
		clips = append(clips, step.Body)
	}
	for _, name := range []string{step.Expression, step.Gaze, step.Secondary} {
		if name == "" || name == step.Body {
			continue
		}
		if rr == nil || rr.clips == nil {
			clips = append(clips, name)
			continue
		}
		clip := rr.clips[name]
		if clip == nil {
			continue
		}
		if len(clip.Tracks) == 0 {
			continue
		}
		clips = append(clips, name)
	}
	return clips
}

func blendRigFrames(previous, current *image.NRGBA, progress float64) *image.NRGBA {
	if previous == nil || current == nil || !previous.Bounds().Eq(current.Bounds()) {
		return current
	}
	progress = math.Max(0, math.Min(1, progress))
	if progress <= 0 {
		dup := image.NewNRGBA(previous.Bounds())
		copy(dup.Pix, previous.Pix)
		return dup
	}
	if progress >= 1 {
		dup := image.NewNRGBA(current.Bounds())
		copy(dup.Pix, current.Pix)
		return dup
	}
	out := image.NewNRGBA(current.Bounds())
	// Straight-alpha lerp (NRGBA is not premultiplied). Naive per-channel
	// blends darken transparent edges during behavior crossfades.
	// Fixed-point: w = progress in 0..256, weights scaled by alpha.
	w := int(progress*256 + 0.5)
	if w < 0 {
		w = 0
	}
	if w > 256 {
		w = 256
	}
	iw := 256 - w
	pp, cp, op := previous.Pix, current.Pix, out.Pix
	for i := 0; i+3 < len(op); i += 4 {
		pa, ca := int(pp[i+3]), int(cp[i+3])
		// outA = pa*(1-t) + ca*t
		oa := (pa*iw + ca*w + 128) >> 8
		if oa > 255 {
			oa = 255
		}
		op[i+3] = byte(oa)
		if oa == 0 {
			op[i], op[i+1], op[i+2] = 0, 0, 0
			continue
		}
		// outC = (c1*a1*(1-t) + c2*a2*t) / outA  (rounded, clamped)
		den := oa * 256
		half := den / 2
		for c := 0; c < 3; c++ {
			v := (int(pp[i+c])*pa*iw + int(cp[i+c])*ca*w + half) / den
			if v > 255 {
				v = 255
			}
			op[i+c] = byte(v)
		}
	}
	return out
}

// TriggerEvent starts one validated short reaction. It is intentionally a
// separate method so host windows can map only allowlisted local events.
func (r *CharacterRenderer) TriggerEvent(event string, nowMS int64) bool {
	if r == nil || r.director == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.step.Body == "" {
		if !IsAllowedPerformerEvent(event) {
			return false
		}
		// Keep only the latest semantic interaction while the first state is
		// assembled. This bounds memory and prevents an old hover from replaying
		// after a newer task transition.
		r.deferredEvent = event
		return true
	}
	return r.triggerEventLocked(event, nowMS)
}

func (r *CharacterRenderer) triggerEventLocked(event string, nowMS int64) bool {
	step := r.director.SelectEvent(event, nowMS)
	if step.Body == "" {
		return false
	}
	// Reactions are deliberately one-shots. The semantic state renderer resumes
	// after DurationMS, preserving the former loop as the visual baseline.
	step.Loop = ""
	step.Exit = ""
	if step.Interrupt == "after_loop" && r.step.Body != "" && !r.canFinishCurrentStep(nowMS) {
		copy := step
		r.pending = &copy
		r.pendingAfterLoop = true
		r.pendingSinceMS = nowMS
		return true
	}
	// Soft interactions are direct and interruptible. An after-loop reaction
	// reaches this path only at a natural loop boundary, so it first gets the
	// authored exit of the current behavior.
	r.pending = nil
	r.pendingAfterLoop = false
	r.pendingSinceMS = 0
	if step.Interrupt == "after_loop" {
		r.transitionToLocked(step, nowMS)
	} else {
		r.transitionToLockedWithoutExit(step, nowMS)
	}
	return true
}

// transitionToLocked respects an authored exit clip before entering the next
// semantic behavior. It keeps state changes and after-loop reactions spatially
// continuous, while soft direct interactions can still respond immediately.
func (r *CharacterRenderer) transitionToLocked(target PerformanceStep, nowMS int64) {
	if target.Body == "" {
		return
	}
	previous, previousElapsed := r.step, nowMS-r.startedMS
	if previous.Body != "" && previous.Exit != "" {
		exit := previous
		exit.Body = previous.Exit
		exit.Loop = ""
		exit.Exit = ""
		exit.EntryMS = 0
		exit.DwellMS = 0
		exit.DurationMS = clipDuration(r.rig.rig, exit.Body)
		r.step, r.startedMS = exit, nowMS
		r.looping = false
		copy := target
		r.pending = &copy
		r.pendingAfterLoop = false
		r.pendingSinceMS = 0
		r.exiting = true
		r.setPrevious(previous, previousElapsed, nowMS)
		return
	}
	r.transitionToLockedWithoutExit(target, nowMS)
}

// afterLoopDeadlineReached bounds the latency of an after_loop event. Natural
// boundaries still win, but an irregular render cadence or a very long loop
// must not defer a meaningful local response indefinitely.
func (r *CharacterRenderer) afterLoopDeadlineReached(nowMS int64) bool {
	if r == nil || !r.pendingAfterLoop || r.pendingSinceMS <= 0 || r.director == nil || r.director.performer == nil {
		return false
	}
	return nowMS-r.pendingSinceMS >= int64(r.director.performer.Rules.MaxInterruptMS)
}

func (r *CharacterRenderer) transitionToLockedWithoutExit(target PerformanceStep, nowMS int64) {
	previous, previousElapsed := r.step, nowMS-r.startedMS
	r.step, r.startedMS = target, nowMS
	r.looping = false
	r.exiting = false
	r.setPrevious(previous, previousElapsed, nowMS)
}

func (r *CharacterRenderer) setPrevious(step PerformanceStep, elapsedMS, nowMS int64) {
	if step.Body == "" || r == nil || r.director == nil || r.director.performer == nil || r.director.performer.Rules.CrossfadeMS < 1 {
		r.previous = PerformanceStep{}
		r.previousAt = 0
		r.previousElapsed = 0
		return
	}
	r.previous, r.previousAt, r.previousElapsed = step, nowMS, elapsedMS
}

func (r *CharacterRenderer) canFinishCurrentStep(nowMS int64) bool {
	if r == nil || r.step.Body == "" {
		return true
	}
	if !r.looping {
		// A one-shot reaction has no entry/loop split. Its interrupt boundary is
		// the end of its authored clip, not time zero (EntryMS is intentionally
		// zero for reactions). This prevents an after_loop event from cutting a
		// click/hover response off on the very next frame.
		if r.step.Loop == "" && r.step.DurationMS > 0 {
			return nowMS-r.startedMS >= int64(r.step.DurationMS)
		}
		return nowMS-r.startedMS >= int64(r.step.EntryMS)
	}
	clip := r.rig.clips[r.step.Body]
	if clip == nil || clip.DurationMS < 1 {
		return true
	}
	return (nowMS-r.startedMS)%int64(clip.DurationMS) < 50
}

// IsLooping reports whether the effective state clip loops. Callers can keep
// looping idle phases stable across a state switch while one-shot gestures
// still start when their semantic state is entered.
func (r *RigRenderer) IsLooping(state PetRuntimeState) bool {
	clip := r.clipForState(state)
	return clip != nil && clip.Loop
}

// Render composes a clip at elapsedMS into a square NRGBA image. Rig design
// coordinates use a 256×256 reference canvas; rendering is scaled to pet size.
func (r *RigRenderer) Render(state PetRuntimeState, elapsedMS, size int) *image.NRGBA {
	if r == nil || r.rig == nil || size <= 0 {
		return nil
	}
	// Multi-part packs need body + expression; body-only would stack every face
	// (trackAlpha defaults to 1). Prefer the same pairing as settings preview.
	if r.multiHead {
		bodyClip := bodyClipForState(r, state)
		exprClip := expressionClipForState(r, state)
		if bodyClip != "" && exprClip != "" {
			return r.RenderClips([]string{bodyClip, exprClip}, elapsedMS, size)
		}
		if bodyClip != "" {
			return r.RenderClip(bodyClip, elapsedMS, size)
		}
	}
	clip := r.clipForState(state)
	return r.renderClip(clip, elapsedMS, size)
}

// RenderClip renders one named, validated rig clip. It is used by the v3
// performance director; an absent clip returns nil so callers retain fallback.
func (r *RigRenderer) RenderClip(name string, elapsedMS, size int) *image.NRGBA {
	if r == nil {
		return nil
	}
	return r.renderClip(r.clips[name], elapsedMS, size)
}

// RenderClips composes local transform tracks from body, expression, gaze,
// and secondary layers into one pose instead of alpha-stacking whole pets.
func (r *RigRenderer) RenderClips(names []string, elapsedMS, size int) *image.NRGBA {
	if r == nil {
		return nil
	}
	clips := make([]*RigClip, 0, len(names))
	for _, name := range names {
		if clip := r.clips[name]; clip != nil {
			clips = append(clips, clip)
		}
	}
	return r.renderClips(clips, elapsedMS, size)
}

func (r *RigRenderer) renderClip(clip *RigClip, elapsedMS, size int) *image.NRGBA {
	return r.renderClips([]*RigClip{clip}, elapsedMS, size)
}

func (r *RigRenderer) renderClips(clips []*RigClip, elapsedMS, size int) *image.NRGBA {
	if r == nil || r.rig == nil || size <= 0 {
		return nil
	}
	if len(clips) == 0 {
		return nil
	}
	times := make([]int, len(clips))
	for i, clip := range clips {
		if clip == nil {
			return nil
		}
		times[i] = rigClipTime(clip, elapsedMS)
	}

	// Optional per-state head bone offset (from join.state_head_offset).
	headOff := r.joinHeadOffsetForClips(clips, times)

	bones := make([]rigTransform, len(r.rig.Bones))
	for _, index := range r.parentOrder {
		bone := r.rig.Bones[index]
		local := rigTransform{x: bone.X, y: bone.Y, rotation: bone.Rotate, sx: nonZero(bone.ScaleX), sy: nonZero(bone.ScaleY)}
		for i, clip := range clips {
			local = applyRigTrack(local, clip.Tracks[bone.Name], times[i])
		}
		if r.join.enabled && r.join.headBone != "" && bone.Name == r.join.headBone {
			local.x += headOff.X
			local.y += headOff.Y
		}
		if bone.Parent != "" {
			local = combineRigTransform(bones[r.boneIndex[bone.Parent]], local)
		}
		bones[index] = local
	}

	// When multi-part packs play body-only clips (no expression alpha tracks),
	// empty trackAlpha defaults to 1.0 and would stack every face/head layer.
	// If no clip drives expression visibility, show only the idle expression.
	exprDriven := clipsDriveExpressionAlpha(clips, r.slots)

	out := image.NewNRGBA(image.Rect(0, 0, size, size))
	var bodyWorld rigTransform
	var bodyTexture *image.NRGBA
	var bodyAlpha float64
	for _, slot := range r.slots {
		bone, ok := r.boneIndex[slot.Bone]
		if !ok {
			continue
		}
		texture := r.textures[safeRel(slot.Texture)]
		if texture == nil {
			continue
		}
		local := rigTransform{x: slot.X, y: slot.Y, rotation: slot.Rotate, sx: nonZero(slot.ScaleX), sy: nonZero(slot.ScaleY)}
		alpha := 1.0
		for i, clip := range clips {
			// Slot tracks may key either the slot name (slot_h_idle) or the bone
			// name (h_idle). Multi-part packs author expression alpha on bones.
			local = applyRigTrack(local, clip.Tracks[slot.Name], times[i])
			if slot.Bone != "" && slot.Bone != slot.Name {
				local = applyRigTrack(local, clip.Tracks[slot.Bone], times[i])
			}
			alpha *= trackAlpha(clip.Tracks[slot.Name], times[i])
			if slot.Bone != "" && slot.Bone != slot.Name {
				alpha *= trackAlpha(clip.Tracks[slot.Bone], times[i])
			}
		}
		// Body-only multi-head: hide non-idle expression layers (hair stays).
		if r.multiHead && !exprDriven && isExpressionHeadSlot(slot) && !isHairSlot(slot) && !isIdleExpressionSlot(slot) {
			continue
		}
		if alpha < 0.02 {
			continue // skip fully hidden expression heads
		}
		world := combineRigTransform(bones[bone], local)
		if r.join.enabled && slot.Name == r.join.bodySlot {
			bodyWorld, bodyTexture, bodyAlpha = world, texture, alpha
		}
		drawRigTexture(out, texture, world, alpha)
	}
	// Collar cover: re-draw upper body (or explicit overlay) on top of heads so
	// clothing hides residual neck plugs — the #1 multi-part failure mode.
	if r.join.enabled && r.join.collarCover {
		r.drawCollarCover(out, bodyWorld, bodyTexture, bodyAlpha)
	}
	return out
}

// joinHeadOffsetForClips returns a head bone offset when exactly one expression
// head is visible and join.state_head_offset names that expression's state.
func (r *RigRenderer) joinHeadOffsetForClips(clips []*RigClip, times []int) RigHeadOffset {
	if r == nil || !r.join.enabled || len(r.join.stateHeadOffset) == 0 || len(r.join.headSlots) == 0 {
		return RigHeadOffset{}
	}
	// Match draw path: body-only multi-head treats only idle expression as visible.
	exprDriven := clipsDriveExpressionAlpha(clips, r.slots)
	// Find the active expression head slot (highest alpha among head slots).
	active := ""
	best := 0.0
	for _, name := range r.join.headSlots {
		var slot *RigSlot
		for i := range r.slots {
			if r.slots[i].Name == name {
				slot = &r.slots[i]
				break
			}
		}
		if slot == nil || isHairSlot(*slot) {
			continue
		}
		if r.multiHead && !exprDriven && !isIdleExpressionSlot(*slot) {
			continue
		}
		a := 1.0
		bone := slot.Bone
		for i, clip := range clips {
			if clip == nil {
				continue
			}
			a *= trackAlpha(clip.Tracks[name], times[i])
			if bone != "" && bone != name {
				a *= trackAlpha(clip.Tracks[bone], times[i])
			}
		}
		if a > best {
			best = a
			active = name
		}
	}
	if best < 0.5 || active == "" {
		return RigHeadOffset{}
	}
	state := expressionSlotToState(active)
	if state == "" {
		return RigHeadOffset{}
	}
	return r.join.stateHeadOffset[state]
}

func expressionSlotToState(slot string) string {
	s := strings.ToLower(slot)
	// Common patterns: h_idle, slot_h_listen, head_speak, f_think, face_alert
	pairs := []struct{ key, state string }{
		{"listen", "listening"},
		{"think", "thinking"},
		{"speak", "speaking"},
		{"done", "done"},
		{"alert", "alert"},
		{"quiet", "quiet"},
		{"idle", "idle"},
	}
	for _, p := range pairs {
		if strings.Contains(s, p.key) {
			return p.state
		}
	}
	return ""
}

func (r *RigRenderer) drawCollarCover(dst *image.NRGBA, bodyWorld rigTransform, bodyTex *image.NRGBA, alpha float64) {
	if dst == nil || alpha <= 0 {
		return
	}
	// Prefer explicit overlay texture when declared.
	if r.join.collarOverlay != "" {
		if overlay := r.textures[r.join.collarOverlay]; overlay != nil {
			drawRigTexture(dst, overlay, bodyWorld, alpha)
			return
		}
	}
	cover := r.collarCoverTex
	if cover == nil && bodyTex != nil {
		// Fallback if load path skipped cache (should be rare).
		frac := r.join.collarCoverFrac
		if frac <= 0 {
			frac = 0.18
		}
		cover = bodyTopStrip(bodyTex, frac)
	}
	if cover == nil {
		return
	}
	drawRigTexture(dst, cover, bodyWorld, alpha)
}

// bodyTopStrip returns a same-size copy of src with only the top frac kept
// opaque (soft feather at the strip bottom). Used to re-cover the neck join.
func bodyTopStrip(src *image.NRGBA, frac float64) *image.NRGBA {
	if src == nil {
		return nil
	}
	if frac < 0.05 {
		frac = 0.05
	}
	if frac > 0.40 {
		frac = 0.40
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	out := image.NewNRGBA(b)
	// Content top/bottom via raw alpha (once at load).
	pix := src.Pix
	stride := src.Stride
	top, bot := -1, -1
	for y := 0; y < h; y++ {
		row := y * stride
		for x := 0; x < w; x++ {
			if pix[row+x*4+3] > 16 {
				if top < 0 {
					top = y
				}
				bot = y
				break
			}
		}
	}
	if top < 0 || bot < top {
		return nil
	}
	contentH := bot - top + 1
	stripH := int(math.Round(float64(contentH) * frac))
	if stripH < 4 {
		stripH = 4
	}
	end := top + stripH
	if end > h {
		end = h
	}
	feather := stripH / 4
	if feather < 2 {
		feather = 2
	}
	outPix := out.Pix
	outStride := out.Stride
	for y := top; y < end; y++ {
		fade := 1.0
		if y > end-feather {
			fade = float64(end-y) / float64(feather)
		}
		si := y * stride
		di := y * outStride
		for x := 0; x < w; x++ {
			o := x * 4
			a := pix[si+o+3]
			if a == 0 {
				continue
			}
			outPix[di+o] = pix[si+o]
			outPix[di+o+1] = pix[si+o+1]
			outPix[di+o+2] = pix[si+o+2]
			outPix[di+o+3] = uint8(float64(a)*fade + 0.5)
		}
	}
	return out
}

func rigClipTime(clip *RigClip, elapsedMS int) int {
	if clip == nil || clip.DurationMS < 1 {
		return 0
	}
	if clip.Loop {
		elapsedMS %= clip.DurationMS
	}
	if elapsedMS < 0 {
		return 0
	}
	if elapsedMS > clip.DurationMS {
		return clip.DurationMS
	}
	return elapsedMS
}

func rigParentOrder(rig *Rig) ([]int, map[string]int, error) {
	if rig == nil {
		return nil, nil, fmt.Errorf("nil rig")
	}
	index := make(map[string]int, len(rig.Bones))
	for i, bone := range rig.Bones {
		index[bone.Name] = i
	}
	order := make([]int, 0, len(rig.Bones))
	added := make([]bool, len(rig.Bones))
	for len(order) < len(rig.Bones) {
		progress := false
		for i, bone := range rig.Bones {
			if added[i] || (bone.Parent != "" && !added[index[bone.Parent]]) {
				continue
			}
			added[i] = true
			order = append(order, i)
			progress = true
		}
		if !progress {
			return nil, nil, fmt.Errorf("invalid cyclic rig hierarchy")
		}
	}
	return order, index, nil
}

func (r *RigRenderer) clipForState(state PetRuntimeState) *RigClip {
	if r == nil {
		return nil
	}
	// Prefer explicit state name, then common v3 loop naming (listening_loop).
	st := string(state)
	for _, name := range []string{st, st + "_loop", st + "_in"} {
		if clip := r.clips[name]; clip != nil {
			return clip
		}
	}
	if name := bodyClipForState(r, state); name != "" {
		if clip := r.clips[name]; clip != nil {
			return clip
		}
	}
	if clip := r.clips["idle"]; clip != nil {
		return clip
	}
	return r.fallbackClip
}

// rigClips turns map values into stable pointers once at load time. This
// removes per-frame slice allocation, sorting, and value copying when a pack
// falls back from an unsupported state clip.
func rigClips(rig *Rig) (map[string]*RigClip, *RigClip) {
	clips := make(map[string]*RigClip, len(rig.Clips))
	keys := make([]string, 0, len(rig.Clips))
	for name, clip := range rig.Clips {
		copy := clip
		clips[name] = &copy
		keys = append(keys, name)
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return clips, nil
	}
	// Prefer a calm idle loop over alphabetically first clip (often alert_in).
	for _, prefer := range []string{
		"idle_breathe_loop",
		"idle_breathe",
		"idle",
		"idle_breathe_in",
	} {
		if c := clips[prefer]; c != nil {
			return clips, c
		}
	}
	return clips, clips[keys[0]]
}

func nonZero(v float64) float64 {
	if v == 0 {
		return 1
	}
	return v
}

func combineRigTransform(parent, local rigTransform) rigTransform {
	radians := parent.rotation * math.Pi / 180
	cos, sin := math.Cos(radians), math.Sin(radians)
	x := local.x * parent.sx
	y := local.y * parent.sy
	return rigTransform{x: parent.x + x*cos - y*sin, y: parent.y + x*sin + y*cos, rotation: parent.rotation + local.rotation, sx: parent.sx * local.sx, sy: parent.sy * local.sy}
}

func applyRigTrack(base rigTransform, frames []RigKeyframe, at int) rigTransform {
	if len(frames) == 0 {
		return base
	}
	if at <= frames[0].AtMS {
		first := frames[0]
		base.x += first.X
		base.y += first.Y
		base.rotation += first.Rotate
		base.sx *= nonZero(first.ScaleX)
		base.sy *= nonZero(first.ScaleY)
		return base
	}
	before, after := frames[0], frames[len(frames)-1]
	for i := 1; i < len(frames); i++ {
		if frames[i].AtMS >= at {
			before, after = frames[i-1], frames[i]
			break
		}
	}
	t := 0.0
	if after.AtMS > before.AtMS {
		t = float64(at-before.AtMS) / float64(after.AtMS-before.AtMS)
	}
	t = easeRig(t, after.Ease)
	base.x += lerp(before.X, after.X, t)
	base.y += lerp(before.Y, after.Y, t)
	base.rotation += lerp(before.Rotate, after.Rotate, t)
	base.sx *= nonZero(lerp(nonZero(before.ScaleX), nonZero(after.ScaleX), t))
	base.sy *= nonZero(lerp(nonZero(before.ScaleY), nonZero(after.ScaleY), t))
	return base
}

func trackAlpha(frames []RigKeyframe, at int) float64 {
	if len(frames) == 0 {
		return 1
	}
	if at <= frames[0].AtMS {
		if frames[0].Alpha != nil {
			return math.Max(0, math.Min(1, *frames[0].Alpha))
		}
		return 1
	}
	before, after := frames[0], frames[len(frames)-1]
	for i := 1; i < len(frames); i++ {
		if frames[i].AtMS >= at {
			before, after = frames[i-1], frames[i]
			break
		}
	}
	a, b := 1.0, 1.0
	if before.Alpha != nil {
		a = *before.Alpha
	}
	if after.Alpha != nil {
		b = *after.Alpha
	}
	t := 0.0
	if after.AtMS > before.AtMS {
		t = easeRig(float64(at-before.AtMS)/float64(after.AtMS-before.AtMS), after.Ease)
	}
	return math.Max(0, math.Min(1, lerp(a, b, t)))
}

func easeRig(t float64, mode string) float64 {
	t = math.Max(0, math.Min(1, t))
	switch strings.ToLower(mode) {
	case "ease-out":
		return 1 - math.Pow(1-t, 3)
	case "ease-in-out":
		return t * t * (3 - 2*t)
	default:
		return t
	}
}
func lerp(a, b, t float64) float64 { return a + (b-a)*t }

// copyToNRGBA preserves a texture's natural aspect ratio. Rig slots anchor to
// the centre of their source image, so coercing a 2:1 claw texture into a
// square changes the intended character silhouette before animation starts.
func copyToNRGBA(src image.Image) *image.NRGBA {
	if src == nil {
		return nil
	}
	bounds := src.Bounds()
	dst := image.NewNRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(dst, dst.Bounds(), src, bounds.Min, draw.Src)
	return dst
}

func drawRigTexture(dst, src *image.NRGBA, tr rigTransform, alpha float64) {
	if dst == nil || src == nil || alpha <= 0 {
		return
	}
	scale := float64(dst.Bounds().Dx()) / 256
	sw, sh := src.Bounds().Dx(), src.Bounds().Dy()
	cx, cy := float64(sw)/2, float64(sh)/2
	angle := tr.rotation * math.Pi / 180
	cos, sin := math.Cos(angle), math.Sin(angle)
	sx, sy := tr.sx*scale, tr.sy*scale
	if sx == 0 || sy == 0 {
		return
	}
	dx, dy := tr.x*scale, tr.y*scale

	// AABB of the four texture corners in destination space — multi-part packs
	// draw 8–11 slots; full-canvas inverse maps were ~9× more work than needed.
	minX, minY := math.MaxInt32, math.MaxInt32
	maxX, maxY := math.MinInt32, math.MinInt32
	for _, corner := range [4][2]float64{
		{0, 0},
		{float64(sw), 0},
		{0, float64(sh)},
		{float64(sw), float64(sh)},
	} {
		ux := (corner[0] - cx) * sx
		uy := (corner[1] - cy) * sy
		x := ux*cos - uy*sin + dx
		y := ux*sin + uy*cos + dy
		// pad 1px for floor sampling
		ix0, iy0 := int(math.Floor(x))-1, int(math.Floor(y))-1
		ix1, iy1 := int(math.Ceil(x))+1, int(math.Ceil(y))+1
		if ix0 < minX {
			minX = ix0
		}
		if iy0 < minY {
			minY = iy0
		}
		if ix1 > maxX {
			maxX = ix1
		}
		if iy1 > maxY {
			maxY = iy1
		}
	}
	dw, dh := dst.Bounds().Dx(), dst.Bounds().Dy()
	if minX < 0 {
		minX = 0
	}
	if minY < 0 {
		minY = 0
	}
	if maxX > dw {
		maxX = dw
	}
	if maxY > dh {
		maxY = dh
	}
	if minX >= maxX || minY >= maxY {
		return
	}

	srcPix := src.Pix
	srcStride := src.Stride
	dstPix := dst.Pix
	dstStride := dst.Stride
	for y := minY; y < maxY; y++ {
		for x := minX; x < maxX; x++ {
			px, py := float64(x)-dx, float64(y)-dy
			lx := (px*cos+py*sin)/sx + cx
			ly := (-px*sin+py*cos)/sy + cy
			ix, iy := int(math.Floor(lx)), int(math.Floor(ly))
			if ix < 0 || iy < 0 || ix >= sw || iy >= sh {
				continue
			}
			si := iy*srcStride + ix*4
			sa8 := srcPix[si+3]
			if sa8 == 0 {
				continue
			}
			// Premultiply slot alpha into source alpha (same as prior path).
			sa8 = uint8(float64(sa8) * alpha)
			if sa8 == 0 {
				continue
			}
			di := y*dstStride + x*4
			overRigPixelPix(dstPix, di, srcPix[si], srcPix[si+1], srcPix[si+2], sa8)
		}
	}
}

func overRigPixel(dst *image.NRGBA, x, y int, src color.NRGBA) {
	if dst == nil {
		return
	}
	overRigPixelPix(dst.Pix, dst.PixOffset(x, y), src.R, src.G, src.B, src.A)
}

// overRigPixelPix blends one straight-alpha NRGBA source over dst at byte offset i.
func overRigPixelPix(dstPix []uint8, i int, sr, sg, sb, sa8 uint8) {
	if sa8 == 0 {
		return
	}
	if sa8 == 255 {
		dstPix[i] = sr
		dstPix[i+1] = sg
		dstPix[i+2] = sb
		dstPix[i+3] = 255
		return
	}
	sa := float64(sa8) / 255
	da := float64(dstPix[i+3]) / 255
	oa := sa + da*(1-sa)
	if oa == 0 {
		return
	}
	inv := 1 / oa
	dstPix[i] = uint8(((float64(sr)/255)*sa + (float64(dstPix[i])/255)*da*(1-sa)) * inv * 255 + 0.5)
	dstPix[i+1] = uint8(((float64(sg)/255)*sa + (float64(dstPix[i+1])/255)*da*(1-sa)) * inv * 255 + 0.5)
	dstPix[i+2] = uint8(((float64(sb)/255)*sa + (float64(dstPix[i+2])/255)*da*(1-sa)) * inv * 255 + 0.5)
	dstPix[i+3] = uint8(oa*255 + 0.5)
}
