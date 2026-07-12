package skill

import (
	"github.com/RapidAI/CodeClaw/corelib"
)

// CloneNLSkillEntry returns a deep-enough copy of entry for async pipelines
// (evolution, self-repair). Nested maps/slices that mutation-prone code may
// rewrite are cloned so the main agent loop cannot race with background workers.
func CloneNLSkillEntry(entry *corelib.NLSkillEntry) *corelib.NLSkillEntry {
	if entry == nil {
		return nil
	}
	cp := *entry
	cp.Triggers = entryCloneStrings(entry.Triggers)
	cp.Capabilities = entryCloneStrings(entry.Capabilities)
	cp.RequiresTools = entryCloneStrings(entry.RequiresTools)
	cp.FallbackForTools = entryCloneStrings(entry.FallbackForTools)
	cp.RequiresToolsets = entryCloneStrings(entry.RequiresToolsets)
	cp.FallbackForToolsets = entryCloneStrings(entry.FallbackForToolsets)
	cp.RequiredCredentialFiles = entryCloneStrings(entry.RequiredCredentialFiles)
	cp.RequiresPython = entryCloneStrings(entry.RequiresPython)
	cp.RequiresNode = entryCloneStrings(entry.RequiresNode)
	cp.RequiresBins = entryCloneStrings(entry.RequiresBins)
	cp.Platforms = entryCloneStrings(entry.Platforms)
	cp.RequiredArgs = entryCloneStrings(entry.RequiredArgs)
	cp.RequiredEnv = entryCloneStrings(entry.RequiredEnv)
	cp.Steps = CloneNLSkillSteps(entry.Steps)
	cp.Params = entryCloneParams(entry.Params)
	cp.Operations = entryCloneOperations(entry.Operations)
	cp.RepairHistory = entryCloneRepairHistory(entry.RepairHistory)
	cp.SolidificationCandidates = entryCloneSolidification(entry.SolidificationCandidates)
	cp.Pipeline = entryClonePipeline(entry.Pipeline)
	cp.References = entryCloneReferences(entry.References)
	if entry.Capability != nil {
		cap := *entry.Capability
		cp.Capability = &cap
	}
	return &cp
}

// CloneNLSkillSteps deep-copies steps including Params maps and nested fallbacks.
func CloneNLSkillSteps(steps []corelib.NLSkillStep) []corelib.NLSkillStep {
	if len(steps) == 0 {
		return nil
	}
	out := make([]corelib.NLSkillStep, len(steps))
	for i := range steps {
		out[i] = cloneNLSkillStep(steps[i])
	}
	return out
}

func cloneNLSkillStep(s corelib.NLSkillStep) corelib.NLSkillStep {
	cp := s
	cp.Params = entryCloneAnyMap(s.Params)
	cp.Capture = entryCloneStringMap(s.Capture)
	if s.Poll != nil {
		p := *s.Poll
		cp.Poll = &p
	}
	if s.Loop != nil {
		l := *s.Loop
		cp.Loop = &l
	}
	if s.FallbackStep != nil {
		fb := cloneNLSkillStep(*s.FallbackStep)
		cp.FallbackStep = &fb
	}
	return cp
}

func entryCloneStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

func entryCloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func entryCloneAnyMap(in map[string]interface{}) map[string]interface{} {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		out[k] = entryCloneAny(v)
	}
	return out
}

func entryCloneAny(v interface{}) interface{} {
	switch t := v.(type) {
	case map[string]interface{}:
		return entryCloneAnyMap(t)
	case []interface{}:
		out := make([]interface{}, len(t))
		for i := range t {
			out[i] = entryCloneAny(t[i])
		}
		return out
	case []string:
		return entryCloneStrings(t)
	default:
		return v
	}
}

func entryCloneParams(in []corelib.NLSkillParam) []corelib.NLSkillParam {
	if len(in) == 0 {
		return nil
	}
	out := make([]corelib.NLSkillParam, len(in))
	for i, p := range in {
		out[i] = p
		out[i].Aliases = entryCloneStrings(p.Aliases)
	}
	return out
}

func entryCloneOperations(in []corelib.NLSkillOperation) []corelib.NLSkillOperation {
	if len(in) == 0 {
		return nil
	}
	out := make([]corelib.NLSkillOperation, len(in))
	for i, op := range in {
		out[i] = op
		out[i].Params = entryCloneStrings(op.Params)
		out[i].Labels = entryCloneStrings(op.Labels)
	}
	return out
}

func entryCloneRepairHistory(in []corelib.SkillRepairRecord) []corelib.SkillRepairRecord {
	if len(in) == 0 {
		return nil
	}
	out := make([]corelib.SkillRepairRecord, len(in))
	copy(out, in)
	return out
}

func entryCloneSolidification(in []corelib.SolidificationCandidate) []corelib.SolidificationCandidate {
	if len(in) == 0 {
		return nil
	}
	out := make([]corelib.SolidificationCandidate, len(in))
	for i, c := range in {
		out[i] = c
		out[i].ParamSlots = entryCloneStrings(c.ParamSlots)
	}
	return out
}

func entryClonePipeline(in []corelib.SkillPipelineStep) []corelib.SkillPipelineStep {
	if len(in) == 0 {
		return nil
	}
	out := make([]corelib.SkillPipelineStep, len(in))
	copy(out, in)
	return out
}

func entryCloneReferences(in []corelib.SkillReference) []corelib.SkillReference {
	if len(in) == 0 {
		return nil
	}
	out := make([]corelib.SkillReference, len(in))
	copy(out, in)
	return out
}
