package skill

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

// RepairDraftsDirName is the per-skill directory holding pending repair
// drafts: <skill_dir>/.evolution-drafts/.
const RepairDraftsDirName = ".evolution-drafts"

// RepairDraft is a pending, human-reviewed repair proposal for a file-backed
// skill. The pipeline writes it as JSON under
// <skill_dir>/.evolution-drafts/<utc-timestamp>.json; the GUI lists, applies
// (写回 skill.yaml + config) or rejects it. The pipeline never applies drafts
// itself and never mutates the skill entry on this path.
type RepairDraft struct {
	Skill       string                `json:"skill"`
	OldSteps    []corelib.NLSkillStep `json:"old_steps"`
	NewSteps    []corelib.NLSkillStep `json:"new_steps"`
	Explanation string                `json:"explanation"`
	LastError   string                `json:"last_error,omitempty"`
	CreatedAt   string                `json:"created_at"` // RFC3339 UTC
	// Disable marks a "disable suggestion" draft (LLM returned
	// repaired:false + should_disable:true): OldSteps/NewSteps are empty and
	// Explanation carries the disable rationale. Applying it sets the skill
	// status to "disabled" instead of writing steps back.
	Disable bool `json:"disable,omitempty"`
}

// statRepairDraftPath is indirected so tests can simulate non-IsNotExist
// stat failures in the collision loop below.
var statRepairDraftPath = os.Stat

// WriteRepairDraft persists draft under <skillDir>/.evolution-drafts/ and
// returns the draft file name (not the full path) for event payloads.
func WriteRepairDraft(skillDir string, draft RepairDraft) (string, error) {
	dir := filepath.Join(skillDir, RepairDraftsDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	// Nanoseconds in the name prevent collisions when two drafts land in the
	// same second (e.g. cooldown disabled in tests or rapid manual retries).
	base := time.Now().UTC().Format("20060102T150405.000000000Z")
	name := base + ".json"
	// Windows timer granularity can still hand out the identical nanosecond
	// stamp twice; fall back to a numeric suffix when the name is taken.
	for i := 1; ; i++ {
		_, err := statRepairDraftPath(filepath.Join(dir, name))
		if os.IsNotExist(err) {
			break
		}
		if err != nil {
			// A real stat error (permissions, I/O) would loop forever —
			// surface it instead of spinning on suffixes.
			return "", err
		}
		name = fmt.Sprintf("%s-%d.json", base, i)
	}
	data, err := json.MarshalIndent(draft, "", "  ")
	if err != nil {
		return "", err
	}
	data = append(data, '\n')
	// Atomic write via *.tmp + rename: a crash mid-write must not leave a
	// truncated JSON draft behind, which would permanently block the draft
	// flow (HasPendingRepairDraft counts any *.json as pending).
	tmp := name + ".tmp"
	if err := os.WriteFile(filepath.Join(dir, tmp), data, 0o644); err != nil {
		return "", err
	}
	if err := os.Rename(filepath.Join(dir, tmp), filepath.Join(dir, name)); err != nil {
		_ = os.Remove(filepath.Join(dir, tmp))
		return "", err
	}
	return name, nil
}

// HasPendingRepairDraft reports whether dir already contains an unreviewed
// draft — any *.json file counts as pending. The suffix check is
// case-insensitive, matching the GUI draft listing.
func HasPendingRepairDraft(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".json") {
			return true
		}
	}
	return false
}

// StepsHavePollLoop reports whether any step carries a poll/loop config.
// WriteBackOptimizedSteps does not round-trip poll/loop, so the repair-draft
// flow (which rewrites the whole steps array) must refuse such skills
// instead of silently stripping the configs on apply.
func StepsHavePollLoop(steps []corelib.NLSkillStep) bool {
	for _, s := range steps {
		if s.Poll != nil || s.Loop != nil {
			return true
		}
	}
	return false
}
