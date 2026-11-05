package hopjit

func normalizeSnapshotMaps(snapshot *EngineSnapshot) {
	if snapshot.StepStates == nil {
		snapshot.StepStates = map[string]StepStatus{}
	}
	if snapshot.Variables == nil {
		snapshot.Variables = map[string]VariableValue{}
	}
	if snapshot.LoopCounters == nil {
		snapshot.LoopCounters = map[string]int{}
	}
	if snapshot.LoopItems == nil {
		snapshot.LoopItems = map[string][]any{}
	}
	if snapshot.LoopResults == nil {
		snapshot.LoopResults = map[string]map[string]any{}
	}
	if snapshot.Calls == nil {
		snapshot.Calls = map[string]CallState{}
	}
	if snapshot.ResolvedSpecs == nil {
		snapshot.ResolvedSpecs = map[string]string{}
	}
	if snapshot.Inflight == nil {
		snapshot.Inflight = map[string]InflightCall{}
	}
	if snapshot.RetryCounters == nil {
		snapshot.RetryCounters = map[string]int{}
	}
	if snapshot.RetryHistory == nil {
		snapshot.RetryHistory = map[string][]RetryRecord{}
	}
	if snapshot.ReplanCounters == nil {
		snapshot.ReplanCounters = map[string]int{}
	}
	if snapshot.ReplanDigests == nil {
		snapshot.ReplanDigests = map[string]string{}
	}
	if snapshot.CommittedSteps == nil {
		snapshot.CommittedSteps = map[string]bool{}
	}
	if snapshot.OnFailActive == nil {
		snapshot.OnFailActive = map[string]bool{}
	}
	if snapshot.Failures == nil {
		snapshot.Failures = map[string]Failure{}
	}
}
