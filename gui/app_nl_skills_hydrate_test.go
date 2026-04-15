package main

import (
	"math/rand"
	"testing"
	"testing/quick"

	"github.com/RapidAI/CodeClaw/corelib"
)

// ---------------------------------------------------------------------------
// Property-based tests for shouldHydrateSkillFromFile (Bug A fix).
//
// Uses testing/quick from the Go standard library.
// ---------------------------------------------------------------------------

// SkillMergePair groups the three inputs to shouldHydrateSkillFromFile.
type SkillMergePair struct {
	ConfigSkill NLSkillEntry
	FileSkill   NLSkillEntry
	PrimaryDir  string
}

// --- Generators -----------------------------------------------------------

// nonHubSources are all Source values that are NOT "hub".
var nonHubSources = []string{"manual", "learned", "crafted", "file", "zip_import", "github", "clawhub", "auto_hub", "auto_github", "auto_clawhub", ""}

// randomName returns a non-empty skill name.
func randomName(r *rand.Rand) string {
	names := []string{"deploy", "build", "test-runner", "data-pipeline", "my-skill", "格式转换"}
	return names[r.Intn(len(names))]
}

// randomStep returns a minimal NLSkillStep with a random action.
func randomStep(r *rand.Rand) corelib.NLSkillStep {
	actions := []string{"bash", "write_file", "craft_tool", "ask_user"}
	return corelib.NLSkillStep{
		Action: actions[r.Intn(len(actions))],
		Params: map[string]interface{}{"command": "echo hello"},
	}
}

// randomSteps returns a slice of 1..3 random steps.
func randomSteps(r *rand.Rand) []corelib.NLSkillStep {
	n := r.Intn(3) + 1
	steps := make([]corelib.NLSkillStep, n)
	for i := range steps {
		steps[i] = randomStep(r)
	}
	return steps
}

// randomPrimaryDir returns a plausible primaryDir string.
func randomPrimaryDir(r *rand.Rand) string {
	dirs := []string{"/home/user/.maclaw/data/skills", "C:\\Users\\test\\.maclaw\\data\\skills", "/tmp/skills", ""}
	return dirs[r.Intn(len(dirs))]
}

// --- Bug Condition A predicate --------------------------------------------

// isBugConditionA returns true when the input matches the bug condition:
// non-hub source, both have steps, matching names, valid file steps.
//
// Formal spec from bugfix.md:
//
//	fileSkill.Name != ""
//	AND configSkill.Name == fileSkill.Name
//	AND len(fileSkill.Steps) > 0
//	AND len(configSkill.Steps) > 0
//	AND configSkill.Source != "hub"
func isBugConditionA(p SkillMergePair) bool {
	return p.FileSkill.Name != "" &&
		p.ConfigSkill.Name == p.FileSkill.Name &&
		len(p.FileSkill.Steps) > 0 &&
		len(p.ConfigSkill.Steps) > 0 &&
		p.ConfigSkill.Source != "hub"
}

// --- Task 3.1 / 3.2: Exploration + Fix test (Property 1) -----------------
//
// **Validates: Requirements 2.1, 2.2, 2.3**
//
// For any SkillMergePair where isBugConditionA holds, the fixed
// shouldHydrateSkillFromFile MUST return true.
//
// On UNFIXED code this would fail (the function returned false for non-hub
// sources with existing steps). Since the fix is already applied, this test
// should PASS, confirming the bug is resolved.

func TestProperty_BugConditionA_HydratesAfterFix(t *testing.T) {
	f := func(seed int64) bool {
		r := rand.New(rand.NewSource(seed))

		// Generate a SkillMergePair that satisfies isBugConditionA.
		name := randomName(r)
		source := nonHubSources[r.Intn(len(nonHubSources))]

		pair := SkillMergePair{
			ConfigSkill: NLSkillEntry{
				Name:   name,
				Source: source,
				Steps:  randomSteps(r), // non-empty
			},
			FileSkill: NLSkillEntry{
				Name:  name,
				Steps: randomSteps(r), // non-empty
			},
			PrimaryDir: randomPrimaryDir(r),
		}

		// Sanity: confirm the pair satisfies the bug condition.
		if !isBugConditionA(pair) {
			// Generator invariant violated — skip (should not happen).
			return true
		}

		result := shouldHydrateSkillFromFile(pair.ConfigSkill, pair.FileSkill, pair.PrimaryDir)
		return result == true
	}

	cfg := &quick.Config{MaxCount: 200}
	if err := quick.Check(f, cfg); err != nil {
		t.Errorf("Property BugConditionA failed: %v", err)
	}
}

// --- Task 3.3: Preservation test (Property 2) ----------------------------
//
// **Validates: Requirements 3.1, 3.3, 3.4, 3.5**
//
// For any SkillMergePair where isBugConditionA does NOT hold, the fixed
// function should return the expected result based on the guard conditions:
//   - false when fileSkill.Name is empty
//   - false when names don't match
//   - false when fileSkill has no steps
//   - true  when names match and fileSkill has steps (covers hub source,
//     empty config steps, etc.)
//
// This is the "reference oracle" — we compute the expected result from the
// simplified fixed logic and verify the function matches.

func expectedResult(p SkillMergePair) bool {
	if p.FileSkill.Name == "" || p.ConfigSkill.Name != p.FileSkill.Name || len(p.FileSkill.Steps) == 0 {
		return false
	}
	return true
}

func TestProperty_Preservation_NonBugInputsBehaveCorrectly(t *testing.T) {
	f := func(seed int64) bool {
		r := rand.New(rand.NewSource(seed))

		// Generate a SkillMergePair where isBugConditionA does NOT hold.
		// We pick one of several strategies to violate the bug condition.
		strategy := r.Intn(5)
		pair := SkillMergePair{
			PrimaryDir: randomPrimaryDir(r),
		}

		switch strategy {
		case 0:
			// Name mismatch
			pair.ConfigSkill = NLSkillEntry{
				Name:   randomName(r),
				Source: nonHubSources[r.Intn(len(nonHubSources))],
				Steps:  randomSteps(r),
			}
			pair.FileSkill = NLSkillEntry{
				Name:  randomName(r) + "-different",
				Steps: randomSteps(r),
			}

		case 1:
			// Empty file steps
			name := randomName(r)
			pair.ConfigSkill = NLSkillEntry{
				Name:   name,
				Source: nonHubSources[r.Intn(len(nonHubSources))],
				Steps:  randomSteps(r),
			}
			pair.FileSkill = NLSkillEntry{
				Name:  name,
				Steps: nil, // empty
			}

		case 2:
			// Empty config steps (not a bug condition — config has no steps)
			name := randomName(r)
			pair.ConfigSkill = NLSkillEntry{
				Name:   name,
				Source: nonHubSources[r.Intn(len(nonHubSources))],
				Steps:  nil, // empty
			}
			pair.FileSkill = NLSkillEntry{
				Name:  name,
				Steps: randomSteps(r),
			}

		case 3:
			// Hub source with matching primaryDir (not a bug condition)
			name := randomName(r)
			dir := randomPrimaryDir(r)
			pair.ConfigSkill = NLSkillEntry{
				Name:     name,
				Source:   "hub",
				Steps:    randomSteps(r),
				SkillDir: dir + "/" + name,
			}
			pair.FileSkill = NLSkillEntry{
				Name:  name,
				Steps: randomSteps(r),
			}
			pair.PrimaryDir = dir

		case 4:
			// Empty file skill name
			pair.ConfigSkill = NLSkillEntry{
				Name:   randomName(r),
				Source: nonHubSources[r.Intn(len(nonHubSources))],
				Steps:  randomSteps(r),
			}
			pair.FileSkill = NLSkillEntry{
				Name:  "", // empty name
				Steps: randomSteps(r),
			}
		}

		// Ensure this input does NOT satisfy the bug condition.
		if isBugConditionA(pair) {
			// Strategy didn't produce a non-bug input; skip.
			return true
		}

		got := shouldHydrateSkillFromFile(pair.ConfigSkill, pair.FileSkill, pair.PrimaryDir)
		want := expectedResult(pair)
		return got == want
	}

	cfg := &quick.Config{MaxCount: 200}
	if err := quick.Check(f, cfg); err != nil {
		t.Errorf("Property Preservation failed: %v", err)
	}
}
