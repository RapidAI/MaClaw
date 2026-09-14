package agent

import "strings"

func loadInitialSessionFacts(cb LoopCallbacks) *SessionFactOverlay {
	if holder, ok := cb.(SessionFactHolder); ok {
		return CloneSessionFactOverlay(holder.LoadSessionFacts())
	}
	return nil
}

func finishSessionFacts(cb LoopCallbacks, r *LoopResult, overlay *SessionFactOverlay) {
	if overlay != nil {
		if needles := MemoryRetractionNeedles(loadInitialMemoryRetractions(cb)); len(needles) > 0 {
			DropSessionFactsMatching(overlay, needles)
		}
	}
	if r != nil && r.SessionFacts == nil {
		r.SessionFacts = CloneSessionFactOverlay(overlay)
	}
	holder, ok := cb.(SessionFactHolder)
	if !ok {
		return
	}
	if overlay == nil || overlay.Len() == 0 {
		holder.SaveSessionFacts(nil)
		return
	}
	holder.SaveSessionFacts(CloneSessionFactOverlay(overlay))
}

func noteSessionFactFromTool(overlay *SessionFactOverlay, name, argsJSON, result string, outcome ToolExecutionOutcome) (*SessionFactOverlay, SessionFact, bool) {
	fact, ok := ExtractSessionFactFromTool(name, argsJSON, result, outcome)
	if !ok {
		return overlay, SessionFact{}, false
	}
	overlay = EnsureSessionFactOverlay(overlay)
	prevClaim := ""
	existed := false
	if prev, found := LookupSessionFact(overlay, fact.Entity); found {
		existed = true
		prevClaim = prev.Claim
	}
	if !AdmitSessionFact(overlay, fact) {
		return overlay, SessionFact{}, false
	}
	admitted, found := LookupSessionFact(overlay, fact.Entity)
	if !found {
		admitted = fact
	}
	polarity := DetectSessionFactPolarity(admitted.Claim)
	for _, alias := range admitted.Aliases {
		alias = strings.TrimSpace(alias)
		if alias == "" || alias == admitted.Entity {
			continue
		}
		AdmitSessionFact(overlay, SessionFact{
			Entity:    alias,
			Predicate: admitted.Predicate,
			Claim:     sessionFactClaimFor(alias, polarity),
			Evidence:  admitted.Evidence,
		})
	}
	// Evidence-only refreshes stay in the overlay; they must not rewrite stores.
	writeback := !existed || prevClaim != admitted.Claim
	return overlay, admitted, writeback
}

func notifyVerifiedSessionFact(cb LoopCallbacks, fact SessionFact) {
	if cb == nil || strings.TrimSpace(fact.Claim) == "" {
		return
	}
	sink, ok := cb.(VerifiedFactSink)
	if !ok {
		return
	}
	sink.OnVerifiedSessionFact(fact)
}

func sanitizeSessionFactsVisible(r *LoopResult) {
	if r == nil {
		return
	}
	r.Text = StripSessionFactsFromVisible(r.Text)
	for i := range r.HistoryDelta {
		if strings.EqualFold(strings.TrimSpace(r.HistoryDelta[i].Role), "tool") {
			continue
		}
		if s, ok := r.HistoryDelta[i].Content.(string); ok {
			r.HistoryDelta[i].Content = StripSessionFactsFromVisible(s)
		}
		r.HistoryDelta[i].ReasoningContent = StripSessionFactsFromVisible(r.HistoryDelta[i].ReasoningContent)
	}
}
