package inputcorrect

import "context"

// The correction loop reads the isolated desktop, compares the identified
// field with the expected text, and only then types. A layout, frame, or
// ref change throws away the plan and reads again. An upload that is still
// pending is not success. There is no fixed sleep and no foreground lock.
// Send, publish, and pay are not actions of this loop.

const (
	// MaxActs is the number of corrective typings in one call.
	MaxActs = 3
	// MaxObserves bounds reobservation after a stale frame, ref, or layout.
	MaxObserves = 8
)

// Run decides and, through relay, performs a bounded correction.
// store may be nil. A skipped outcome means the relay could not observe;
// the caller keeps its previous read-back.
func Run(ctx context.Context, req Request, relay Relay, store *Store) Outcome {
	if ctx != nil && ctx.Err() != nil {
		return Outcome{Status: StatusUnverified, Reason: "cancelled"}
	}
	if ExternalEffect(req.Effect) {
		return Outcome{Status: StatusRefused, Reason: DecideEffect(req.Effect, true, Procedure{}).Reason}
	}
	if req.Secret {
		return Outcome{Status: StatusUnverified, Reason: "secret_not_read"}
	}
	if _, kind := CompareExact(req.Expected, ""); kind == EmptyExpected {
		return Outcome{Status: StatusUnverified, Reason: "empty_expected"}
	}
	if relay == nil {
		return Outcome{Status: StatusSkipped, Reason: "no_relay"}
	}

	var (
		seen        Observation
		have        bool
		ready       bool
		planned     string
		plannedMemo bool
		acts        int
		last        string
		lastMemo    bool
		armedImages = -1
		busy        bool
		failed      = map[string]bool{}
	)
	finishFail := func(reason, mismatch string) Outcome {
		if lastMemo && last != "" && store != nil {
			store.Demote(last, conditions(req, seen))
		}
		return Outcome{
			Status: StatusFailed, Escalated: true, Strategy: last, Mismatch: mismatch,
			Attempts: acts, Reason: reason, Images: seen.ImageCount, EditorSettled: editorReady(seen),
		}
	}

	for n := 0; n < MaxObserves; n++ {
		if ctx != nil && ctx.Err() != nil {
			return Outcome{Status: StatusUnverified, Reason: "cancelled", Attempts: acts}
		}
		obs, err := relay.Observe(ctx)
		if err != nil || obs.Unavailable {
			if n == 0 && acts == 0 {
				return Outcome{Status: StatusSkipped, Reason: "observation_unavailable"}
			}
			return Outcome{Status: StatusUnverified, Reason: "observation_lost", Attempts: acts}
		}
		if obs.Secret {
			return Outcome{Status: StatusUnverified, Reason: "secret_not_read", Attempts: acts}
		}
		if obs.ReadbackDenied || !obs.TextTrusted || obs.Clipped {
			return Outcome{Status: StatusUnverified, Reason: "readback_denied", Attempts: acts}
		}
		if obs.EdgeStripped && HasEdgeSpace(req.Expected) {
			return Outcome{Status: StatusUnverified, Reason: "readback_denied", Attempts: acts}
		}
		if wrongElement(req, obs) || !obs.Identified {
			ready = false
			have = false
			if n >= 1 && acts == 0 {
				return finishFail("wrong_element", "")
			}
			continue
		}
		if frameMismatch(req, obs) {
			ready = false
			have = false
			if n >= 1 && acts == 0 {
				return finishFail("stale_target", "")
			}
			continue
		}
		if have && stale(seen, obs) {
			// The plan belonged to the previous frame, ref, or layout.
			ready = false
			seen = obs
			have = true
			continue
		}
		seen = obs
		have = true
		if obs.UploadPending || obs.EditorBusy || (req.MinImages > 0 && obs.ImageCount < req.MinImages) {
			ready = false
			busy = true
			continue
		}
		busy = false
		if armedImages >= 0 && obs.ImageCount < armedImages {
			if lastMemo && store != nil {
				store.Demote(last, conditions(req, obs))
			}
			return Outcome{Status: StatusFailed, Escalated: true, Strategy: last, Attempts: acts, Reason: "preserved_content_lost", Images: obs.ImageCount}
		}

		ok, kind := CompareExact(req.Expected, obs.FieldText)
		if ok {
			strat := last
			if acts == 0 {
				strat = req.UsedStrategy
			}
			if strat != "" && store != nil {
				store.Confirm(strat, conditions(req, obs))
			}
			return Outcome{
				Status: StatusVerified, InputVerified: true, Strategy: strat, Attempts: acts,
				Reason: "exact_readback", Images: obs.ImageCount, EditorSettled: true,
			}
		}
		if last != "" && !failed[last] {
			if lastMemo && store != nil {
				store.Demote(last, conditions(req, seen))
			}
			failed[last] = true
			lastMemo = false
		}
		if acts >= MaxActs {
			return finishFail("bounded_failure", kind)
		}
		if !ready {
			strat, mem := choose(obs, failed, store, conditions(req, obs))
			if strat == "" {
				return finishFail("no_strategy", kind)
			}
			planned, plannedMemo, ready = strat, mem, true
			continue
		}
		if obs.ImageCount > 0 && planned != StrategyEditorReplace {
			failed[planned] = true
			ready = false
			continue
		}
		if (planned == StrategyClipboard || planned == StrategySegmented) && obs.ImageCount == 0 {
			if err := relay.Act(ctx, Action{Kind: ActionClearField, ElementID: obs.ElementID}); err != nil {
				failed[planned] = true
				ready = false
				acts++
				last = planned
				lastMemo = plannedMemo
				continue
			}
		}
		if !AllowedKind(planned) {
			return finishFail("strategy_rejected", kind)
		}
		if err := relay.Act(ctx, Action{Kind: planned, Text: req.Expected, ElementID: obs.ElementID}); err != nil {
			if plannedMemo && store != nil {
				store.Demote(planned, conditions(req, obs))
			}
			failed[planned] = true
			ready = false
			acts++
			last = planned
			lastMemo = false
			continue
		}
		acts++
		last = planned
		lastMemo = plannedMemo
		ready = false
		if obs.ImageCount > 0 {
			armedImages = obs.ImageCount
		}
	}
	if busy {
		return Outcome{Status: StatusUnverified, Reason: "editor_unsettled", Attempts: acts, Images: seen.ImageCount}
	}
	return finishFail("bounded_failure", "")
}

func wrongElement(req Request, obs Observation) bool {
	want := stringTrim(req.ElementKey)
	got := stringTrim(obs.ElementKey)
	return want != "" && got != "" && want != got
}

func frameMismatch(req Request, obs Observation) bool {
	want := stringTrim(req.FrameID)
	got := stringTrim(obs.FrameID)
	return want != "" && got != "" && want != got
}

func stale(prev, obs Observation) bool {
	if prev.FrameID != obs.FrameID || prev.RefEpoch != obs.RefEpoch || prev.LayoutScale != obs.LayoutScale {
		return true
	}
	return prev.ElementKey != "" && obs.ElementKey != "" && prev.ElementKey != obs.ElementKey
}

func editorReady(obs Observation) bool {
	return !obs.UploadPending && !obs.EditorBusy
}

func choose(obs Observation, failed map[string]bool, store *Store, cond Conditions) (string, bool) {
	if store != nil {
		if p, ok := store.Best(cond); ok && !failed[p.Strategy] && capabilityAllows(obs, p.Strategy) {
			return p.Strategy, true
		}
	}
	var order []string
	if obs.ImageCount > 0 {
		if obs.Capability.EditorReplace {
			order = append(order, StrategyEditorReplace)
		}
	} else {
		if obs.Capability.Semantic && obs.ElementID > 0 {
			order = append(order, StrategySemantic)
		}
		if obs.Capability.Clipboard {
			order = append(order, StrategyClipboard)
		}
		if obs.Capability.Keys {
			order = append(order, StrategySegmented)
		}
	}
	for _, strat := range order {
		if !failed[strat] && !demoted(store, strat, cond) {
			return strat, false
		}
	}
	return "", false
}

func capabilityAllows(obs Observation, strategy string) bool {
	switch strategy {
	case StrategySemantic:
		return obs.Capability.Semantic && obs.ElementID > 0 && obs.ImageCount == 0
	case StrategyClipboard:
		return obs.Capability.Clipboard && obs.ImageCount == 0
	case StrategySegmented:
		return obs.Capability.Keys && obs.ImageCount == 0
	case StrategyEditorReplace:
		return obs.Capability.EditorReplace && obs.ImageCount > 0
	default:
		return false
	}
}

func demoted(store *Store, strategy string, cond Conditions) bool {
	if store == nil {
		return false
	}
	return store.IsDemoted(strategy, cond)
}

func conditions(req Request, obs Observation) Conditions {
	class := FieldPlain
	if obs.ImageCount > 0 {
		class = FieldRich
	}
	return Conditions{
		Environment:   stringTrim(req.Environment),
		FieldClass:    class,
		LayoutScale:   obs.LayoutScale,
		Semantic:      obs.Capability.Semantic,
		Clipboard:     obs.Capability.Clipboard,
		Keys:          obs.Capability.Keys,
		EditorReplace: obs.Capability.EditorReplace,
	}
}
