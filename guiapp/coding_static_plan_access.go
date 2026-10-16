package guiapp

import "sync"

// codingStaticShadowPlanMu protects publication and detachment of the
// host-owned static shadow-plan pointer.  CodingSubAgent values are copied for
// nested children (child := *s), so embedding a mutex in CodingSubAgent would
// copy lock state and make the copy unsafe.  A package-level lock keeps the
// pointer publication fence independent from that copy operation.
//
// A published codingStaticPlanPreparation is immutable.  Callers may retain
// the returned pointer after the read lock is released, but must publish a new
// preparation object instead of mutating Plan in place.
var codingStaticShadowPlanMu sync.RWMutex

func codingStaticShadowPlanOf(s *CodingSubAgent) *codingStaticPlanPreparation {
	if s == nil {
		return nil
	}
	codingStaticShadowPlanMu.RLock()
	plan := s.staticShadowPlan
	codingStaticShadowPlanMu.RUnlock()
	return plan
}

func setCodingStaticShadowPlan(s *CodingSubAgent, plan *codingStaticPlanPreparation) {
	if s == nil {
		return
	}
	codingStaticShadowPlanMu.Lock()
	s.staticShadowPlan = plan
	codingStaticShadowPlanMu.Unlock()
}
