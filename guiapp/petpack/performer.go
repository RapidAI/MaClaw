package petpack

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Performer is the restricted v3 character-performance definition. It is a
// data-only director contract: no expressions, URLs, callbacks, or scripts.
type Performer struct {
	Version   int                          `json:"version"`
	Moods     []string                     `json:"moods"`
	Layers    []string                     `json:"layers"`
	Behaviors map[string]PerformerBehavior `json:"behaviors"`
	States    map[string]PerformerState    `json:"states"`
	Events    map[string]PerformerEvent    `json:"events"`
	Reactions map[string]PerformerReaction `json:"reactions"`
	Rules     PerformerRules               `json:"rules"`
}

type PerformerBehavior struct {
	Enter      string   `json:"enter"`
	Loop       string   `json:"loop"`
	Exit       string   `json:"exit"`
	Weight     int      `json:"weight"`
	MinMS      int      `json:"min_ms"`
	MaxMS      int      `json:"max_ms"`
	CooldownMS int      `json:"cooldown_ms"`
	Moods      []string `json:"moods,omitempty"`
}

type PerformerState struct {
	Enter        string   `json:"enter"`
	Loop         string   `json:"loop"`
	Exit         string   `json:"exit"`
	BehaviorPool []string `json:"behavior_pool,omitempty"`
	Mood         string   `json:"mood,omitempty"`
	Expression   string   `json:"expression,omitempty"`
	Gaze         string   `json:"gaze,omitempty"`
	Secondary    string   `json:"secondary,omitempty"`
}

type PerformerEvent struct {
	Play       string `json:"play"`
	Interrupt  string `json:"interrupt,omitempty"`
	Mood       string `json:"mood,omitempty"`
	CooldownMS int    `json:"cooldown_ms,omitempty"`
}

// PerformerReaction provides several weighted visual responses for one of the
// eight fixed runtime events, without expanding the event input surface.
type PerformerReaction struct {
	Event      string `json:"event"`
	Play       string `json:"play"`
	Interrupt  string `json:"interrupt,omitempty"`
	Mood       string `json:"mood,omitempty"`
	CooldownMS int    `json:"cooldown_ms,omitempty"`
	Weight     int    `json:"weight"`
}

type PerformerRules struct {
	NoRepeatLast   int `json:"no_repeat_last"`
	CrossfadeMS    int `json:"crossfade_ms"`
	MaxInterruptMS int `json:"max_interrupt_ms"`
}

// PerformanceStep is the director's safe, deterministic playback decision.
// It contains only clip IDs and timing; callers remain responsible for drawing.
type PerformanceStep struct {
	Body        string
	Loop        string
	Exit        string
	Interrupt   string
	Expression  string
	Gaze        string
	Secondary   string
	EntryMS     int
	DwellMS     int
	DurationMS  int
	CrossfadeMS int
	Mood        string
}

func (s PerformanceStep) isZero() bool { return s.Body == "" }

// PerformanceDirector selects v3 behaviors deterministically from a supplied
// clock and seed. It has no filesystem, screen, network, audio, or scripting
// access, so an authored pack cannot exceed the declarative contract.
type PerformanceDirector struct {
	performer     *Performer
	rig           *Rig
	seed          uint64
	lastBehaviors []string
	cooldowns     map[string]int64
	currentMood   string
}

func NewPerformanceDirector(performer *Performer, rig *Rig, seed uint64) *PerformanceDirector {
	return &PerformanceDirector{performer: performer, rig: rig, seed: seed, cooldowns: make(map[string]int64), currentMood: "calm"}
}

// SelectState returns entry/loop playback for a semantic runtime state.
func (d *PerformanceDirector) SelectState(state PetRuntimeState, nowMS int64) PerformanceStep {
	if d == nil || d.performer == nil {
		return PerformanceStep{}
	}
	s, ok := d.performer.States[string(state)]
	if !ok {
		s = d.performer.States["idle"]
	}
	if s.Mood != "" {
		d.currentMood = s.Mood
	} else if state == StateIdle {
		// Idle is the stable home state. Do not retain a transient reaction or
		// task mood indefinitely when the pack deliberately leaves its idle mood
		// implicit.
		d.currentMood = "calm"
	}
	if len(s.BehaviorPool) > 0 {
		return d.selectBehavior(s.BehaviorPool, nowMS, s.Expression, s.Gaze, s.Secondary)
	}
	entry := firstNonEmpty(s.Enter, s.Loop)
	return PerformanceStep{Body: entry, Loop: s.Loop, Exit: s.Exit, Expression: s.Expression, Gaze: s.Gaze, Secondary: s.Secondary, EntryMS: clipDuration(d.rig, entry), DurationMS: clipDuration(d.rig, entry), CrossfadeMS: d.performer.Rules.CrossfadeMS, Mood: d.currentMood}
}

// SelectEvent returns a short reaction. An unknown/cooling-down event returns
// an empty decision and leaves the current performance uninterrupted.
func (d *PerformanceDirector) SelectEvent(event string, nowMS int64) PerformanceStep {
	if d == nil || d.performer == nil || !allowedPerformerEvents[event] {
		return PerformanceStep{}
	}
	e, ok := d.performer.Events[event]
	if !ok || nowMS < d.cooldowns["event:"+event] {
		return PerformanceStep{}
	}
	// Pick a reaction variant for this event when declared. This keeps the host
	// event vocabulary fixed while avoiding one repeated response per event.
	type reactionChoice struct {
		name     string
		reaction PerformerReaction
	}
	choices, total := make([]reactionChoice, 0), 0
	for name, reaction := range d.performer.Reactions {
		if reaction.Event == event && nowMS >= d.cooldowns["reaction:"+reaction.Play] {
			choices, total = append(choices, reactionChoice{name: name, reaction: reaction}), total+reaction.Weight
		}
	}
	if total > 0 {
		// Map iteration is deliberately random in Go. Keep the weighted sequence
		// stable so a pack produces the same performance from the same seed/clock.
		sort.Slice(choices, func(i, j int) bool { return choices[i].name < choices[j].name })
		d.seed = d.seed*6364136223846793005 + uint64(nowMS) + 1442695040888963407
		pick := int(d.seed % uint64(total))
		for _, choice := range choices {
			reaction := choice.reaction
			if pick < reaction.Weight {
				e = PerformerEvent{Play: reaction.Play, Interrupt: reaction.Interrupt, Mood: reaction.Mood, CooldownMS: reaction.CooldownMS}
				d.cooldowns["reaction:"+reaction.Play] = nowMS + int64(reaction.CooldownMS)
				break
			}
			pick -= reaction.Weight
		}
	}
	if e.Mood != "" {
		d.currentMood = e.Mood
	}
	d.cooldowns["event:"+event] = nowMS + int64(e.CooldownMS)
	return PerformanceStep{Body: e.Play, Interrupt: e.Interrupt, DurationMS: clipDuration(d.rig, e.Play), CrossfadeMS: d.performer.Rules.CrossfadeMS, Mood: d.currentMood}
}

func (d *PerformanceDirector) selectBehavior(pool []string, nowMS int64, expression, gaze, secondary string) PerformanceStep {
	candidates := make([]string, 0, len(pool))
	weights := 0
	for _, name := range pool {
		behavior, ok := d.performer.Behaviors[name]
		if !ok || nowMS < d.cooldowns["behavior:"+name] || containsRecent(d.lastBehaviors, name) || (len(behavior.Moods) > 0 && !containsString(behavior.Moods, d.currentMood)) {
			continue
		}
		candidates = append(candidates, name)
		weights += behavior.Weight
	}
	if len(candidates) == 0 {
		for _, name := range pool {
			behavior, ok := d.performer.Behaviors[name]
			// Cooldown may be relaxed as a liveness fallback, but repeat avoidance
			// is a semantic quality guarantee. Validation requires the idle pool to
			// have more choices than no_repeat_last, so this always has a candidate.
			if ok && !containsRecent(d.lastBehaviors, name) && (len(behavior.Moods) == 0 || containsString(behavior.Moods, d.currentMood)) {
				candidates = append(candidates, name)
			}
		}
		weights = 0
		for _, name := range candidates {
			weights += d.performer.Behaviors[name].Weight
		}
	}
	if len(candidates) == 0 || weights < 1 {
		return PerformanceStep{}
	}
	d.seed = d.seed*6364136223846793005 + uint64(nowMS) + 1442695040888963407
	pick := int(d.seed % uint64(weights))
	name := candidates[0]
	for _, candidate := range candidates {
		weight := d.performer.Behaviors[candidate].Weight
		if pick < weight {
			name = candidate
			break
		}
		pick -= weight
	}
	b := d.performer.Behaviors[name]
	d.cooldowns["behavior:"+name] = nowMS + int64(b.CooldownMS)
	d.lastBehaviors = append(d.lastBehaviors, name)
	if n := d.performer.Rules.NoRepeatLast; len(d.lastBehaviors) > n {
		d.lastBehaviors = d.lastBehaviors[len(d.lastBehaviors)-n:]
	}
	span := b.MaxMS - b.MinMS
	duration := b.MinMS
	if span > 0 {
		duration += int(d.seed % uint64(span+1))
	}
	entry := firstNonEmpty(b.Enter, b.Loop)
	return PerformanceStep{Body: entry, Loop: b.Loop, Exit: b.Exit, Expression: expression, Gaze: gaze, Secondary: secondary, EntryMS: clipDuration(d.rig, entry), DwellMS: duration, DurationMS: clipDuration(d.rig, entry), CrossfadeMS: d.performer.Rules.CrossfadeMS, Mood: d.currentMood}
}

func containsRecent(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}
func containsString(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
func clipDuration(rig *Rig, name string) int {
	if rig == nil {
		return 0
	}
	return rig.Clips[name].DurationMS
}

const (
	minCharacterIdleBehaviors = 10
	// The runtime exposes eight deliberately narrow event types. Richer reaction
	// variety is authored as additional clips and state transitions, rather than
	// inventing new event inputs that would expand package privileges.
	minCharacterEvents      = 8
	minCharacterReactions   = 12
	minCharacterExpressions = 6
	minCharacterGazes       = 4
	maxPerformerBehaviors   = 64
	maxPerformerEvents      = 32
	maxPerformerStates      = 16
)

var allowedPerformerMoods = map[string]bool{
	"calm": true, "curious": true, "focused": true, "pleased": true, "concerned": true, "tired": true,
}
var allowedPerformerLayers = map[string]bool{"body": true, "expression": true, "gaze": true, "secondary": true}
var allowedPerformerStates = map[string]bool{
	"idle": true, "listening": true, "thinking": true, "speaking": true, "done": true, "alert": true, "quiet": true,
}
var allowedPerformerEvents = map[string]bool{
	"click": true, "hover": true, "drag_start": true, "drag_end": true, "task_started": true, "task_done": true, "task_failed": true, "long_idle": true,
}

// IsAllowedPerformerEvent reports whether a host event is part of the fixed
// v3 input contract. Hosts use this guard before forwarding an interaction to
// a pack, so authored definitions can never broaden their event privileges.
func IsAllowedPerformerEvent(event string) bool { return allowedPerformerEvents[event] }

// ParsePerformer decodes and structurally validates an authored v3 definition.
func ParsePerformer(raw []byte) (*Performer, error) {
	if len(raw) == 0 || len(raw) > maxRigTextureBytes {
		return nil, fmt.Errorf("character definition exceeds %d bytes", maxRigTextureBytes)
	}
	var p Performer
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("parse character definition: %w", err)
	}
	return &p, nil
}

// ValidateCharacterDefinition is the shared desktop/Pet Store validation
// entry point for a v3 performer JSON and its declared rig definition.
func ValidateCharacterDefinition(raw, rigRaw []byte, assets *PetPackRigAssets) (*Performer, error) {
	performer, err := ParsePerformer(raw)
	if err != nil {
		return nil, err
	}
	var rig Rig
	if err := json.Unmarshal(rigRaw, &rig); err != nil {
		return nil, fmt.Errorf("parse character rig definition: %w", err)
	}
	if err := ValidateRig(&rig, assets); err != nil {
		return nil, err
	}
	if err := ValidatePerformer(performer, &rig); err != nil {
		return nil, err
	}
	return performer, nil
}

// ValidatePerformer validates the exact public 3.0 contract and cross-checks
// every authored clip against the already restricted rig.
func ValidatePerformer(p *Performer, rig *Rig) error {
	if p == nil || p.Version != 1 {
		return fmt.Errorf("unsupported performer version")
	}
	if rig == nil || len(rig.Clips) == 0 {
		return fmt.Errorf("performer requires a rig with clips")
	}
	if err := validatePerformerNames(p.Moods, allowedPerformerMoods, "mood", 6); err != nil {
		return err
	}
	if err := validatePerformerNames(p.Layers, allowedPerformerLayers, "layer", 4); err != nil {
		return err
	}
	if len(p.Behaviors) < minCharacterIdleBehaviors || len(p.Behaviors) > maxPerformerBehaviors {
		return fmt.Errorf("performer requires %d-%d behaviors", minCharacterIdleBehaviors, maxPerformerBehaviors)
	}
	if len(p.Events) < minCharacterEvents || len(p.Events) > maxPerformerEvents {
		return fmt.Errorf("performer requires %d-%d events", minCharacterEvents, maxPerformerEvents)
	}
	if len(p.Reactions) < minCharacterReactions || len(p.Reactions) > maxPerformerBehaviors {
		return fmt.Errorf("performer requires %d-%d reactions", minCharacterReactions, maxPerformerBehaviors)
	}
	if len(p.States) != len(allowedPerformerStates) || len(p.States) > maxPerformerStates {
		return fmt.Errorf("performer requires all %d runtime states", len(allowedPerformerStates))
	}
	if p.Rules.NoRepeatLast < 3 || p.Rules.NoRepeatLast > 10 || p.Rules.CrossfadeMS < 80 || p.Rules.CrossfadeMS > 500 || p.Rules.MaxInterruptMS < 80 || p.Rules.MaxInterruptMS > 2000 {
		return fmt.Errorf("invalid performer rules")
	}
	for name, behavior := range p.Behaviors {
		if !validRigName(name) || behavior.Weight < 1 || behavior.MinMS < 250 || behavior.MaxMS < behavior.MinMS || behavior.MaxMS > 60000 || behavior.CooldownMS < 0 || behavior.CooldownMS > 600000 {
			return fmt.Errorf("invalid behavior %q", name)
		}
		if err := requireRigClips(rig, behavior.Enter, behavior.Loop, behavior.Exit); err != nil {
			return fmt.Errorf("behavior %q: %w", name, err)
		}
		for _, mood := range behavior.Moods {
			if !allowedPerformerMoods[mood] {
				return fmt.Errorf("behavior %q uses unsupported mood %q", name, mood)
			}
		}
	}
	expressions := make(map[string]bool, len(p.States))
	gazes := make(map[string]bool, len(p.States))
	for name, state := range p.States {
		if !allowedPerformerStates[name] {
			return fmt.Errorf("unsupported performer state %q", name)
		}
		if name == "idle" {
			if len(state.BehaviorPool) < minCharacterIdleBehaviors {
				return fmt.Errorf("idle state requires at least %d behaviors", minCharacterIdleBehaviors)
			}
			if len(state.BehaviorPool) <= p.Rules.NoRepeatLast {
				return fmt.Errorf("idle state requires more behaviors than no_repeat_last")
			}
			seenBehaviors := make(map[string]bool, len(state.BehaviorPool))
			for _, behavior := range state.BehaviorPool {
				if seenBehaviors[behavior] {
					return fmt.Errorf("idle state references duplicate behavior %q", behavior)
				}
				seenBehaviors[behavior] = true
				if _, ok := p.Behaviors[behavior]; !ok {
					return fmt.Errorf("idle state references unknown behavior %q", behavior)
				}
			}
			idleMood := state.Mood
			if idleMood == "" {
				idleMood = "calm"
			}
			viable := false
			for _, behaviorName := range state.BehaviorPool {
				behavior := p.Behaviors[behaviorName]
				if len(behavior.Moods) == 0 || containsString(behavior.Moods, idleMood) {
					viable = true
					break
				}
			}
			if !viable {
				return fmt.Errorf("idle state has no behavior for mood %q", idleMood)
			}
		} else if err := requireRigClips(rig, state.Enter, state.Loop, state.Exit); err != nil {
			return fmt.Errorf("state %q: %w", name, err)
		}
		if state.Mood != "" && !allowedPerformerMoods[state.Mood] {
			return fmt.Errorf("state %q uses unsupported mood", name)
		}
		if err := requireOptionalRigClips(rig, state.Expression, state.Gaze, state.Secondary); err != nil {
			return fmt.Errorf("state %q: %w", name, err)
		}
		if state.Expression != "" {
			expressions[state.Expression] = true
		}
		if state.Gaze != "" {
			gazes[state.Gaze] = true
		}
	}
	if len(expressions) < minCharacterExpressions {
		return fmt.Errorf("performer requires at least %d expression clips", minCharacterExpressions)
	}
	if len(gazes) < minCharacterGazes {
		return fmt.Errorf("performer requires at least %d gaze clips", minCharacterGazes)
	}
	for name, event := range p.Events {
		if !allowedPerformerEvents[name] || event.Play == "" || !validRigName(event.Play) || rig.Clips[event.Play].DurationMS < 1 {
			return fmt.Errorf("invalid event %q", name)
		}
		if event.Interrupt != "" && event.Interrupt != "soft" && event.Interrupt != "after_loop" {
			return fmt.Errorf("event %q has invalid interrupt mode", name)
		}
		if event.Mood != "" && !allowedPerformerMoods[event.Mood] {
			return fmt.Errorf("event %q uses unsupported mood", name)
		}
		if event.CooldownMS < 0 || event.CooldownMS > 600000 {
			return fmt.Errorf("event %q has invalid cooldown", name)
		}
	}
	for name, reaction := range p.Reactions {
		if !validRigName(name) || !allowedPerformerEvents[reaction.Event] || reaction.Play == "" || !validRigName(reaction.Play) || rig.Clips[reaction.Play].DurationMS < 1 || reaction.Weight < 1 {
			return fmt.Errorf("invalid reaction %q", name)
		}
		if reaction.Interrupt != "" && reaction.Interrupt != "soft" && reaction.Interrupt != "after_loop" {
			return fmt.Errorf("reaction %q has invalid interrupt mode", name)
		}
		if reaction.Mood != "" && !allowedPerformerMoods[reaction.Mood] {
			return fmt.Errorf("reaction %q uses unsupported mood", name)
		}
		if reaction.CooldownMS < 0 || reaction.CooldownMS > 600000 {
			return fmt.Errorf("reaction %q has invalid cooldown", name)
		}
	}
	return nil
}

func validatePerformerNames(values []string, allowed map[string]bool, label string, want int) error {
	if len(values) != want {
		return fmt.Errorf("performer requires %d %ss", want, label)
	}
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if !allowed[value] || seen[value] {
			return fmt.Errorf("invalid or duplicate %s %q", label, value)
		}
		seen[value] = true
	}
	return nil
}

func requireRigClips(rig *Rig, names ...string) error {
	for _, name := range names {
		if !validRigName(name) || rig.Clips[name].DurationMS < 1 {
			return fmt.Errorf("references missing rig clip %q", name)
		}
	}
	return nil
}

func requireOptionalRigClips(rig *Rig, names ...string) error {
	for _, name := range names {
		if name != "" {
			if err := requireRigClips(rig, name); err != nil {
				return err
			}
		}
	}
	return nil
}

// PerformerClipNames returns a sorted stable set of clips referenced by a
// performer. It is useful for diagnostics and lets the renderer preload only
// declared behavior.
func PerformerClipNames(p *Performer) []string {
	if p == nil {
		return nil
	}
	seen := map[string]bool{}
	add := func(name string) {
		if name != "" {
			seen[name] = true
		}
	}
	for _, behavior := range p.Behaviors {
		add(behavior.Enter)
		add(behavior.Loop)
		add(behavior.Exit)
	}
	for _, state := range p.States {
		add(state.Enter)
		add(state.Loop)
		add(state.Exit)
		add(state.Expression)
		add(state.Gaze)
		add(state.Secondary)
	}
	for _, event := range p.Events {
		add(event.Play)
	}
	for _, reaction := range p.Reactions {
		add(reaction.Play)
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func (p *Performer) HasState(state PetRuntimeState) bool { _, ok := p.States[string(state)]; return ok }

func (p *Performer) String() string { return strings.Join(PerformerClipNames(p), ",") }
