package experience

import (
	"fmt"
	"sort"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
)

const similarTriggerThreshold = defaultSimilarTriggerThreshold

func matchExistingSkill(candidate corelib.NLSkillEntry, existing []corelib.NLSkillEntry, threshold float64) (corelib.NLSkillEntry, bool) {
	// A name is the on-disk identity, so a name collision is the same skill
	// whatever pool it sits in.
	for _, current := range existing {
		if current.Name == candidate.Name {
			return current, true
		}
	}
	// Merely looking alike is not enough to merge across experience pools: the
	// match renames the candidate onto the existing skill, so a coding recipe
	// folded into a general one would overwrite the general recipe and leave
	// coding with nothing. Skills learned in separate pools stay separate.
	for _, current := range existing {
		if !corelib.SkillVisibleInExperienceDomain(current.ExperienceDomain, candidate.ExperienceDomain) {
			continue
		}
		if similarSkillThreshold(candidate, current, threshold) {
			return current, true
		}
	}
	return corelib.NLSkillEntry{}, false
}

func similarSkill(a, b corelib.NLSkillEntry) bool {
	return similarSkillThreshold(a, b, similarTriggerThreshold)
}

func similarSkillThreshold(a, b corelib.NLSkillEntry, threshold float64) bool {
	if threshold <= 0 || threshold > 1 {
		threshold = similarTriggerThreshold
	}
	if a.Name == "" || b.Name == "" || len(a.Steps) == 0 || len(b.Steps) == 0 {
		return false
	}
	if stepShapeSignature(a.Steps) != stepShapeSignature(b.Steps) {
		return false
	}
	return triggerOverlap(a.Triggers, b.Triggers) >= threshold
}

func preserveExistingSkillIdentity(candidate, existing corelib.NLSkillEntry) corelib.NLSkillEntry {
	candidate.Name = existing.Name
	candidate.DirName = existing.DirName
	candidate.CreatedAt = existing.CreatedAt
	candidate.UsageCount = existing.UsageCount
	candidate.SuccessCount = existing.SuccessCount
	candidate.FailureCount = existing.FailureCount
	candidate.WorkaroundCount = existing.WorkaroundCount
	candidate.LastUsedAt = existing.LastUsedAt
	candidate.LastError = existing.LastError
	candidate.RepairAttemptCount = existing.RepairAttemptCount
	candidate.LastRepairAt = existing.LastRepairAt
	candidate.RepairHistory = existing.RepairHistory
	candidate.HubSkillID = existing.HubSkillID
	candidate.HubVersion = existing.HubVersion
	if candidate.SourceProject == "" {
		candidate.SourceProject = existing.SourceProject
	}
	// Refining a recipe must not move it between experience pools: what the
	// skill is for was decided when it was first learned, and a candidate
	// always carries the pool of the session that produced it, so letting the
	// candidate win would silently delete the recipe from its original pool.
	//
	// An empty pool is ambiguous, and the two meanings need opposite handling:
	// a skill the user deliberately installed is universal on purpose and must
	// stay visible everywhere, while a self-learned skill from before pools
	// existed is merely unstamped and adopts the candidate's pool.
	switch {
	case existing.ExperienceDomain != "":
		candidate.ExperienceDomain = existing.ExperienceDomain
	case !corelib.IsLearnedSource(existing.Source):
		candidate.ExperienceDomain = corelib.SkillDomainUniversal
	}
	return candidate
}

func replaceExistingSkill(skills []corelib.NLSkillEntry, updated corelib.NLSkillEntry) []corelib.NLSkillEntry {
	for i := range skills {
		if skills[i].Name == updated.Name {
			skills[i] = updated
			return skills
		}
	}
	return append(skills, updated)
}

func stepShapeSignature(steps []corelib.NLSkillStep) string {
	parts := make([]string, 0, len(steps))
	for _, step := range steps {
		parts = append(parts, stepSignature(step))
	}
	return strings.Join(parts, "|")
}

func stepSignature(step corelib.NLSkillStep) string {
	keys := make([]string, 0, len(step.Params))
	for key := range step.Params {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := []string{strings.TrimSpace(step.Action)}
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", key, paramShape(key, step.Params[key])))
	}
	return strings.Join(parts, ";")
}

func paramShape(key string, value interface{}) string {
	switch v := value.(type) {
	case string:
		return stringParamShape(key, v)
	case []interface{}:
		return "list"
	case map[string]interface{}:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return "map:" + strings.Join(keys, ",")
	default:
		return fmt.Sprintf("%T", value)
	}
}

func stringParamShape(key, value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return "empty"
	}
	if strings.EqualFold(key, "command") {
		return "command:" + structuralCommandShape(value)
	}
	if strings.Contains(value, "{{") {
		return "template"
	}
	return "literal"
}

func structuralCommandShape(command string) string {
	segments := commandSignatures(command)
	if len(segments) == 0 {
		return "empty"
	}
	return strings.Join(segments, "+")
}

func triggerOverlap(a, b []string) float64 {
	setA := normalizedStringSet(a)
	setB := normalizedStringSet(b)
	if len(setA) == 0 || len(setB) == 0 {
		return 0
	}
	intersection := 0
	for item := range setA {
		if setB[item] {
			intersection++
		}
	}
	if intersection == 0 {
		return 0
	}
	union := len(setA) + len(setB) - intersection
	return float64(intersection) / float64(union)
}

func normalizedStringSet(items []string) map[string]bool {
	set := make(map[string]bool, len(items))
	for _, item := range items {
		item = strings.ToLower(strings.TrimSpace(item))
		if item != "" {
			set[item] = true
		}
	}
	return set
}
