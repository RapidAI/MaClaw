package longhorizon

func ClampMaxRounds(n int) int {
	if n <= 0 {
		return DefaultMaxRounds
	}
	if n > MaxRounds {
		return MaxRounds
	}
	return n
}

func LatestRealAudit(state *TaskState) *AuditReport {
	if state == nil {
		return nil
	}
	for i := len(state.Rounds) - 1; i >= 0; i-- {
		audit := state.Rounds[i].Audit
		if audit == nil || audit.Synthetic {
			continue
		}
		return audit
	}
	return nil
}

func CanComplete(state *TaskState) bool {
	if state == nil || state.ManagerNext != NextDone {
		return false
	}
	audit := LatestRealAudit(state)
	if audit == nil {
		return false
	}
	return audit.Status == "complete" && audit.Integrity == "clean" && audit.Alignment == "aligned"
}

func MarkCompleted(state *TaskState) bool {
	if !CanComplete(state) {
		if state != nil {
			state.Completed = false
		}
		return false
	}
	state.Completed = true
	state.Status = StatusDone
	return true
}

func Resumable(state *TaskState) bool {
	if state == nil || state.Completed {
		return false
	}
	switch state.Status {
	case StatusDone, StatusCancelled:
		return false
	}
	return state.RoundIndex < ClampMaxRounds(state.MaxRounds)
}

func CloneTaskState(state *TaskState) *TaskState {
	if state == nil {
		return nil
	}
	cp := *state
	if len(state.Rounds) > 0 {
		cp.Rounds = make([]ManagedRound, len(state.Rounds))
		copy(cp.Rounds, state.Rounds)
		for i := range cp.Rounds {
			if state.Rounds[i].Audit != nil {
				audit := *state.Rounds[i].Audit
				cp.Rounds[i].Audit = &audit
			}
		}
	}
	if len(state.Carryover) > 0 {
		cp.Carryover = append([]string(nil), state.Carryover...)
	}
	return &cp
}
